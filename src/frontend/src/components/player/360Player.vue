<template>
  <div class="pano-player">
    <div ref="containerRef" class="pano-container"></div>
    <video ref="videoRef" crossorigin="anonymous" playsinline webkit-playsinline loop muted class="pano-video"></video>

    <div class="bar topbar" :class="{ hidden: barsHidden }">
      <span class="title">{{ title || '360 全景播放' }}</span>
      <span class="stats">{{ statsText }}</span>
      <select v-model="qualityValue" @change="onQualityChange">
        <option value="-1">自动</option>
        <option v-for="q in qualityOptions" :key="q.value" :value="q.value" :disabled="q.disabled">
          {{ q.label }}
        </option>
      </select>
    </div>

    <div class="bar controls" :class="{ hidden: barsHidden }">
      <div class="progress-wrap">
        <span class="time">{{ timeText }}</span>
        <input type="range" class="progress" min="0" max="1000" v-model="progressValue" @input="onSeek">
      </div>
      <button @click="togglePlay">{{ playing ? '暂停' : '播放' }}</button>
      <button :class="{ active: !muted }" @click="toggleMute">音量</button>
      <select v-model="rate" @change="onRateChange">
        <option value="0.5">0.5x</option>
        <option value="1">1.0x</option>
        <option value="1.5">1.5x</option>
        <option value="2">2.0x</option>
      </select>
      <button :class="{ active: gyroOn }" @click="toggleGyro">陀螺仪 {{ gyroOn ? '开' : '关' }}</button>
      <button @click="calibrate">校准</button>
      <button :disabled="!vrSupported" @click="enterVr">VR 模式</button>
      <span class="fov-label">FOV</span>
      <input type="range" class="fov" min="45" max="100" v-model="fovValue" @input="onFovInput">
    </div>

    <div class="overlay" :class="{ show: overlay.show }">
      <div class="msg">{{ overlay.msg }}</div>
      <div class="sub">{{ overlay.sub }}</div>
      <button class="retry" @click="onRetry">重试</button>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onBeforeUnmount } from 'vue'
import * as THREE from 'three'
import Hls from 'hls.js'
import { getAccessToken } from '../../utils/tokenStore'

const props = defineProps({
  mediaId: { type: String, required: true },
  src: { type: String, required: true }, // hls master 完整 URL
  title: { type: String, default: '' }
})

const containerRef = ref(null)
const videoRef = ref(null)

/* UI 状态 */
const barsHidden = ref(false)
const statsText = ref('')
const qualityOptions = ref([])
const qualityValue = ref('-1')
const timeText = ref('00:00 / 00:00')
const progressValue = ref(0)
const playing = ref(false)
const muted = ref(true)
const rate = ref('1')
const gyroOn = ref(false)
const vrSupported = ref(false)
const fovValue = ref(75)
const overlay = reactive({ show: false, msg: '', sub: '' })

/* 内部引用（非响应式） */
let renderer, scene, camera, texture, sphereGeo, sphereMat, video, el
let hls = null
let maxTex = 0
let hevcSupported = false

/* 子系统 2：拖拽/捏合 */
let lon = 0, lat = 0, downX = 0, downY = 0, downLon = 0, downLat = 0
let dragging = false
const pointers = new Map()
let pinchDist = 0

/* 子系统 3：陀螺仪 */
const zee = new THREE.Vector3(0, 0, 1)
const euler = new THREE.Euler()
const q0 = new THREE.Quaternion()
const q1 = new THREE.Quaternion(-Math.sqrt(0.5), 0, 0, Math.sqrt(0.5))
const calibOffset = new THREE.Quaternion()
const isWeChat = /MicroMessenger/i.test(navigator.userAgent)
let gyroWatchdog = 0
let gyroGotData = false

/* 子系统 5：错误恢复 */
let netRetries = 0
const MAX_NET_RETRIES = 3
let backoff = 1000

/* 子系统 6：性能监测 */
let frameCount = 0, fpsWindowStart = 0, lowFpsSince = 0
let statsLevel = ''
let toastTimer = 0
let hideTimer = 0
let disposed = false

function setFov(f) {
  camera.fov = Math.max(45, Math.min(100, f))
  camera.updateProjectionMatrix()
  fovValue.value = Math.round(camera.fov)
}
function onFovInput() { setFov(Number(fovValue.value)) }

