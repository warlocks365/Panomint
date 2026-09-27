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
// Job000126：无 HLS 的 360 视频不再死局——原始文件 blob 回退贴球面（视角/陀螺仪/VR
// 可用，清晰度切换与拖动降级）；实时开关开则转码期间同播、完成后经 panoSrc 优先级
// 自动切回 HLS（blob 即时 revoke 防双份大文件占内存）；总闸门关闭时仅回退不发起。
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
  const panoOriginalUrl = ref('') // Job000126：360 视频无 HLS 时的原始文件 blob URL（回退贴球面）
  const panoFallbackLoading = ref(false) // Job000126：原始回退 blob 拉取中（防止 TranscodePrompt 闪现）
  const liveEligible = ref(false) // Job000131 P1：HEVC 编码且浏览器不支持 → 可提供实时转码兜底入口
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

  /* 360 播放属性：照片走 TextureLoader，视频走 HLS（Job000126：无 HLS 时原始 blob 回退） */
  const panoKind = computed(() => (detail.value?.type === 'photo' ? 'photo' : 'video'))
  const panoSrc = computed(() =>
    panoKind.value === 'photo' ? panoPhotoUrl.value : (hlsUrl.value || panoOriginalUrl.value)
  )
  const panoReady = computed(() => !!panoSrc.value)

  function trackBlob(url) {
    blobUrls.push(url)
    return url
  }

  // Job000126：从池中移除并立即 revoke（360 视频原始 blob 体积大，HLS 就绪后不留双份内存）
  function untrackBlob(url) {
    const i = blobUrls.indexOf(url)
    if (i >= 0) blobUrls.splice(i, 1)
    URL.revokeObjectURL(url)
  }

  async function fetchBlobUrl(path) {
    const res = await http.get(path, { responseType: 'blob' })
    return trackBlob(URL.createObjectURL(res.data))
  }

  /* Job000129：原始文件直链流式（Range）——GB 级视频的全量 blob 下载会在 Chromium
   * 缓冲预分配阶段直接失败（实测 5.3GB：net::ERR_FAILED @ 8ms，传输未开始）。
   * <video> 元素无法携带 Authorization 头：download 端点经 AuthRequired 的 query 分支
   * 接受短时 ?at=<access_token>（仅 GET /media/ 读路径），浏览器原生 Range 分段拉取
   * （服务端 Accept-Ranges: bytes 已在位，206 能力零改动）。 */
  function directDownloadUrl() {
    const token = getAccessToken()
    const base = API_BASE + `/media/${mediaId.value}/download`
    return token ? `${base}?at=${encodeURIComponent(token)}` : base
  }
  function isHevcCodec(codec) {
    const c = (codec || '').toLowerCase()
    return c === 'hevc' || c === 'h265' || c === 'hev1' || c === 'hvc1'
  }
  // Chrome/Edge 默认无 HEVC 解码（需硬件解码 + 系统扩展）：先检测，不支持的给出
  // 显式编码提示——否则 Range 改造后仍会以 video error 形式笼统失败。
  function hevcPlayable() {
    const v = document.createElement('video')
    return v.canPlayType('video/mp4; codecs="hvc1"') !== '' || v.canPlayType('video/mp4; codecs="hev1"') !== ''
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
    panoOriginalUrl.value = ''
    liveEligible.value = false
    panoFallbackLoading.value = false
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
        } else {
          // Job000126：无 HLS 的 360 视频——原始文件回退贴球面（总闸门关闭也可见，
          // 不再死局）。视角转动/陀螺仪/VR 可用，清晰度切换与拖动起播降级（提示条说明）。
          // Job000129：回退由全量 blob 改为直链 Range 流式（GB 级 blob 缓冲预分配即死）；
          // HEVC 源文件先经浏览器解码能力检测，不支持则显式提示而非笼统加载失败。
          // 拉取期间 panoFallbackLoading 守卫 TranscodePrompt 闪现；转码完成后 panoSrc
          // 优先 hlsUrl 自动切回 HLS。
          const codec = (d.data && d.data.codec) || ''
          if (isHevcCodec(codec) && !hevcPlayable()) {
            error.value = '浏览器不支持该视频编码（HEVC），请开启 HLS 转码或换用支持的浏览器'
            liveEligible.value = true
          } else {
            panoOriginalUrl.value = directDownloadUrl()
          }
          if (realtimeEnabled.value && !transcodeDisabled.value) {
            // Job000124 自动转码：回退播放与转码并行，完成后热切换 HLS
            startTranscode(true)
          }
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
        setupPlainVideo(p.data.hls_master, (d.data && d.data.codec) || '')
        if (!p.data.hls_master && realtimeEnabled.value && !transcodeDisabled.value) {
          // Job000124：普通视频无 HLS 且实时开关开——后台自动转码，期间原始文件照播，
          // 完成后 pollJob 原地热切换 HLS（用户无感升级）
          startTranscode(true)
        }
      }
    } catch (e) {
      if (seq !== loadSeq) return
      error.value = e.response?.data?.error?.message || `加载失败（${e.response?.status || '网络'}）`
    } finally {
      if (seq === loadSeq) loading.value = false
    }
  }

  function setupPlainVideo(master, codec) {
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
      // 无 HLS 或 Safari 原生：回退原始文件（Job000129：直链 Range 流式替代全量 blob；
      // HEVC 先检测浏览器解码能力，不支持给显式编码提示）
      if (isHevcCodec(codec) && !hevcPlayable()) {
        error.value = '浏览器不支持该视频编码（HEVC），请开启 HLS 转码或换用支持的浏览器'
        liveEligible.value = true
        return
      }
      v.src = directDownloadUrl()
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
            // Job000126：HLS 就绪即收回原始 blob（panoSrc 优先 hlsUrl，key 变化触发 360Player
            // 重挂载走 attachHls）——4K 原始文件 blob 体积大，不留双份内存
            if (panoOriginalUrl.value) {
              untrackBlob(panoOriginalUrl.value)
              panoOriginalUrl.value = ''
    liveEligible.value = false
            }
          }
          return
        }
        if (res.data.status === 'failed') {
          // 降级（Job000124/126）：普通视频保持原始文件播放（transcode.failed 仅驱动轻提示）；
          // 360 视频 Job000126 起同样有原始回退——失败态由回退提示条给出重试按钮；服务端队列层仍有退避重试。
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
    panoKind, panoSrc, panoReady, panoOriginalUrl, panoFallbackLoading, liveEligible,
    load, startTranscode, cleanup
  }
}
