/* 360Player 流媒体与播放控制：HLS 接入/质量档/带宽选档/错误恢复/照片贴图/播放控制/性能监测。
 *
 * 自持：hls 实例与重试退避/质量单与 autoLevelCapping/原生 HLS 分支/TextureLoader 贴球/
 *      播放控制（播放暂停/静音/倍速/进度）/fps 监测与低帧率降档/页面隐藏暂停/错误 overlay。
 * UI 状态（statsText/timeText/progressValue/qualityOptions 等）由本域持有并导出给宿主模板绑定。
 */
import { ref, reactive } from 'vue'
import Hls from 'hls.js'
import * as THREE from 'three'
import { getAccessToken } from '../../utils/tokenStore'

export function usePanoStream({ engine, props, showToast }) {
  /* UI 状态（宿主模板绑定） */
  const statsText = ref('')
  const qualityOptions = ref([])
  const qualityValue = ref('-1')
  const timeText = ref('00:00 / 00:00')
  const progressValue = ref(0)
  const playing = ref(false)
  const muted = ref(true)
  const rate = ref('1')
  const vrSupported = ref(false)
  const overlay = reactive({ show: false, msg: '', sub: '' })

  let hls = null
  let netRetries = 0
  const MAX_NET_RETRIES = 3
  let backoff = 1000

  /* 性能监测 */
  let frameCount = 0, fpsWindowStart = 0, lowFpsSince = 0
  let statsLevel = ''
  let toastTimer = 0

  function showOverlay(msg, sub) {
    overlay.msg = msg; overlay.sub = sub || ''; overlay.show = true
  }

  /* ---- WebXR：会话结束联动播放暂停（播放状态属本域） ---- */
  async function enterVr() {
    const renderer = engine.getRenderer()
    if (!renderer || renderer.xr.isPresenting) return
    try {
      const session = await navigator.xr.requestSession('immersive-vr', { optionalFeatures: ['local-floor'] })
      session.addEventListener('end', () => {
        if (props.mode !== 'photo') { video().pause(); playing.value = false }
      })
      await renderer.xr.setSession(session)
      if (props.mode !== 'photo') { video().play().catch(() => {}); playing.value = true }
    } catch (err) {
      showToast('VR 会话启动失败：' + err.message)
    }
  }
  function detectVr() {
    if (navigator.xr && navigator.xr.isSessionSupported) {
      navigator.xr.isSessionSupported('immersive-vr').then((ok) => { vrSupported.value = ok }).catch(() => {})
    }
  }

  /* ---- hls.js ---- */
  // pickStartLevel 按实测下行带宽在 master.m3u8 的档位里挑**初始档**：
  // 取「BANDWIDTH ≤ 0.8×down_kbps」中码率最高的那档（留 20% 余量，避免首片就卡）。
  // 返回 -1 表示不干预，交给 hls.js 默认 ABR —— 以下三种情况必须走 -1：
  //   · 带宽未知（0）或测速失败；· 只有一档（源片分辨率不够导致阶梯退化为单档）；
  //   · 没有任何档位低于可用带宽。
  function pickStartLevel(levels, downKbps, capIdx) {
    if (!Array.isArray(levels) || levels.length <= 1) return -1
    if (!downKbps || downKbps <= 0) return -1
    const budget = downKbps * 1000 * 0.8
    let best = -1
    let bestRate = -1
    levels.forEach((lv, i) => {
      if (capIdx >= 0 && i > capIdx) return // 不越过纹理上限（与 MANIFEST_PARSED 里的 autoLevelCapping 一致）
      const rate = Number(lv?.bitrate) || 0
      if (rate > 0 && rate <= budget && rate > bestRate) {
        bestRate = rate
        best = i
      }
    })
    return best
  }

  function video() { return engine.getVideo() }

  function attachHls(url) {
    if (hls) { try { video().__hls = null } catch {} ; hls.destroy(); hls = null }
    netRetries = 0; backoff = 1000
    if (Hls.isSupported()) {
      hls = new Hls({
        maxBufferLength: 30,
        capLevelToPlayerSize: false,
        xhrSetup: (xhr, url) => {
          // 分享密码：m3u8 相对路径的 ts 切片请求不继承 master URL 查询串，逐请求补挂
          if (props.appendQuery) {
            const sep = url.includes('?') ? '&' : '?'
            xhr.open('GET', url + sep + props.appendQuery, true)
          }
          // 主站 HLS 需 Bearer；分享公开端点免鉴权
          if (props.auth === 'bearer') {
            const token = getAccessToken()
            if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token)
          }
        }
      })
      hls.loadSource(url)
      hls.attachMedia(video())
      // Job000136-1.8.2：hls 实例挂到 video 元素——码率采样读 bandwidthEstimate（MSE 场景唯一可靠数据源）
      video().__hls = hls
      hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
        const opts = []
        let maxAllowed = -1
        const maxTex = engine.getMaxTex()
        data.levels.forEach((lv, i) => {
          const overCap = lv.width > maxTex
          opts.push({
            value: String(i),
            label: `${lv.height}p (${(lv.bitrate / 1000).toFixed(0)}k)${overCap ? ' 超纹理上限' : ''}`,
            disabled: overCap
          })
          if (!overCap) maxAllowed = i
        })
        qualityOptions.value = opts
        if (maxAllowed >= 0) hls.autoLevelCapping = maxAllowed

        // Phase 4 P1：按实测带宽选初始档。只影响**首片**，首个分片到手后立即把控制权交还 ABR，
        // 因此既有的「纹理上限 autoLevelCapping」与「低帧率自动降档」全部保持原样；
        // pickStartLevel 返回 -1 时（单档 / 无量测 / 无合适档）行为与改动前完全一致。
        const hintLevel = pickStartLevel(data.levels, props.bandwidthKbps, maxAllowed)
        if (hintLevel >= 0) {
          hls.startLevel = hintLevel
          hls.currentLevel = hintLevel
          hls.once(Hls.Events.FRAG_LOADED, () => {
            if (hls) hls.currentLevel = -1 // 交还 ABR，避免整段锁死档位
          })
        }

        video().play().catch(() => {})
        playing.value = !video().paused
      })
      hls.on(Hls.Events.LEVEL_SWITCHED, (_, data) => {
        const lv = hls.levels[data.level]
        if (lv) statsLevel = `${lv.height}p`
      })
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (!data.fatal) return
        if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
          if (netRetries < MAX_NET_RETRIES) {
            netRetries++
            showToast(`网络错误，${backoff / 1000}s 后重试（${netRetries}/${MAX_NET_RETRIES}）`)
            setTimeout(() => { if (hls) hls.startLoad() }, backoff)
            backoff = Math.min(backoff * 2, 8000)
          } else {
            showOverlay('网络连接失败', '已重试 3 次，请检查网络后重试')
          }
        } else if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
          try {
            hls.recoverMediaError()
          } catch {
            if (hls.currentLevel > 0) { hls.currentLevel = hls.currentLevel - 1; showToast('已切换到更低画质') }
            else showOverlay('媒体解析失败', '无法恢复播放')
          }
        } else {
          showOverlay('播放失败', '请检查网络或流地址')
        }
      })
    } else if (video().canPlayType('application/vnd.apple.mpegurl')) {
      // 原生 HLS（iOS Safari / 微信 iOS，无 MSE）：无法给 master 请求加 Authorization，
      // 也无法像 xhrSetup 那样给 m3u8 相对路径的 ts 切片逐个补挂查询串。
      // 因此受保护内容（主站 Bearer / 密码分享）在该分支必然 401/403 —— 不得静默失败，
      // 明确提示换设备。长期方案：后端为切片签发一次性查询令牌（签名 URL），
      // 使切片请求自带鉴权，前端无需注入。
      if (props.auth === 'bearer' || props.appendQuery) {
        showOverlay('当前浏览器不支持受保护视频播放', '请用桌面或 Android 设备观看')
        return
      }
      video().src = url
      video().play().catch(() => {})
      playing.value = !video().paused
    } else {
      showOverlay('当前浏览器不支持 HLS', '请使用现代浏览器')
    }
  }
  function onQualityChange() {
    if (!hls) return
    hls.currentLevel = Number(qualityValue.value)
  }

  /* ---- 360 照片：TextureLoader 贴球（与视频共用球体/交互/陀螺仪/VR） ---- */
  function attachPhoto(url) {
    statsText.value = '加载中…'
    new THREE.TextureLoader().load(
      url,
      (tex) => {
        if (engine.isDisposed()) { tex.dispose(); return }
        // SphereGeometry 顶部 UV v=1 → 图片顶行；flipY=true 时 v=1 恰为顶行，天顶朝上（与初始纹理约定一致）
        tex.flipY = true
        tex.colorSpace = THREE.SRGBColorSpace
        engine.setTexture(tex)
        showToast('全景照片已加载')
      },
      undefined,
      () => { showOverlay('图片加载失败', '请检查网络后重试') }
    )
  }

  /* ---- 按媒体形态分派加载：照片贴图 / 原始文件直连（Job000126 回退）/ HLS ---- */
  function attachSource() {
    if (props.mode === 'photo') { attachPhoto(props.src); return }
    // 原始文件回退（usePlayerMedia 在无 HLS 时提供）：video 元素直接吃原始源——
    // Job000126 为 blob URL（全量下载），Job000129 起为直链 HTTP URL（浏览器原生
    // Range 流式，GB 级文件不再缓冲预分配即死）。两者共同特征：非 .m3u8（HLS 入口
    // 恒为 master playlist）；纹理循环照常每帧取帧贴球——视角转动/陀螺仪/VR 全部可用。
    if (props.src && !props.src.includes('.m3u8')) { attachOriginal(props.src); return }
    attachHls(props.src)
  }
  function attachOriginal(url) {
    statsLevel = ''
    qualityOptions.value = [] // 原始文件无档位概念（360Player 档位选择器随 options 为空隐藏）
    const v = video()
    v.src = url
    v.play().catch(() => {})
    playing.value = !v.paused
  }
  // Job000129：直链模式下网络/鉴权失败不再经 axios catch（video 元素自主拉流）——
  // error 事件兜底 overlay，避免静默黑屏（token 过期/断流/编码不支持均落此提示）。
  function onOriginalError() {
    if (props.mode === 'photo') return
    if (hls) return // HLS 模式的错误由 hls.js ERROR 事件处理（含重试/恢复），不重复弹层
    showOverlay('视频加载失败', '请检查网络或登录状态后重试')
  }
  function onRetry() {
    overlay.show = false
    attachSource()
  }

  /* ---- 性能监测（帧钩子返回 false，不接管相机） ---- */
  function fpsFrame() {
    frameCount++
    const now = performance.now()
    if (now - fpsWindowStart >= 1000) {
      const fps = frameCount * 1000 / (now - fpsWindowStart)
      frameCount = 0; fpsWindowStart = now
      if (!toastTimer) statsText.value = `${fps.toFixed(0)}fps ${statsLevel}`.trim()
      if (fps < 30 && hls && hls.autoLevelEnabled) {
        lowFpsSince += 1000
        if (lowFpsSince >= 3000) {
          const lv = hls.currentLevel >= 0 ? hls.currentLevel : hls.loadLevel
          if (lv > 0) { hls.currentLevel = lv - 1; showToast('帧率过低，已自动降档') }
          lowFpsSince = 0
        }
      } else {
        lowFpsSince = 0
      }
    }
    return false
  }

  /* ---- 播放控制 ---- */
  function togglePlay() {
    video().paused ? video().play() : video().pause()
    playing.value = !video().paused
  }
  function toggleMute() {
    video().muted = !video().muted
    muted.value = video().muted
  }
  function onRateChange() { video().playbackRate = Number(rate.value) }
  function fmt(t) {
    if (!isFinite(t)) return '00:00'
    const m = Math.floor(t / 60), s = Math.floor(t % 60)
    return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
  }
  function onTimeUpdate() {
    if (video().duration) progressValue.value = Math.round((video().currentTime / video().duration) * 1000)
    timeText.value = `${fmt(video().currentTime)} / ${fmt(video().duration)}`
  }
  function onSeek() {
    if (video().duration) video().currentTime = (Number(progressValue.value) / 1000) * video().duration
  }
  function onVisibilityPause() {
    if (props.mode !== 'photo' && document.hidden) { video().pause(); playing.value = false }
  }

  function init() {
    fpsWindowStart = performance.now()
    engine.onFrame(fpsFrame)
    engine.setVisibilityHook(onVisibilityPause)
    video().addEventListener('timeupdate', onTimeUpdate)
    video().addEventListener('error', onOriginalError)
    detectVr()
    attachSource()
  }

  function destroy() {
    if (hls) { hls.destroy(); hls = null }
    const v = engine.getVideo()
    if (v) {
      v.removeEventListener('timeupdate', onTimeUpdate)
      v.removeEventListener('error', onOriginalError)
      v.pause()
      v.removeAttribute('src')
      v.load()
    }
    clearTimeout(toastTimer)
  }

  return {
    statsText, qualityOptions, qualityValue, timeText, progressValue,
    playing, muted, rate, vrSupported, overlay,
    attachSource, onRetry, onQualityChange, togglePlay, toggleMute,
    onRateChange, onSeek, enterVr, init, destroy
  }
}