/* ---- 指针事件（命名函数以便卸载） ---- */
function onPointerDown(e) {
  pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
  if (pointers.size === 1) {
    dragging = true
    downX = e.clientX; downY = e.clientY
    downLon = lon; downLat = lat
  } else if (pointers.size === 2) {
    const [a, b] = [...pointers.values()]
    pinchDist = Math.hypot(a.x - b.x, a.y - b.y)
  }
  el.setPointerCapture(e.pointerId)
}
function onPointerMove(e) {
  if (pointers.has(e.pointerId)) {
    pointers.set(e.pointerId, { x: e.clientX, y: e.clientY })
    if (pointers.size === 2) {
      const [a, b] = [...pointers.values()]
      const d = Math.hypot(a.x - b.x, a.y - b.y)
      if (pinchDist > 0) setFov(camera.fov * (pinchDist / d))
      pinchDist = d
    } else if (dragging && !gyroOn.value) {
      lon = downLon + (downX - e.clientX) * 0.12
      lat = Math.max(-85, Math.min(85, downLat + (e.clientY - downY) * 0.12))
    }
  }
  pokeBars()
}
function onPointerRelease(e) {
  pointers.delete(e.pointerId)
  if (pointers.size === 0) dragging = false
}
function onWheel(e) {
  e.preventDefault()
  setFov(camera.fov + e.deltaY * 0.03)
}

/* ---- 陀螺仪 ---- */
function screenOrientationAngle() {
  return (screen.orientation && screen.orientation.angle) || window.orientation || 0
}
function orientToQuat(alpha, beta, gamma) {
  const d = Math.PI / 180
  euler.set(beta * d, alpha * d, -gamma * d, 'YXZ')
  const q = new THREE.Quaternion().setFromEuler(euler)
  q.multiply(q1)
  q.multiply(q0.setFromAxisAngle(zee, -screenOrientationAngle() * d))
  return q
}
function onDeviceOrientation(e) {
  if (e.alpha === null && e.beta === null && e.gamma === null) return
  gyroGotData = true
  camera.quaternion.copy(orientToQuat(e.alpha || 0, e.beta || 0, e.gamma || 0)).premultiply(calibOffset)
}
function attachGyroListeners() {
  window.addEventListener('deviceorientation', onDeviceOrientation)
  window.addEventListener('deviceorientationabsolute', onDeviceOrientation)
  gyroGotData = false
  clearTimeout(gyroWatchdog)
  gyroWatchdog = setTimeout(() => {
    if (!gyroGotData) {
      gyroOff(isWeChat ? '微信浏览器未提供陀螺仪数据，已降级为拖拽模式' : '未检测到陀螺仪数据，已降级为拖拽模式')
    }
  }, 1500)
}
async function enableGyro() {
  if (typeof DeviceOrientationEvent !== 'undefined' && typeof DeviceOrientationEvent.requestPermission === 'function') {
    try {
      const res = await DeviceOrientationEvent.requestPermission()
      if (res !== 'granted') { gyroOff('权限被拒，已降级为拖拽模式'); return }
    } catch { gyroOff('权限请求失败，已降级为拖拽模式'); return }
  }
  attachGyroListeners()
  gyroOn.value = true
}
function gyroOff(note) {
  window.removeEventListener('deviceorientation', onDeviceOrientation)
  window.removeEventListener('deviceorientationabsolute', onDeviceOrientation)
  clearTimeout(gyroWatchdog)
  gyroOn.value = false
  if (note) showToast(note)
}
function toggleGyro() { gyroOn.value ? gyroOff() : enableGyro() }
function calibrate() {
  calibOffset.copy(camera.quaternion).invert()
  showToast('已校准视角')
}

/* ---- WebXR ---- */
async function enterVr() {
  if (renderer.xr.isPresenting) return
  try {
    const session = await navigator.xr.requestSession('immersive-vr', { optionalFeatures: ['local-floor'] })
    session.addEventListener('end', () => {
      video.pause()
      playing.value = false
    })
    await renderer.xr.setSession(session)
    video.play()
    playing.value = true
  } catch (err) {
    showToast('VR 会话启动失败：' + err.message)
  }
}
function onVisibilityChange() {
  if (document.hidden) { video.pause(); playing.value = false }
}

