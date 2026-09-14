<template>
  <div class="share-public">
    <!-- 加载中 -->
    <div v-if="phase === 'loading'" class="center-box">
      <div class="spinner"></div>
      <p class="tip">加载中…</p>
    </div>

    <!-- 密码输入页 -->
    <div v-else-if="phase === 'password'" class="center-box">
      <div class="pwd-card">
        <div class="pwd-icon">
          <svg viewBox="0 0 24 24" width="28" height="28" fill="none">
            <rect x="5" y="10" width="14" height="10" rx="2" stroke="currentColor" stroke-width="1.6" />
            <path d="M8 10V7a4 4 0 018 0v3" stroke="currentColor" stroke-width="1.6" />
            <circle cx="12" cy="15" r="1.4" fill="currentColor" />
          </svg>
        </div>
        <h2 class="pwd-title">该分享已设置访问密码</h2>
        <p class="pwd-desc">请输入分享者提供的 4-8 位密码</p>
        <input
          v-model.trim="passwordInput"
          class="pwd-input"
          type="password"
          inputmode="numeric"
          maxlength="8"
          placeholder="请输入访问密码"
          @keyup.enter="submitPassword"
        />
        <p v-if="passwordError" class="pwd-error">{{ passwordError }}</p>
        <button class="primary-btn" :disabled="!passwordInput || passwordChecking" @click="submitPassword">
          {{ passwordChecking ? '验证中…' : '查看分享' }}
        </button>
      </div>
    </div>

    <!-- 错误页 -->
    <div v-else-if="phase === 'error'" class="center-box">
      <div class="error-icon">
        <svg viewBox="0 0 24 24" width="36" height="36" fill="none">
          <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.5" />
          <path d="M12 7.5V13M12 16.2v.01" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
      </div>
      <h2 class="error-title">{{ errorInfo.title }}</h2>
      <p class="error-desc">{{ errorInfo.desc }}</p>
    </div>

    <!-- 分享内容 -->
    <template v-else-if="phase === 'ready'">
      <header class="share-header">
        <h1 class="share-title">{{ share.title || '全景相册分享' }}</h1>
        <p class="share-meta">{{ share.items.length }} 项 · 仅支持在线浏览</p>
      </header>

      <div v-if="!share.items.length" class="center-box">
        <p class="tip">分享内容为空</p>
      </div>

      <div v-else class="media-grid">
        <button
          v-for="m in share.items"
          :key="m.id"
          class="media-cell"
          type="button"
          @click="openItem(m)"
        >
          <img v-if="thumbOf(m.id) && thumbOf(m.id) !== 'x-failed'" :src="thumbOf(m.id)" :alt="m.filename || ''" class="thumb" loading="lazy" />
          <div v-else-if="thumbOf(m.id) === 'x-failed'" class="thumb-placeholder">
            <svg viewBox="0 0 24 24" width="26" height="26" fill="none" class="thumb-failed-icon">
              <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
              <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
            </svg>
          </div>
          <div v-else class="thumb-placeholder">
            <div class="spinner sm"></div>
          </div>
          <span v-if="isVideo(m)" class="badge video-badge">
            <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
              <path d="M8 5.5v13l11-6.5z" />
            </svg>
            {{ durationLabel(m) }}
          </span>
          <span v-if="is360(m)" class="badge pano-badge">360</span>
        </button>
      </div>

      <!-- 照片大图 -->
      <div v-if="viewerItem" class="viewer-mask" @click.self="closeViewer">
        <button class="viewer-close" type="button" aria-label="关闭" @click="closeViewer">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
        <img v-if="viewerUrl" :src="viewerUrl" :alt="viewerItem.filename || ''" class="viewer-img" />
        <div v-else class="spinner"></div>
      </div>

      <!-- 视频 / 360 播放 -->
      <div v-if="playingItem" class="viewer-mask" @click.self="closePlayer">
        <button class="viewer-close" type="button" aria-label="关闭" @click="closePlayer">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
        <div class="player-box">
          <video ref="videoEl" class="player-video" controls autoplay playsinline webkit-playsinline></video>
          <p v-if="playerError" class="player-error">{{ playerError }}</p>
        </div>
      </div>

      <!-- 360 全景（照片/视频统一球面渲染） -->
      <div v-if="panoItem" class="viewer-mask" @click.self="closePano">
        <button class="viewer-close" type="button" aria-label="关闭" @click="closePano">
          <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
        <div class="pano-box">
          <Player360
            v-if="panoReady"
            :key="panoItem.id"
            :media-id="String(panoItem.id)"
            :mode="panoItem.type === 'photo' ? 'photo' : 'video'"
            :src="panoSrc"
            :title="panoItem.filename || ''"
            auth="none"
            :append-query="panoAppendQuery"
            :bandwidth-kbps="panoBandwidthKbps"
          />
          <div v-else class="pano-loading">
            <div class="spinner"></div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import Hls from 'hls.js'
