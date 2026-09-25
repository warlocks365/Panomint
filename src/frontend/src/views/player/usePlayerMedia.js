import { computed, nextTick, ref } from 'vue'
import Hls from 'hls.js'
import http from '../../api/http'
import { getAccessToken } from '../../utils/tokenStore'

// PlayerView 拆解（Job000091）：媒体加载状态机 + 转码编排——
// load() 四态分支（pano 照片贴球面 / pano 视频 HLS / 普通照片原图 / 普通视频 HLS+blob 回退）
// + blob URL 池（track/revoke 配对，防泄漏）+ 代次守卫（快速切换旧响应不落地）
// + 转码发起与轮询（连续失败 5 次置 failed 停重试）。
// Job000124：播放时自动转码——开关开且总闸门开时，无 HLS 视频播放即自动发起
//（auto 分支服务端 attach-or-create，幂等）；普通视频转码期间先播原始文件，
// 完成后原地热切换 HLS；hls.js ABR 调优（capLevelToPlayerSize + 缓冲上界）。
// plainVideoRef 由宿主传入（DOM 归属宿主）；plainHls 实例本模块自持，cleanup 销毁。
const API_BASE = http.defaults.baseURL

export function usePlayerMedia(mediaId, plainVideoRef) {
  const loading = ref(true)
  const error = ref('')
  const detail = ref(null)
  const pano = ref(null)
  const mode = ref('') // photo | pano | video
  const hlsUrl = ref('')
  const photoUrl = ref('')
  const panoPhotoUrl = ref('') // 360 照片：原图 blob URL（球面贴图）
  const transcode = ref({ jobId: '', status: '', starting: false, failed: false })
  // Job000120-r2：系统级「自动 HLS 转码」总闸门（false = 播放器不得发起转码，直接播原始文件）
  const transcodeDisabled = ref(false)
  // Job000124：系统级「播放时自动转码」开关（false = 保持手动按钮现状）
  const realtimeEnabled = ref(false)

  let pollTimer = 0
  let plainHls = null
  let blobUrls = []
  let loadSeq = 0 // 代次守卫（同 MediaViewer.loadCurrent）：快速切换媒体时旧响应不得落地
  let pollFailCount = 0 // 转码轮询连续失败计数（P2-06：达上限置 failed 停止重试）

  /* 360 播放属性：照片走 TextureLoader，视频走 HLS */
  const panoKind = computed(() => (detail.value?.type === 'photo' ? 'photo' : 'video'))
  const panoSrc = computed(() => (panoKind.value === 'photo' ? panoPhotoUrl.value : hlsUrl.value))
  const panoReady = computed(() => !!panoSrc.value)

  function trackBlob(url) {
    blobUrls.push(url)
    return url
  }

  async function fetchBlobUrl(path) {
    const res = await http.get(path, { responseType: 'blob' })
    return trackBlob(URL.createObjectURL(res.data))
  }

  async function load() {
    const seq = ++loadSeq
    cleanup()
    loading.value = true
    error.value = ''
    mode.value = ''
    hlsUrl.value = ''
    detail.value = null
    pano.value = null
    transcode.value = { jobId: '', status: '', starting: false, failed: false }
    transcodeDisabled.value = false
    realtimeEnabled.value = false
    pollFailCount = 0

    try {
      const [d, p, cfg] = await Promise.all([
        http.get(`/media/${mediaId.value}`),
        http.get(`/media/${mediaId.value}/360`),
        // 系统级开关每次播放现取（管理后台改完全站即生效）。
        // 总闸门失败按开启处理（宁可提示转码也不误锁播放，Job000120-r2）；
        // 实时开关失败按关闭处理（宁可退手动也不误自动，Job000124）。
        http.get('/transcode/config').catch(() => null)
      ])
      if (seq !== loadSeq) return // 已切到新媒体：旧响应不得落地（其 blob 可能已被 cleanup revoke）
      detail.value = d.data
      pano.value = p.data
      transcodeDisabled.value = !!cfg && cfg.data && cfg.data.auto_transcode === false
      realtimeEnabled.value = !!cfg && cfg.data && cfg.data.realtime_transcode === true

      if (p.data.is_360) {
        // 360 媒体：直接全景播放（照片贴球面 / 视频走 HLS），无「普通播放器 → 点按钮」两步
        mode.value = 'pano'
        if (d.data.type === 'photo') {
          const url = await fetchBlobUrl(`/media/${mediaId.value}/download`)
          if (seq !== loadSeq) return
          panoPhotoUrl.value = url
        } else if (p.data.hls_master) {
          hlsUrl.value = API_BASE + p.data.hls_master
        } else if (realtimeEnabled.value && !transcodeDisabled.value) {
          // Job000124：播放时自动转码——无 HLS 的 360 视频自动发起，进度经 TranscodePrompt 展示
          startTranscode(true)
        }
      } else if (d.data.type === 'photo') {
        mode.value = 'photo'
        const url = await fetchBlobUrl(`/media/${mediaId.value}/download`)
        if (seq !== loadSeq) return
        photoUrl.value = url
      } else {
        mode.value = 'video'
        loading.value = false // 先渲染出 video 元素再挂载 HLS
        await nextTick()
        if (seq !== loadSeq) return
        setupPlainVideo(p.data.hls_master, seq)
        if (!p.data.hls_master && realtimeEnabled.value && !transcodeDisabled.value) {
          // Job000124：普通视频无 HLS 且实时开关开——后台自动转码，期间原始文件照播，
          // 完成后 pollJob 原地热切换 HLS（用户无感升级）
          startTranscode(true)
        }
      }
    } catch (e) {
      if (seq !== loadSeq) return
      error.value = e.response?.data?.error?.message || '加载失败'
    } finally {
      if (seq === loadSeq) loading.value = false
    }
  }

  function setupPlainVideo(master, seq) {
    const v = plainVideoRef.value
    if (!v) return
    if (plainHls) {
      plainHls.destroy() // 热切换前先拆旧实例（Job000124：转码完成后 blob → HLS 升级）
      plainHls = null
    }
    if (master && Hls.isSupported()) {
      plainHls = new Hls({
        // ABR 调优（Job000124）：不投放超过播放器像素尺寸的档位；缓冲上界 30s/60s，
        // 升降档交给 hls.js 双 EWMA 带宽估算（直播式自适应体验，防抖由估算器天然承担）
        capLevelToPlayerSize: true,
        maxBufferLength: 30,
        maxMaxBufferLength: 60,
        xhrSetup: (xhr) => {
          const token = getAccessToken()
          if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token)
        }
      })
      plainHls.loadSource(API_BASE + master)
      plainHls.attachMedia(v)
    } else if (!master) {
      // 无 HLS 或 Safari 原生：回退下载 blob（Safari 原生 HLS 无法带 Bearer，同样走 blob）
      fetchBlobUrl(`/media/${mediaId.value}/download`)
        .then((url) => {
          if (seq !== loadSeq) return // 过期代次的 blob 不回填（URL 可能已被 revoke）
          v.src = url
        })
        .catch(() => {
          if (seq !== loadSeq) return
          error.value = '视频加载失败'
        })
    }
  }

  // startTranscode：auto=true 走 Job000124 自动分支（服务端 attach-or-create + 按源选档）；
  // auto=false 保持 Job000120 手动语义（profile 写死 1080p，新建任务）。
  async function startTranscode(auto = false) {
    const seq = loadSeq // 代次捕获：退避重试落地前若已切媒体，结果不得写入新态
    // Job000120：总闸门关闭时客户端直接挡（服务端 CreateJob 同样 409 拒绝，双保险）
    if (transcodeDisabled.value) {
      if (!auto) error.value = '已关闭自动转码，可在设置页重新开启'
      return
    }
    transcode.value.starting = true
    transcode.value.failed = false
    try {
      const body = auto ? { media_id: mediaId.value, auto: true } : { media_id: mediaId.value, profile: '1080p' }
      const res = await http.post('/transcode/job', body)
      transcode.value.jobId = res.data.job_id
      transcode.value.status = 'pending'
      pollFailCount = 0
      pollJob()
    } catch (e) {
      if (e?.response?.status === 429 && auto) {
        // 全局限流（整站共享 60 令牌桶）：自动触发退避 3s 重试一次，再败则按失败态展示
        await new Promise((r) => setTimeout(r, 3000))
        if (seq !== loadSeq) return
        try {
          const res = await http.post('/transcode/job', { media_id: mediaId.value, auto: true })
          transcode.value.jobId = res.data.job_id
          transcode.value.status = 'pending'
          pollJob()
          return
        } catch (e2) {
          e = e2
        }
      }
      // 自动分支失败不打扰播放：普通视频正播原始文件（提示由 transcode.failed 驱动），
      // 360 视频由 TranscodePrompt 的失败态给出「重新发起」按钮
      transcode.value.failed = true
      if (!auto) error.value = e.response?.data?.error?.message || '发起转码失败'
    } finally {
      transcode.value.starting = false
    }
  }

  function pollJob() {
    clearTimeout(pollTimer)
    pollTimer = setTimeout(async () => {
      try {
        const res = await http.get(`/transcode/job/${transcode.value.jobId}`)
        pollFailCount = 0
        transcode.value.status = res.data.status
        if (res.data.status === 'done') {
          const p = await http.get(`/media/${mediaId.value}/360`)
          pano.value = p.data
          if (mode.value === 'video' && plainVideoRef.value) {
            // Job000124 热切换：普通视频转码完成后原地换 HLS（blob 继续兜底由 setupPlainVideo 处理）
            if (p.data.hls_master) setupPlainVideo(p.data.hls_master, loadSeq)
          } else if (p.data.hls_master) {
            hlsUrl.value = API_BASE + p.data.hls_master
          }
          return
        }
        if (res.data.status === 'failed') {
          // 降级（Job000124）：普通视频保持原始文件播放（transcode.failed 仅驱动轻提示）；
          // 360 视频无回退可能，TranscodePrompt 失败态可手动重试；服务端队列层仍有退避重试。
          transcode.value.failed = true
          return
        }
        pollJob()
      } catch {
        // 轮询连续失败达上限则停止（服务下线等场景不再无限重试）
        pollFailCount++
        if (pollFailCount >= 5) {
          transcode.value.failed = true
          return
        }
        pollJob()
      }
    }, 2000)
  }

  function cleanup() {
    clearTimeout(pollTimer)
    if (plainHls) {
      plainHls.destroy()
      plainHls = null
    }
    for (const u of blobUrls) URL.revokeObjectURL(u)
    blobUrls = []
    photoUrl.value = ''
    panoPhotoUrl.value = ''
  }

  return {
    loading, error, detail, pano, mode, hlsUrl, photoUrl, panoPhotoUrl, transcode,
    transcodeDisabled, realtimeEnabled,
    panoKind, panoSrc, panoReady,
    load, startTranscode, cleanup
  }
}
