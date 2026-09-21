<template>
  <div class="pano-player">
    <div ref="containerRef" class="pano-container"></div>
    <video ref="videoRef" crossorigin="anonymous" playsinline webkit-playsinline loop muted class="pano-video"></video>

    <div class="bar topbar" :class="{ hidden: barsHidden }">
      <span class="title">{{ title || (isPhoto ? '360 全景照片' : '360 全景播放') }}</span>
      <span class="stats">{{ statsText }}</span>
      <select v-if="!isPhoto" v-model="qualityValue" @change="onQualityChange">
        <option value="-1">自动</option>
        <option v-for="q in qualityOptions" :key="q.value" :value="q.value" :disabled="q.disabled">
          {{ q.label }}
        </option>
      </select>
    </div>

    <div class="bar controls" :class="{ hidden: barsHidden }">
      <div v-if="!isPhoto" class="progress-wrap">
        <span class="time">{{ timeText }}</span>
        <input type="range" class="progress" min="0" max="1000" v-model="progressValue" @input="onSeek">
      </div>
      <button v-if="!isPhoto" @click="togglePlay">{{ playing ? '暂停' : '播放' }}</button>
      <button v-if="!isPhoto" :class="{ active: !muted }" @click="toggleMute">音量</button>
      <select v-if="!isPhoto" v-model="rate" @change="onRateChange">
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
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import * as THREE from 'three'
import Hls from 'hls.js'
import { getAccessToken } from '../../utils/tokenStore'

const props = defineProps({
  mediaId: { type: String, required: true },
  src: { type: String, required: true }, // video: hls master 完整 URL；photo: 图片 URL
  title: { type: String, default: '' },
  mode: { type: String, default: 'video' }, // video | photo（360 照片球面渲染）
  auth: { type: String, default: 'bearer' }, // bearer（主站 HLS 带 token）| none（分享公开端点免鉴权）
  appendQuery: { type: String, default: '' }, // 追加到每个 HLS 请求 URL 的查询串（如分享密码 password=xxx）
  // Phase 4 P1：实测下行带宽（kbps）。0 = 未知/测速失败 → 完全不干预，用 hls.js 默认 ABR。
  bandwidthKbps: { type: Number, default: 0 }
})
const isPhoto = computed(() => props.mode === 'photo')

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
/* 平滑（R2/R4）：事件回调只写 targetQuat，相机由 rAF 统一 slerp 写入。
   GYRO_SLEW = 每帧向目标朝向逼近的比例，0.2~0.3 兼顾跟手与平滑：越小越平滑、跟随延迟越大。 */
