<template>
  <div class="spaces-view">
    <h1 class="page-title">空间</h1>

    <div v-if="loadError" class="error-banner">{{ loadError }}</div>

    <div class="space-cards">
      <button
        class="space-card"
        :class="{ 'space-card--active': activeSpace === 'personal' }"
        @click="switchSpace('personal')"
      >
        <span class="space-icon" v-html="icons.personal"></span>
        <span class="space-info">
          <span class="space-name">个人空间</span>
          <span v-if="personal" class="space-meta">
            {{ personal.media_count }} 个媒体 · {{ formatBytes(personal.used_bytes) }}
          </span>
          <span v-else class="space-meta muted">加载中…</span>
        </span>
      </button>

      <button
        class="space-card"
        :class="{ 'space-card--active': activeSpace === 'shared' }"
        @click="switchSpace('shared')"
      >
        <span class="space-icon" v-html="icons.shared"></span>
        <span class="space-info">
          <span class="space-name">共享空间</span>
          <span class="space-meta muted">
            {{ shared.length > 0 ? shared.length + ' 个空间' : '暂无共享空间' }}
          </span>
        </span>
      </button>
    </div>

    <div v-if="activeSpace === 'shared' && shared.length === 0" class="empty-state">
      <span class="empty-icon" v-html="icons.shared"></span>
      <p class="empty-title">暂无共享空间</p>
      <p class="empty-desc">共享空间用于与家人朋友共同管理照片和视频。被邀请加入共享空间后，会显示在这里。</p>
    </div>

    <template v-else>
      <div class="grid-header">
        <h2 class="section-title">
          {{ activeSpace === 'personal' ? '个人空间媒体' : sharedSpaceName }}
        </h2>
        <span v-if="!mediaLoading" class="muted">共 {{ mediaTotal }} 项</span>
      </div>

      <div v-if="mediaLoading" class="muted grid-tip">加载中…</div>
      <div v-else-if="mediaItems.length === 0" class="muted grid-tip">该空间暂无媒体</div>

      <MediaTileGrid :items="mediaItems" @open="openItem" />

      <div v-if="nextCursor" class="load-more">
        <button class="btn" :disabled="loadingMore" @click="loadMore">
          {{ loadingMore ? '加载中…' : '加载更多' }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api/http'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'

const router = useRouter()

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id }
 })
}

const icons = {
  personal:
    '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><circle cx="12" cy="8" r="4" stroke="currentColor" stroke-width="1.6"/><path d="M4.5 20c1.2-3.9 4.1-6 7.5-6s6.3 2.1 7.5 6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
  shared:
    '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><circle cx="8.5" cy="9" r="3.2" stroke="currentColor" stroke-width="1.6"/><path d="M2.8 19c1-3.2 3.2-4.8 5.7-4.8s4.7 1.6 5.7 4.8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/><circle cx="16.5" cy="10" r="2.6" stroke="currentColor" stroke-width="1.6"/><path d="M15.4 14.4c2.4.2 4.3 1.7 5.2 4.6" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>'
}

const personal = ref(null)
const shared = ref([])
const activeSpace = ref('personal')
const loadError = ref('')

const mediaItems = ref([])
const mediaTotal = ref(0)
const nextCursor = ref('')
const mediaLoading = ref(false)
const loadingMore = ref(false)
const sharedSpaceName = ref('共享空间媒体')

function formatBytes(n) {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let v = n
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i]
}

async function fetchMedia(cursor) {
  const params = { space: activeSpace.value, limit: 60 }
  if (cursor) params.cursor = cursor
  const res = await http.get('/media', { params })
  return res.data
}

async function loadMedia() {
  mediaLoading.value = true
  mediaItems.value = []
  nextCursor.value = ''
  try {
    const data = await fetchMedia('')
    mediaItems.value = data.items || []
    mediaTotal.value = data.total ?? mediaItems.value.length
    nextCursor.value = data.next_cursor || ''
  } catch (e) {
    loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
  } finally {
    mediaLoading.value = false
  }
}

async function loadMore() {
  if (!nextCursor.value) return
  loadingMore.value = true
  try {
    const data = await fetchMedia(nextCursor.value)
    mediaItems.value = mediaItems.value.concat(data.items || [])
    nextCursor.value = data.next_cursor || ''
  } finally {
    loadingMore.value = false
  }
}

function switchSpace(space) {
  if (activeSpace.value === space) return
  activeSpace.value = space
  if (space === 'personal' || shared.value.length > 0) {
    loadMedia()
  }
}

onMounted(async () => {
  try {
    const res = await http.get('/spaces')
    personal.value = res.data.personal
    shared.value = res.data.shared || []
  } catch (e) {
    loadError.value = '空间信息加载失败：' + (e.response?.data?.error?.message || '网络错误')
  }
  await loadMedia()
})
</script>

<style scoped>
.spaces-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 1080px;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.error-banner {
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
}

.space-cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 16px;
}

.space-card {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 18px 20px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  cursor: pointer;
  text-align: left;
  font-family: var(--font-family);
}

.space-card:hover {
  background-color: var(--color-surface-hover);
}

.space-card--active {
  border-color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.space-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-md);
  background-color: var(--color-bg);
  color: var(--color-primary);
  flex-shrink: 0;
}

.space-card--active .space-icon {
  background-color: var(--color-surface);
}

.space-info {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}

.space-name {
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.space-meta {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.muted {
  color: var(--color-text-secondary);
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 56px 24px;
  background-color: var(--color-surface);
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-lg);
  text-align: center;
}

.empty-icon {
  color: var(--color-text-disabled);
  display: inline-flex;
}

.empty-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.empty-desc {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  max-width: 420px;
}

.grid-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.section-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
}

.grid-tip {
  padding: 32px 0;
  text-align: center;
}



.load-more {
  display: flex;
  justify-content: center;
}

.btn {
  padding: 8px 20px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  font-family: var(--font-family);
}

.btn:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.btn:disabled {
  color: var(--color-text-disabled);
  cursor: default;
}
</style>
