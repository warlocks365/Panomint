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
        <p class="share-meta">{{ share.items.length }} 项{{ share.allow_download ? ' · 已开放下载' : ' · 仅支持在线浏览' }}</p>
      </header>

      <div v-if="!share.items.length" class="center-box">
        <p class="tip">分享内容为空</p>
      </div>

      <ShareMediaGrid
        v-else
        :items="share.items"
        :thumb-url="thumbOf"
        :token="token"
        :password="password"
        :allow-download="!!share.allow_download"
        @open="openItem"
      />

      <!-- 照片大图 -->
      <SharePhotoViewer
        v-if="viewerItem"
        :item="viewerItem"
        :token="token"
        :password="password"
        :fallback-thumb-url="thumbOf(viewerItem.id)"
        @close="viewerItem = null"
      />

      <!-- 视频 / 360 播放 -->
      <ShareVideoPlayer
        v-if="playingItem"
        :item="playingItem"
        :token="token"
        :password="password"
        @close="playingItem = null"
      />

      <!-- 360 全景（照片/视频统一球面渲染） -->
      <SharePanoViewer
        v-if="panoItem"
        :item="panoItem"
        :token="token"
        :password="password"
        @close="panoItem = null"
      />
    </template>
  </div>
</template>

<script setup>
// Job000084 拆解：网格/照片查看器/HLS 播放器/360 全景四个部分拆为独立组件，
// 各自自持挂载即工作、卸载即销毁的完整生命周期（objectURL/hls.js/带宽自测不外泄）。
// 本组件保留：phase 状态机、密码提交、错误映射、缩略图有限并发加载与回收、openItem 分派。
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { fetchPublicShare, loadPublicThumb } from '../components/shares/publicApi'
import ShareMediaGrid from '../components/shares/ShareMediaGrid.vue'
import SharePhotoViewer from '../components/shares/SharePhotoViewer.vue'
import ShareVideoPlayer from '../components/shares/ShareVideoPlayer.vue'
import SharePanoViewer from '../components/shares/SharePanoViewer.vue'

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

/* 三个查看器各自 v-if 挂载即工作；宿主只持有「当前打开哪类」的分派状态 */
const viewerItem = ref(null)
const playingItem = ref(null)
const panoItem = ref(null)
let alive = true

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
  // 有限并发（6）：串行 N 张 = N 个 RTT 太慢，全并发又打爆连接/触发限流
  const CONCURRENCY = 6
  const items = share.value.items
  let next = 0
  async function worker() {
    while (next < items.length && alive) {
      const m = items[next++]
      try {
        const url = await loadPublicThumb(token, m.id, 'md', password.value)
        if (alive) thumbs.set(m.id, url)
      } catch (e) {
        // 单个缩略图失败不阻塞整体，标记为失败态（显示占位图标而非一直转圈）
        if (alive) thumbs.set(m.id, 'x-failed')
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(CONCURRENCY, items.length) }, worker))
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
    panoItem.value = m // 360 照片/视频统一进全景查看器（视频需先测速定初始档）
  } else if (isVideo(m)) {
    playingItem.value = m
  } else {
    viewerItem.value = m
  }
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
</style>
