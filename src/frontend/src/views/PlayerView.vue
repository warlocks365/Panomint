<template>
  <div class="player-view">
    <!-- 舞台区：按类型渲染核心（照片 / 普通视频 / 360 全景直接球面渲染） -->
    <div class="stage-area">
      <div v-if="loading" class="state">加载中…</div>
      <div v-else-if="error" class="state error">{{ error }}</div>

      <!-- 照片：直接显示原图 -->
      <div v-else-if="mode === 'photo'" class="photo-wrap">
        <img v-if="photoUrl" :src="photoUrl" :alt="detail?.filename || '照片'" class="photo">
      </div>

      <!-- 360：照片 → 球面渲染（TextureLoader）；视频已转码 → 全景播放 -->
      <Player360
        v-else-if="mode === 'pano' && panoReady"
        :key="panoSrc"
        :media-id="mediaId"
        :mode="panoKind"
        :src="panoSrc"
        :title="detail?.filename || ''"
        class="pano"
      />

      <!-- 360 视频：未转码 → 提示并发起转码（照片无需转码） -->
      <div v-else-if="mode === 'pano' && panoKind === 'video'" class="state transcode">
        <template v-if="!transcode.jobId && !transcode.failed">
          <div class="msg">该 360 视频尚未转码</div>
          <div class="sub">转码为 HLS 多码率流后才能全景播放</div>
          <button class="primary" :disabled="transcode.starting" @click="startTranscode">
            {{ transcode.starting ? '发起中…' : '发起转码（1080p）' }}
          </button>
        </template>
        <template v-else-if="transcode.failed">
          <div class="msg">转码失败</div>
          <button class="primary" @click="startTranscode">重新发起转码</button>
        </template>
        <template v-else>
          <div class="msg">转码中…（{{ transcode.status }}）</div>
          <div class="sub">完成后将自动加载播放</div>
        </template>
      </div>

      <!-- 普通视频 -->
      <div v-else class="video-wrap">
        <video ref="plainVideoRef" controls playsinline class="plain-video"></video>
      </div>

      <!-- 退出：移动端返回按钮（左上） + 右上 ×（全端） -->
      <button v-if="!isDesktop" class="exit-btn back-btn" title="返回" @click="exit">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
          <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button class="exit-btn close-btn" title="退出查看器（Esc）" @click="exit">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
          <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
      </button>
      <button v-if="!isDesktop" class="exit-btn info-btn" title="媒体信息" @click="drawerOpen = true">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
          <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
          <path d="M12 11v5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          <circle cx="12" cy="7.8" r="1.1" fill="currentColor" />
        </svg>
      </button>
    </div>

    <!-- 桌面端：右侧信息窗格平铺 -->
    <MediaInfoPanel
      v-if="isDesktop"
      :media-id="mediaId"
      :detail="detail"
      :loading="loading"
      :show-close="false"
      @deleted="exit"
    />

    <!-- 移动端：信息抽屉 -->
    <div v-if="!isDesktop && drawerOpen" class="drawer-mask" @click.self="drawerOpen = false">
      <div class="drawer">
        <MediaInfoPanel
          :media-id="mediaId"
          :detail="detail"
          :loading="loading"
          @close="drawerOpen = false"
          @deleted="exit"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Hls from 'hls.js'
import http from '../api/http'
import { getAccessToken } from '../utils/tokenStore'
import { useResponsive } from '../composables/useResponsive'
import Player360 from '../components/player/360Player.vue'
import MediaInfoPanel from '../components/player/MediaInfoPanel.vue'

const route = useRoute()
const router = useRouter()
const { isDesktop } = useResponsive()
const mediaId = computed(() => String(route.params.id || ''))

const loading = ref(true)
const error = ref('')
const detail = ref(null)
const pano = ref(null)
const mode = ref('') // photo | pano | video
const hlsUrl = ref('')
const photoUrl = ref('')
const panoPhotoUrl = ref('') // 360 照片：原图 blob URL（球面贴图）
const plainVideoRef = ref(null)
const transcode = ref({ jobId: '', status: '', starting: false, failed: false })
const drawerOpen = ref(false)

let pollTimer = 0
let plainHls = null
let blobUrls = []

const API_BASE = http.defaults.baseURL

/* 360 播放属性：照片走 TextureLoader，视频走 HLS */
const panoKind = computed(() => (detail.value?.type === 'photo' ? 'photo' : 'video'))
const panoSrc = computed(() => (panoKind.value === 'photo' ? panoPhotoUrl.value : hlsUrl.value))
const panoReady = computed(() => !!panoSrc.value)

function trackBlob(url) { blobUrls.push(url); return url }

async function fetchBlobUrl(path) {
  const res = await http.get(path, { responseType: 'blob' })
  return trackBlob(URL.createObjectURL(res.data))
}

