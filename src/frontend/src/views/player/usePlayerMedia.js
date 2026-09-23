import { computed, nextTick, ref } from 'vue'
import Hls from 'hls.js'
import http from '../../api/http'
import { getAccessToken } from '../../utils/tokenStore'

// PlayerView 拆解（Job000091）：媒体加载状态机 + 转码编排——
// load() 四态分支（pano 照片贴球面 / pano 视频 HLS / 普通照片原图 / 普通视频 HLS+blob 回退）
// + blob URL 池（track/revoke 配对，防泄漏）+ 代次守卫（快速切换旧响应不落地）
// + 转码发起与轮询（连续失败 5 次置 failed 停重试）。
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
    pollFailCount = 0

    try {
      const [d, p] = await Promise.all([
        http.get(`/media/${mediaId.value}`),
        http.get(`/media/${mediaId.value}/360`)
      ])
      if (seq !== loadSeq) return // 已切到新媒体：旧响应不得落地（其 blob 可能已被 cleanup revoke）
      detail.value = d.data
      pano.value = p.data

      if (p.data.is_360) {
        // 360 媒体：直接全景播放（照片贴球面 / 视频走 HLS），无「普通播放器 → 点按钮」两步
        mode.value = 'pano'
        if (d.data.type === 'photo') {
          const url = await fetchBlobUrl(`/media/${mediaId.value}/download`)
          if (seq !== loadSeq) return
          panoPhotoUrl.value = url
        } else if (p.data.hls_master) {
          hlsUrl.value = API_BASE + p.data.hls_master
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
    if (master && Hls.isSupported()) {
      plainHls = new Hls({
        xhrSetup: (xhr) => {
          const token = getAccessToken()
          if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token)
        }
      })
      plainHls.loadSource(API_BASE + master)
      plainHls.attachMedia(v)
    } else {
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

  async function startTranscode() {
    transcode.value.starting = true
    transcode.value.failed = false
    try {
      const res = await http.post('/transcode/job', { media_id: mediaId.value, profile: '1080p' })
      transcode.value.jobId = res.data.job_id
      transcode.value.status = 'pending'
      pollFailCount = 0
      pollJob()
    } catch (e) {
      error.value = e.response?.data?.error?.message || '发起转码失败'
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
          if (p.data.hls_master) hlsUrl.value = API_BASE + p.data.hls_master
          return
        }
        if (res.data.status === 'failed') {
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
    panoKind, panoSrc, panoReady,
    load, startTranscode, cleanup
  }
}