/* ---- hls.js ---- */
function attachHls(url) {
  if (hls) { hls.destroy(); hls = null }
  netRetries = 0; backoff = 1000
  if (Hls.isSupported()) {
    hls = new Hls({
      maxBufferLength: 30,
      capLevelToPlayerSize: false,
      // HLS 端点需 Bearer 鉴权
      xhrSetup: (xhr) => {
        const token = getAccessToken()
        if (token) xhr.setRequestHeader('Authorization', 'Bearer ' + token)
      }
    })
    hls.loadSource(url)
    hls.attachMedia(video)
    hls.on(Hls.Events.MANIFEST_PARSED, (_, data) => {
      const opts = []
      let maxAllowed = -1
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
      video.play().catch(() => {})
      playing.value = !video.paused
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
  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = url
    video.play().catch(() => {})
    playing.value = !video.paused
  } else {
    showOverlay('当前浏览器不支持 HLS', '请使用现代浏览器')
  }
}
function onQualityChange() {
  if (!hls) return
  hls.currentLevel = Number(qualityValue.value)
}

/* ---- 性能监测 ---- */
function fpsTick() {
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
}

/* ---- 播放控制 ---- */
function togglePlay() {
  video.paused ? video.play() : video.pause()
  playing.value = !video.paused
}
function toggleMute() {
  video.muted = !video.muted
  muted.value = video.muted
}
function onRateChange() { video.playbackRate = Number(rate.value) }
function fmt(t) {
  if (!isFinite(t)) return '00:00'
  const m = Math.floor(t / 60), s = Math.floor(t % 60)
  return `${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`
}
function onTimeUpdate() {
  if (video.duration) progressValue.value = Math.round((video.currentTime / video.duration) * 1000)
  timeText.value = `${fmt(video.currentTime)} / ${fmt(video.duration)}`
}
function onSeek() {
  if (video.duration) video.currentTime = (Number(progressValue.value) / 1000) * video.duration
}

/* ---- 控制栏自动隐藏 ---- */
function pokeBars() {
  barsHidden.value = false
  clearTimeout(hideTimer)
  hideTimer = setTimeout(() => { barsHidden.value = true }, 3000)
}

/* ---- overlay / toast ---- */
function showOverlay(msg, sub) {
  overlay.msg = msg; overlay.sub = sub || ''; overlay.show = true
}
function onRetry() {
  overlay.show = false
  attachHls(props.src)
}
function showToast(text) {
  statsText.value = text
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { statsText.value = ''; toastTimer = 0 }, 2500)
  pokeBars()
}

function onResize() {
  if (!renderer) return
  const w = containerRef.value.clientWidth, h = containerRef.value.clientHeight
  camera.aspect = w / h
  camera.updateProjectionMatrix()
  renderer.setSize(w, h)
}

onMounted(() => {
  video = videoRef.value
  const container = containerRef.value
  const w = container.clientWidth, h = container.clientHeight

  renderer = new THREE.WebGLRenderer({ antialias: true, powerPreference: 'high-performance' })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2))
  renderer.setSize(w, h)
  renderer.xr.enabled = true
  container.appendChild(renderer.domElement)
  el = renderer.domElement

  scene = new THREE.Scene()
  camera = new THREE.PerspectiveCamera(75, w / h, 0.1, 1000)

  texture = new THREE.VideoTexture(video)
  texture.flipY = false
  texture.colorSpace = THREE.SRGBColorSpace

  sphereGeo = new THREE.SphereGeometry(500, 64, 32)
  sphereGeo.scale(-1, 1, 1)
  sphereMat = new THREE.ShaderMaterial({
    uniforms: { map: { value: texture } },
    vertexShader: `
      varying vec2 vUv;
      void main() {
        vUv = uv;
        gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
      }`,
    fragmentShader: `
      uniform sampler2D map;
      varying vec2 vUv;
      void main() {
        gl_FragColor = texture2D(map, vUv);
      }`
  })
  scene.add(new THREE.Mesh(sphereGeo, sphereMat))

  maxTex = renderer.capabilities.maxTextureSize
  hevcSupported = typeof MediaSource !== 'undefined' &&
    (MediaSource.isTypeSupported('video/mp4; codecs="hvc1.1.6.L123.00"') ||
     MediaSource.isTypeSupported('video/mp4; codecs="hev1.1.6.L123.00"'))

  /* 事件绑定 */
  el.addEventListener('pointerdown', onPointerDown)
  el.addEventListener('pointermove', onPointerMove)
  el.addEventListener('pointerup', onPointerRelease)
  el.addEventListener('pointercancel', onPointerRelease)
  el.addEventListener('wheel', onWheel, { passive: false })
  el.addEventListener('touchstart', pokeBars, { passive: true })
  video.addEventListener('timeupdate', onTimeUpdate)
  document.addEventListener('visibilitychange', onVisibilityChange)
  window.addEventListener('resize', onResize)

  if (navigator.xr && navigator.xr.isSessionSupported) {
    navigator.xr.isSessionSupported('immersive-vr').then((ok) => { vrSupported.value = ok }).catch(() => {})
  }

  fpsWindowStart = performance.now()
  renderer.setAnimationLoop(() => {
    if (!gyroOn.value && !renderer.xr.isPresenting) {
      const phi = THREE.MathUtils.degToRad(90 - lat)
      const theta = THREE.MathUtils.degToRad(lon)
      camera.lookAt(
        500 * Math.sin(phi) * Math.cos(theta),
        500 * Math.cos(phi),
        500 * Math.sin(phi) * Math.sin(theta)
      )
    }
    fpsTick()
    renderer.render(scene, camera)
  })

  attachHls(props.src)
  pokeBars()
})