async function load() {
  cleanup()
  loading.value = true
  error.value = ''
  mode.value = ''
  hlsUrl.value = ''
  detail.value = null
  pano.value = null
  drawerOpen.value = false
  transcode.value = { jobId: '', status: '', starting: false, failed: false }

  try {
    const [d, p] = await Promise.all([
      http.get(`/media/${mediaId.value}`),
      http.get(`/media/${mediaId.value}/360`)
    ])
    detail.value = d.data
    pano.value = p.data

    if (p.data.is_360) {
      // 360 媒体：直接全景播放（照片贴球面 / 视频走 HLS），无「普通播放器 → 点按钮」两步
      mode.value = 'pano'
      if (d.data.type === 'photo') {
        panoPhotoUrl.value = await fetchBlobUrl(`/media/${mediaId.value}/download`)
      } else if (p.data.hls_master) {
        hlsUrl.value = API_BASE + p.data.hls_master
      }
    } else if (d.data.type === 'photo') {
      mode.value = 'photo'
      photoUrl.value = await fetchBlobUrl(`/media/${mediaId.value}/download`)
    } else {
      mode.value = 'video'
      loading.value = false // 先渲染出 video 元素再挂载 HLS
      await nextTick()
      setupPlainVideo(p.data.hls_master)
    }
  } catch (e) {
    error.value = e.response?.data?.error?.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function setupPlainVideo(master) {
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
    fetchBlobUrl(`/media/${mediaId.value}/download`).then((url) => { v.src = url })
      .catch(() => { error.value = '视频加载失败' })
  }
}

async function startTranscode() {
  transcode.value.starting = true
  transcode.value.failed = false
  try {
    const res = await http.post('/transcode/job', { media_id: mediaId.value, profile: '1080p' })
    transcode.value.jobId = res.data.job_id
    transcode.value.status = 'pending'
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
      pollJob() // 轮询出错继续重试
    }
  }, 2000)
}

function cleanup() {
  clearTimeout(pollTimer)
  if (plainHls) { plainHls.destroy(); plainHls = null }
  for (const u of blobUrls) URL.revokeObjectURL(u)
  blobUrls = []
  photoUrl.value = ''
  panoPhotoUrl.value = ''
}

/* 退出：返回来源页（优先路由历史，直达链接则回时间轴） */
function exit() {
  if (window.history.state?.back) {
    router.back()
  } else {
    router.replace({ name: 'timeline' })
  }
}

function onKeydown(e) {
  if (e.key !== 'Escape') return
  // 输入框/文本域内的 Esc 留给控件自身（如关闭标签补全）
  const tag = e.target?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA') return
  exit()
}

watch(mediaId, (id) => { if (id) load() }, { immediate: true })
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  cleanup()
})
</script>

<style scoped>
.player-view {
  position: relative;
  /* 抵消 AppShell .content 的 24px 内边距，播放器满幅展示 */
  margin: -24px;
  width: calc(100% + 48px);
  height: calc(100vh - var(--topbar-height));
  background: #14181d;
  overflow: hidden;
  display: flex;
}

.stage-area {
  position: relative;
  flex: 1;
  min-width: 0;
  overflow: hidden;
}

.pano { position: absolute; inset: 0; }

.state {
  height: 100%;
  display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
  color: #9aa7b4; font-size: var(--font-size-md);
}
.state.error { color: var(--color-danger); }
.state .msg { font-size: var(--font-size-lg); color: #f2f5f8; }
.state .sub { font-size: var(--font-size-sm); }
.primary {
  background: var(--color-primary); color: #fff; border: none;
  border-radius: var(--radius-md); padding: 10px 24px;
  font-size: var(--font-size-lg); cursor: pointer;
}
.primary:hover { background: var(--color-primary-hover); }
.primary:disabled { opacity: 0.5; cursor: not-allowed; }
.photo-wrap {
  height: 100%; display: flex; align-items: center; justify-content: center;
}
.photo { max-width: 100%; max-height: 100%; object-fit: contain; }
.video-wrap {
  height: 100%; display: flex; align-items: center; justify-content: center;
}
.plain-video { max-width: 100%; max-height: 100%; }

/* 退出 / 信息按钮：悬浮于舞台之上，高于 360 播放器的控制栏（z-index 10） */
.exit-btn {
  position: absolute;
  z-index: 30;
  width: 36px;
  height: 36px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: rgba(20, 24, 29, 0.6);
  color: #e6ebf0;
  backdrop-filter: blur(6px);
}

.exit-btn:hover {
  background: rgba(20, 24, 29, 0.85);
  color: #fff;
}

.close-btn {
  top: 10px;
  right: 10px;
}

.back-btn {
  top: 10px;
  left: 10px;
}

.info-btn {
  top: 10px;
  right: 56px;
}

/* 移动端信息抽屉：底部上滑面板 */
.drawer-mask {
  position: absolute;
  inset: 0;
  z-index: 40;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-end;
}

.drawer {
  width: 100%;
  max-height: 72vh;
  background: var(--color-surface);
  border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  overflow: hidden;
  display: flex;
}

.drawer :deep(.info-panel) {
  width: 100%;
  border-left: none;
}
</style>