const GYRO_SLEW = 0.25
const GYRO_WATCHDOG_MS = 1500  // 首轮等待：传感器首次出数可能略慢
const GYRO_SWAP_MS = 800       // 换源后第二轮等待：另一个事件源注册后通常立即出数
const targetQuat = new THREE.Quaternion()
const gyroRawQuat = new THREE.Quaternion()
const tmpQuat = new THREE.Quaternion()
const gyroDir = new THREE.Vector3()
let gyroHasTarget = false
let gyroEventName = ''            // 当前实际监听的事件名（两者只注册其一）
let gyroTriedFallback = false     // 看门狗是否已尝试换源
let gyroNeedInitialCalib = false  // 开启后等待首个样本建立补偿（R6）

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
// 结果写入 out（不新建 Quaternion）：陀螺仪事件每秒数十次，避免持续分配。
function orientToQuat(out, alpha, beta, gamma) {
  const d = Math.PI / 180
  euler.set(beta * d, alpha * d, -gamma * d, 'YXZ')
  out.setFromEuler(euler)
  out.multiply(q1)
  out.multiply(q0.setFromAxisAngle(zee, -screenOrientationAngle() * d))
  return out
}
function onDeviceOrientation(e) {
  const a = e.alpha, b = e.beta, g = e.gamma
  // R3：α/β/γ 任一缺失或非有限数都直接丢弃该事件。α 的合法取值包含 0，
  // 用 `|| 0` 兜底会把「缺失」误当成「0° 真实朝向」，相机在真实朝向与 0° 之间反复跳。
  if (a == null || b == null || g == null) return
  if (!Number.isFinite(a) || !Number.isFinite(b) || !Number.isFinite(g)) return
  gyroGotData = true
  orientToQuat(gyroRawQuat, a, b, g)
  if (gyroNeedInitialCalib) {
    gyroNeedInitialCalib = false
    // R6：首个样本建立补偿，等价于开启瞬间按一次「校准」——
    // offset = 当前视角 × 传感器朝向⁻¹，于是 offset × q_raw === 当前视角，开启瞬间零跳变。
    calibOffset.copy(camera.quaternion).multiply(tmpQuat.copy(gyroRawQuat).invert())
  }
  // R2/R4：回调只更新目标朝向，相机统一由 rAF 插值写入（不再逐事件直灌，也避免同帧多次写/读到中间态）。
  targetQuat.copy(gyroRawQuat).premultiply(calibOffset)
  gyroHasTarget = true
}
// R1：deviceorientation 与 deviceorientationabsolute 参考系不同（α 相差常量偏置），
// Android Chrome 会同时派发两者 —— 两个事件交替命中同一 handler，相机在「两个朝向」之间
// 高频交替，这是画面频繁闪动的根因。因此二者只注册其一：优先 absolute（磁力计真北参考系，
// 无累积漂移），能力探测不可用则用 relative。探测可能失真（存在但永不到达 / alpha 恒 null），
// 由看门狗换源兜底。
function gyroAbsoluteAvailable() {
  return 'ondeviceorientationabsolute' in window
}
function attachGyroListener(name) {
  window.addEventListener(name, onDeviceOrientation)
  gyroEventName = name
}
function detachGyroListener() {
  if (!gyroEventName) return
  window.removeEventListener(gyroEventName, onDeviceOrientation)
  gyroEventName = ''
}
function armGyroWatchdog(ms) {
  clearTimeout(gyroWatchdog)
  gyroWatchdog = setTimeout(() => {
    if (gyroGotData) return
    if (!gyroTriedFallback) {
      // 第一次超时先换成另一个事件源再等一轮（比直接降级保守），仍无有效数据才降级。
      gyroTriedFallback = true
      const alt = gyroEventName === 'deviceorientationabsolute' ? 'deviceorientation' : 'deviceorientationabsolute'
      detachGyroListener()
      attachGyroListener(alt)
      armGyroWatchdog(GYRO_SWAP_MS)
      return
    }
    gyroOff(isWeChat ? '微信浏览器未提供陀螺仪数据，已降级为拖拽模式' : '未检测到陀螺仪数据，已降级为拖拽模式')
  }, ms)
}
function attachGyroListeners() {
  gyroGotData = false
  gyroTriedFallback = false
  detachGyroListener()
  attachGyroListener(gyroAbsoluteAvailable() ? 'deviceorientationabsolute' : 'deviceorientation')
  armGyroWatchdog(GYRO_WATCHDOG_MS)
}
async function enableGyro() {
  if (typeof DeviceOrientationEvent !== 'undefined' && typeof DeviceOrientationEvent.requestPermission === 'function') {
    try {
      const res = await DeviceOrientationEvent.requestPermission()
      if (res !== 'granted') { gyroOff('权限被拒，已降级为拖拽模式'); return }
    } catch { gyroOff('权限请求失败，已降级为拖拽模式'); return }
  }
  attachGyroListeners()
  // R6：置 gyroOn 之前先把插值目标对齐当前视角，使「开启瞬间」与「首个样本到达」两个时刻视角都连续。
  gyroNeedInitialCalib = true
  targetQuat.copy(camera.quaternion)
  gyroHasTarget = true
  gyroOn.value = true
}
// R5：把当前相机朝向按 lookAt 的逆映射反解回 lon/lat，供关闭陀螺仪/看门狗降级时 rAF 无缝接管，
// 避免视角瞬跳。（lookAt 的注视方向 dir = (sinφcosθ, cosφ, sinφ sinθ)，φ=deg2rad(90-lat)，θ=deg2rad(lon)）
function syncLonLatFromCamera() {
  gyroDir.set(0, 0, -1).applyQuaternion(camera.quaternion)
  const dy = Math.max(-1, Math.min(1, gyroDir.y))
  lat = Math.max(-89.9, Math.min(89.9, 90 - THREE.MathUtils.radToDeg(Math.acos(dy))))
  const sinPhi = Math.sqrt(Math.max(0, 1 - dy * dy))
  if (sinPhi > 1e-4) lon = THREE.MathUtils.radToDeg(Math.atan2(gyroDir.z, gyroDir.x))
}
function gyroOff(note) {
  const wasOn = gyroOn.value
  detachGyroListener()
  clearTimeout(gyroWatchdog)
  gyroOn.value = false
  gyroHasTarget = false
  gyroNeedInitialCalib = false
  if (wasOn && camera) syncLonLatFromCamera()
  if (note) showToast(note)
}
function toggleGyro() { gyroOn.value ? gyroOff() : enableGyro() }
function calibrate() {
  if (gyroOn.value && gyroHasTarget) {
    // 校准 = 把当前视角重设为当前传感器朝向的参考（offset = 当前视角 × q_raw⁻¹），
    // 与 R6 的开启补偿同一语义，因此按「校准」本身不会移动画面。
    calibOffset.copy(camera.quaternion).multiply(tmpQuat.copy(gyroRawQuat).invert())
    targetQuat.copy(gyroRawQuat).premultiply(calibOffset)
    gyroHasTarget = true
  } else {
    calibOffset.copy(camera.quaternion).invert()
  }
  showToast('已校准视角')
}