import { fetchPublicShare, loadPublicThumb, measureShareBandwidth, publicHlsUrl, publicThumbUrl } from '../components/shares/publicApi'
import Player360 from '../components/player/360Player.vue'

const route = useRoute()
const token = route.params.token

const phase = ref('loading') // loading | password | error | ready
const share = ref(null)
const password = ref('') // 验证通过的密码，用于后续媒体请求

const passwordInput = ref('')
const passwordError = ref('')
const passwordChecking = ref(false)

const errorInfo = reactive({ title: '', desc: '' })

const thumbs = reactive(new Map())

const viewerItem = ref(null)
const viewerUrl = ref('')

const playingItem = ref(null)
const playerError = ref('')
const videoEl = ref(null)
let hls = null
let alive = true

/* 360 全景查看器状态：照片取 lg 缩略图贴球面，视频走公开 HLS */
const panoItem = ref(null)
/* Phase 4 P1：带宽自测结束前先不挂载 360 播放器（避免用错档位起播） */
const panoReady = ref(false)
/* 实测下行带宽（kbps）；0 = 未知/测速失败 → 360Player 走 hls.js 默认 ABR */
const panoBandwidthKbps = ref(0)
/* 带宽自测等待上限：超时先播放，拿不到提示就交给 hls.js 默认 ABR（绝不无限阻塞） */
const BW_HINT_TIMEOUT_MS = 1500
/* 用户快速切换媒体时，丢弃上一轮迟到的测量结果 */
let panoSeq = 0

const panoSrc = computed(() => {
  if (!panoItem.value) return ''
  if (panoItem.value.type === 'photo') return publicThumbUrl(token, panoItem.value.id, 'lg', password.value)
  return publicHlsUrl(token, panoItem.value.id, password.value)
})
// 密码分享的 HLS ts 切片请求不继承 master URL 查询串，需逐请求补挂
const panoAppendQuery = computed(() => (password.value ? `password=${encodeURIComponent(password.value)}` : ''))

// preparePano 打开 360 媒体：照片无需 HLS，立即进播放器；
// 视频先做带宽自测（有上界），据测速值让 360Player 选初始档位。
async function preparePano(item) {
  panoItem.value = item
  panoBandwidthKbps.value = 0
  if (item.type === 'photo') {
    panoReady.value = true
    return
  }
  panoReady.value = false
  const seq = ++panoSeq
  const measured = measureShareBandwidth(token, password.value)
    .then((r) => (r && Number(r.down_kbps) > 0 ? Number(r.down_kbps) : 0))
    .catch(() => 0) // 测速失败 → 0 → 360Player 走 hls.js 默认 ABR
  const hint = await Promise.race([
    measured,
    new Promise((resolve) => setTimeout(() => resolve(0), BW_HINT_TIMEOUT_MS))
  ])
  if (seq !== panoSeq || !alive) return // 已切走/已卸载：丢弃
  panoBandwidthKbps.value = hint
  panoReady.value = true
}

function isVideo(m) {
  // 360 视频走全景播放，不进普通播放器
  return m.type === 'video' && !is360(m)
}

function is360(m) {
  // MediaRef.type 恒为枚举原值（photo|video），360 以独立 is_360 标记；'360' 兼容旧契约
  return !!m.is_360 || m.type === '360'
}

function thumbOf(id) {
  return thumbs.get(id) || ''
}

function durationLabel(m) {
  const d = Number(m.duration)
  if (!d || !Number.isFinite(d)) return ''
  const mm = Math.floor(d / 60)
  const ss = Math.floor(d % 60)
  return `${mm}:${String(ss).padStart(2, '0')}`
}

