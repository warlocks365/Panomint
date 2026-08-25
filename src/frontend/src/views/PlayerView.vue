<template>
  <div class="player-view">
    <div v-if="loading" class="state">加载中…</div>
    <div v-else-if="error" class="state error">{{ error }}</div>

    <!-- 照片：直接显示原图 -->
    <div v-else-if="mode === 'photo'" class="photo-wrap">
      <img v-if="photoUrl" :src="photoUrl" :alt="detail?.filename || '照片'" class="photo">
    </div>

    <!-- 360：已转码 → 全景播放器 -->
    <Player360
      v-else-if="mode === 'pano' && hlsUrl"
      :key="hlsUrl"
      :media-id="mediaId"
      :src="hlsUrl"
      :title="detail?.filename || ''"
      class="pano"
    />

    <!-- 360：未转码 → 提示并发起转码 -->
    <div v-else-if="mode === 'pano'" class="state transcode">
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
  </div>
</template>

<script setup>
import { ref, computed, watch, onBeforeUnmount, nextTick } from 'vue'
import { useRoute } from 'vue-router'
import Hls from 'hls.js'
import http from '../api/http'
import { getAccessToken } from '../utils/tokenStore'
import Player360 from '../components/player/360Player.vue'

const route = useRoute()
const mediaId = computed(() => String(route.params.id || ''))

const loading = ref(true)
const error = ref('')
const detail = ref(null)
const pano = ref(null)
const mode = ref('') // photo | pano | video
const hlsUrl = ref('')
const photoUrl = ref('')
const plainVideoRef = ref(null)
const transcode = ref({ jobId: '', status: '', starting: false, failed: false })

let pollTimer = 0
let plainHls = null
let blobUrls = []

const API_BASE = http.defaults.baseURL

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
  transcode.value = { jobId: '', status: '', starting: false, failed: false }

  try {
    const [d, p] = await Promise.all([
      http.get(`/media/${mediaId.value}`),
      http.get(`/media/${mediaId.value}/360`)
    ])
    detail.value = d.data
    pano.value = p.data

    if (d.data.type === 'photo') {
      mode.value = 'photo'
      photoUrl.value = await fetchBlobUrl(`/media/${mediaId.value}/download`)
    } else if (p.data.is_360) {
      mode.value = 'pano'
      if (p.data.hls_master) hlsUrl.value = API_BASE + p.data.hls_master
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
}

watch(mediaId, (id) => { if (id) load() }, { immediate: true })
onBeforeUnmount(cleanup)
</script>

<style scoped>
.player-view {
  position: relative;
  width: 100%;
  height: calc(100vh - var(--topbar-height));
  background: #14181d;
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
</style>