/* ---- WebXR ---- */
async function enterVr() {
  if (renderer.xr.isPresenting) return
  try {
    const session = await navigator.xr.requestSession('immersive-vr', { optionalFeatures: ['local-floor'] })
    session.addEventListener('end', () => {
      if (!isPhoto.value) { video.pause(); playing.value = false }
    })
    await renderer.xr.setSession(session)
    if (!isPhoto.value) { video.play().catch(() => {}); playing.value = true }
  } catch (err) {
    showToast('VR 会话启动失败：' + err.message)
  }
}
function onVisibilityChange() {
  if (!isPhoto.value && document.hidden) { video.pause(); playing.value = false }
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

function attachHls(url) {
  if (hls) { hls.destroy(); hls = null }
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
    // 原生 HLS（iOS Safari / 微信 iOS，无 MSE）：无法给 master 请求加 Authorization，
    // 也无法像 xhrSetup 那样给 m3u8 相对路径的 ts 切片逐个补挂查询串。
    // 因此受保护内容（主站 Bearer / 密码分享）在该分支必然 401/403 —— 不得静默失败，
    // 明确提示换设备。长期方案：后端为切片签发一次性查询令牌（签名 URL），
    // 使切片请求自带鉴权，前端无需注入。
    if (props.auth === 'bearer' || props.appendQuery) {
      showOverlay('当前浏览器不支持受保护视频播放', '请用桌面或 Android 设备观看')
      return
    }
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

/* ---- 360 照片：TextureLoader 贴球（与视频共用球体/交互/陀螺仪/VR） ---- */
function attachPhoto(url) {
  statsText.value = '加载中…'
  new THREE.TextureLoader().load(
    url,
    (tex) => {
      if (disposed) { tex.dispose(); return }
      // SphereGeometry 顶部 UV v=1 → 图片顶行；flipY=true 时 v=1 恰为顶行，天顶朝上（与初始纹理约定一致）
      tex.flipY = true
      tex.colorSpace = THREE.SRGBColorSpace
      const old = texture
      texture = tex
      sphereMat.uniforms.map.value = tex
      if (old) old.dispose()
      showToast('全景照片已加载')
    },
    undefined,
    () => { showOverlay('图片加载失败', '请检查网络后重试') }
  )
}

/* ---- 按媒体形态分派加载：照片贴图 / 视频 HLS ---- */
function attachSource() {
  if (isPhoto.value) attachPhoto(props.src)
  else attachHls(props.src)
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
  attachSource()
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

  texture = isPhoto.value ? new THREE.Texture() : new THREE.VideoTexture(video)
  // SphereGeometry 顶部 UV v=1 → 图片/帧顶行；flipY=true 时 v=1 恰为顶行，天顶朝上。
  // （此前视频路径 flipY=false 为合成素材期未暴露的朝向缺陷，本次一并修正）
  texture.flipY = true
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
    if (gyroOn.value && gyroHasTarget && !renderer.xr.isPresenting) {
      // R2/R4：传感器只在回调里更新 targetQuat，相机在这里统一插值，噪声不再逐事件直灌相机。
      // 取舍：跟随引入约 1~2 帧延迟（多数场景下眩晕感反而更轻）；GYRO_SLEW 越小越平滑、延迟越大。
      camera.quaternion.slerp(targetQuat, GYRO_SLEW)
    } else if (!gyroOn.value && !renderer.xr.isPresenting) {
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

  attachSource()
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
  background: var(--player-bg);
  color: var(--player-text);
  font-family: var(--font-family);
  --p-text-dim: var(--player-text-dim);
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
  background: transparent; color: var(--player-text);
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