onBeforeUnmount(() => {
  disposed = true
  /* hls */
  if (hls) { hls.destroy(); hls = null }
  /* 陀螺仪 */
  gyroOff()
  /* 事件监听 */
  if (el) {
    el.removeEventListener('pointerdown', onPointerDown)
    el.removeEventListener('pointermove', onPointerMove)
    el.removeEventListener('pointerup', onPointerRelease)
    el.removeEventListener('pointercancel', onPointerRelease)
    el.removeEventListener('wheel', onWheel)
    el.removeEventListener('touchstart', pokeBars)
  }
  if (video) {
    video.removeEventListener('timeupdate', onTimeUpdate)
    video.pause()
    video.removeAttribute('src')
    video.load()
  }
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('resize', onResize)
  /* three */
  if (renderer) {
    renderer.setAnimationLoop(null)
    if (renderer.xr.isPresenting && renderer.xr.getSession()) {
      renderer.xr.getSession().end().catch(() => {})
    }
    if (sphereGeo) sphereGeo.dispose()
    if (sphereMat) sphereMat.dispose()
    if (texture) texture.dispose()
    renderer.dispose()
    renderer.forceContextLoss()
    renderer.domElement.remove()
    renderer = null
  }
  /* 定时器 */
  clearTimeout(hideTimer)
  clearTimeout(toastTimer)
  clearTimeout(gyroWatchdog)
})
</script>

<style scoped>
.pano-player {
  position: relative;
  width: 100%;
  height: 100%;
  overflow: hidden;
  background: #14181d;
  color: #f2f5f8;
  font-family: var(--font-family);
  --p-text-dim: #9aa7b4;
  --p-panel: rgba(20, 24, 29, 0.72);
}
.pano-container { position: absolute; inset: 0; touch-action: none; }
.pano-video { display: none; }

.bar {
  position: absolute; left: 0; right: 0; z-index: 10;
  display: flex; align-items: center; gap: 8px;
  padding: 8px 12px;
  background: var(--p-panel);
  backdrop-filter: blur(8px);
  transition: opacity 0.3s;
}
.bar.hidden { opacity: 0; pointer-events: none; }
.topbar { top: 0; padding-right: 56px; /* 为 PlayerView 右上退出按钮预留空间 */ }
.controls { bottom: 0; flex-wrap: wrap; }
.bar button, .bar select {
  background: transparent; color: #f2f5f8;
  border: 1px solid var(--p-text-dim);
  border-radius: var(--radius-md);
  padding: 6px 12px; font-size: var(--font-size-md);
  cursor: pointer; min-height: 36px;
}
.bar button:hover, .bar select:hover { border-color: var(--color-primary); color: var(--color-primary); }
.bar button.active { background: var(--color-primary); border-color: var(--color-primary); color: #fff; }
.bar button:disabled { opacity: 0.4; cursor: not-allowed; }
.title { font-size: var(--font-size-md); color: var(--p-text-dim); margin-right: auto; }
.stats { font-size: var(--font-size-sm); color: var(--p-text-dim); font-variant-numeric: tabular-nums; }

.progress-wrap { flex: 1 1 100%; display: flex; align-items: center; gap: 8px; order: -1; }
.progress {
  flex: 1; height: 6px; appearance: none; -webkit-appearance: none;
  background: var(--p-text-dim); border-radius: 3px; outline: none;
}
.progress::-webkit-slider-thumb {
  -webkit-appearance: none; width: 14px; height: 14px;
  border-radius: 50%; background: var(--color-primary); cursor: pointer;
}
.time {
  font-size: var(--font-size-sm); color: var(--p-text-dim);
  min-width: 90px; text-align: center; font-variant-numeric: tabular-nums;
}
.fov-label { font-size: var(--font-size-sm); color: var(--p-text-dim); }
.fov { width: 90px; }

.overlay {
  position: absolute; inset: 0; z-index: 20;
  display: none; align-items: center; justify-content: center;
  flex-direction: column; gap: 12px;
  background: var(--p-panel);
}
.overlay.show { display: flex; }
.overlay .msg { font-size: var(--font-size-lg); }
.overlay .sub { font-size: var(--font-size-sm); color: var(--p-text-dim); }
.overlay .retry {
  background: var(--color-primary); color: #fff; border: none;
  border-radius: var(--radius-md); padding: 10px 24px;
  font-size: var(--font-size-lg); cursor: pointer;
}
</style>