function showError(code, status) {
  const map = {
    EXPIRED: { title: '分享已过期', desc: '该链接已超过有效期，请联系分享者重新分享' },
    NOT_FOUND: { title: '链接不存在', desc: '该分享链接不存在或已被分享者吊销' },
    MAX_VIEWS: { title: '访问次数已达上限', desc: '该分享的浏览次数已用完，请联系分享者' },
    WRONG_PASSWORD: { title: '需要访问密码', desc: '请向分享者获取访问密码' }
  }
  // 后端异常体可能为空：404 一律按链接失效处理
  const key = code || (status === 404 ? 'NOT_FOUND' : '')
  const info = map[key] || { title: '加载失败', desc: '网络异常或服务器繁忙，请稍后重试' }
  errorInfo.title = info.title
  errorInfo.desc = info.desc
  phase.value = 'error'
  document.title = '分享不可访问'
}

// 403 且无法取得更精确错误码时，按「需要/错误密码」处理（后端密码类 403 可能无响应体）
function isPasswordError(e) {
  return e.status === 403 && (!e.code || e.code === 'WRONG_PASSWORD' || e.code === 'PASSWORD_REQUIRED')
}

async function loadShare(pwd) {
  const data = await fetchPublicShare(token, pwd)
  if (data.require_password && !pwd) {
    phase.value = 'password'
    return
  }
  password.value = pwd || ''
  share.value = { ...data, items: Array.isArray(data.items) ? data.items : [] }
  phase.value = 'ready'
  document.title = data.title || '全景相册分享'
  loadThumbs()
}

async function loadThumbs() {
  for (const m of share.value.items) {
    try {
      const url = await loadPublicThumb(token, m.id, 'md', password.value)
      if (alive) thumbs.set(m.id, url)
    } catch (e) {
      // 单个缩略图失败不阻塞整体，标记为失败态（显示占位图标而非一直转圈）
      if (alive) thumbs.set(m.id, 'x-failed')
    }
  }
}

async function submitPassword() {
  if (!passwordInput.value || passwordChecking.value) return
  passwordChecking.value = true
  passwordError.value = ''
  try {
    await loadShare(passwordInput.value)
  } catch (e) {
    if (isPasswordError(e)) {
      passwordError.value = '密码错误，请重新输入'
    } else {
      showError(e.code, e.status)
    }
  } finally {
    passwordChecking.value = false
  }
}

async function openItem(m) {
  if (is360(m)) {
    return preparePano(m) // 360 照片/视频统一进全景查看器（视频需先测速定初始档）
  }
  if (isVideo(m)) {
    openPlayer(m)
  } else {
    viewerItem.value = m
    viewerUrl.value = ''
    try {
      const url = await loadPublicThumb(token, m.id, 'lg', password.value)
      if (alive && viewerItem.value === m) viewerUrl.value = url
    } catch (e) {
      if (alive && viewerItem.value === m) viewerUrl.value = thumbOf(m.id)
    }
  }
}

function closeViewer() {
  viewerItem.value = null
  viewerUrl.value = ''
}

async function openPlayer(m) {
  playingItem.value = m
  playerError.value = ''
  await nextTick()
  const v = videoEl.value
  if (!v) return
  const url = publicHlsUrl(token, m.id, password.value)
  if (Hls.isSupported()) {
    hls = new Hls({
      enableWorker: true,
      // 密码分享：m3u8 相对路径的 ts 切片请求不继承 master URL 查询串，逐请求补挂 password
      xhrSetup: (xhr, url) => {
        if (password.value) {
          const sep = url.includes('?') ? '&' : '?'
          xhr.open('GET', url + sep + 'password=' + encodeURIComponent(password.value), true)
        }
      }
    })
    hls.on(Hls.Events.ERROR, (_evt, data) => {
      if (data?.fatal) playerError.value = '视频加载失败，请稍后重试'
    })
    hls.loadSource(url)
    hls.attachMedia(v)
  } else if (v.canPlayType('application/vnd.apple.mpegurl')) {
    // iOS Safari / 微信内置浏览器原生 HLS
    v.src = url
  } else {
    playerError.value = '当前浏览器不支持视频播放'
  }
}

function destroyPlayer() {
  if (hls) {
    hls.destroy()
    hls = null
  }
  const v = videoEl.value
  if (v) {
    v.pause()
    v.removeAttribute('src')
    v.load()
  }
}

function closePlayer() {
  destroyPlayer()
  playingItem.value = null
  playerError.value = ''
}

function closePano() {
  panoSeq++ // 作废在途的带宽测量，避免其回填后重新挂载播放器
  panoReady.value = false
  panoBandwidthKbps.value = 0
  panoItem.value = null // v-if 卸载即触发 Player360 自身资源销毁
}

onMounted(async () => {
  document.title = '全景相册分享'
  // OG 中转页会带 ?spa=1 跳进来（见 internal/shares/og.go 与 docker/web/Dockerfile 的 /share 分流）。
  // 该参数只对「第一次请求要绕过抓取器判别」有意义，进入 SPA 后必须立刻从地址栏抹掉：
  // 否则用户在微信里用「··· → 发送给朋友」转发时会把 ?spa=1 一起带出去，
  // 下一个接收者的**抓取器**命中「强制 SPA」分支 → 拿不到 og:* → 分享卡片退化成纯标题。
  // 保留其余 query（如 ?password=），顺序不变。
  if (route.query.spa) {
    const rest = { ...route.query }
    delete rest.spa
    const qs = new URLSearchParams(rest).toString()
    window.history.replaceState({}, '', route.path + (qs ? '?' + qs : ''))
  }
  try {
    await loadShare('')
  } catch (e) {
    if (isPasswordError(e)) {
      phase.value = 'password'
    } else {
      showError(e.code, e.status)
    }
  }
})

onBeforeUnmount(() => {
  alive = false
  panoSeq++
  destroyPlayer()
  for (const url of thumbs.values()) URL.revokeObjectURL(url)
  thumbs.clear()
})
</script>

<style scoped>
.share-public {
  min-height: 100vh;
  background-color: var(--color-bg);
  font-family: var(--font-family);
  padding-bottom: 40px;
}

.center-box {
  min-height: 70vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  padding: 24px;
}

.tip {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--color-border);
  border-top-color: var(--color-primary);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.spinner.sm {
  width: 20px;
  height: 20px;
  border-width: 2px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

/* 密码页 */
.pwd-card {
  width: 320px;
  max-width: calc(100vw - 48px);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 28px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
}

.pwd-icon {
  width: 52px;
  height: 52px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
  margin-bottom: 14px;
}

.pwd-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 6px;
}

.pwd-desc {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 16px;
}

.pwd-input {
  width: 100%;
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-lg);
  text-align: center;
  letter-spacing: 2px;
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}

.pwd-input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.pwd-error {
  margin-top: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.primary-btn {
  width: 100%;
  margin-top: 14px;
  padding: 10px;
  border: none;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: #fff;
  background-color: var(--color-primary);
  cursor: pointer;
}

.primary-btn:hover {
  background-color: var(--color-primary-hover);
}

.primary-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* 错误页 */
.error-icon {
  color: var(--color-text-disabled);
}

.error-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.error-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  text-align: center;
}

/* 内容页 */
.share-header {
  padding: 20px 16px 12px;
}

.share-title {
  font-size: 18px;
  color: var(--color-text-primary);
  word-break: break-word;
}

.share-meta {
  margin-top: 4px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.media-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 3px;
  padding: 0 3px;
}

.media-cell {
  position: relative;
  aspect-ratio: 1 / 1;
  padding: 0;
  border: none;
  background-color: var(--color-surface-hover);
  cursor: pointer;
  overflow: hidden;
}

.thumb {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.thumb-failed-icon {
  color: var(--color-text-disabled);
}

.badge {
  position: absolute;
  display: flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 10px;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
}

.video-badge {
  right: 4px;
  bottom: 4px;
}

.pano-badge {
  left: 4px;
  top: 4px;
  background-color: var(--color-primary);
}

/* 查看器 / 播放器 */
.viewer-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.9);
}

.viewer-close {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 210;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: rgba(255, 255, 255, 0.15);
  cursor: pointer;
}

.viewer-img {
  max-width: 100vw;
  max-height: 100vh;
  object-fit: contain;
}

.player-box {
  width: 100%;
  max-width: 720px;
  display: flex;
  flex-direction: column;
  align-items: center;
}

.player-video {
  width: 100%;
  max-height: 80vh;
  background-color: #000;
}

/* 360 球面渲染容器：占满可视区（Player360 内部自带控制栏） */
.pano-box {
  position: relative;
  width: 94vw;
  height: 84vh;
  max-width: 1200px;
  background-color: #14181d;
  border-radius: var(--radius-md);
  overflow: hidden;
}

.pano-note {
  margin-top: 10px;
  padding: 0 16px;
  font-size: var(--font-size-sm);
  color: rgba(255, 255, 255, 0.75);
  text-align: center;
}

/* 带宽自测期间的占位（有上限，不会长期停留） */
.pano-loading {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}

.player-error {
  margin-top: 10px;
  font-size: var(--font-size-sm);
  color: #fca5a5;
}
</style>
