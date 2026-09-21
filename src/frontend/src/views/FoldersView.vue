<template>
  <div class="folders-view">
    <h1 class="page-title">文件夹</h1>

    <div v-if="loadError" class="error-banner">{{ loadError }}</div>

    <div class="folders-body">
      <FolderTree
        :tree="tree"
        :loading="treeLoading"
        :selected-path="selectedPath"
        :total-count="totalCount"
        @select="selectFolder"
      />

      <section class="grid-panel">
        <div class="grid-header">
          <h2 class="section-title">{{ selectedPath || '全部媒体' }}</h2>
          <span v-if="!mediaLoading" class="muted">{{ filteredItems.length }} 项</span>
        </div>

        <div v-if="mediaLoading" class="muted grid-tip">加载中…</div>
        <div v-else-if="filteredItems.length === 0" class="muted grid-tip">该目录暂无媒体</div>

        <div v-else class="media-grid">
          <div
            v-for="m in filteredItems"
            :key="m.id"
            class="media-tile"
            :title="m.filename"
            data-testid="folder-tile"
            @click="openItem(m)"
          >
            <img
              v-if="thumbOf(m.id)"
              class="tile-thumb"
              :src="thumbOf(m.id)"
              :alt="m.filename"
              loading="lazy"
            />
            <span v-else class="tile-icon" v-html="typeIcon(m)"></span>
            <span class="tile-name">{{ m.filename }}</span>
            <span class="tile-meta">{{ formatDate(m.taken_at) }}</span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api/http'
import { loadThumbUrl } from '../components/timeline/mediaLoader'
import FolderTree from './folders/FolderTree.vue'

const router = useRouter()

// 预览缩略图：异步 objectURL（loadThumbUrl 返回 Promise，见地点页同款教训）；
// 仅预载当前目录前 100 项，超出保留类型图标（防大目录一次性数百请求）
const THUMB_PRELOAD = 100
const thumbs = reactive({})
const brokenThumbs = reactive(new Set())

function ensureThumbs(list) {
  for (const m of list.slice(0, THUMB_PRELOAD)) {
    if (thumbs[m.id] || brokenThumbs.has(m.id)) continue
    loadThumbUrl({ id: m.id }, 'sm')
      .then((url) => { thumbs[m.id] = url })
      .catch(() => { brokenThumbs.add(m.id) })
  }
}

function thumbOf(id) {
  return thumbs[id] || ''
}

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id } })
}

const icons = {
  photo:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="10" r="2" stroke="currentColor" stroke-width="1.5"/><path d="M4 18l5-5 3.5 3.5L16 13l4 4" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
  video:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 9.5l5 2.5-5 2.5v-5z" fill="currentColor"/></svg>',
  pano:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.5"/><ellipse cx="12" cy="12" rx="3.5" ry="8.5" stroke="currentColor" stroke-width="1.5"/><path d="M3.5 12h17" stroke="currentColor" stroke-width="1.5"/></svg>'
}

const tree = ref(null)
const treeLoading = ref(true)
const selectedPath = ref('')
const loadError = ref('')

const mediaItems = ref([])
const mediaLoading = ref(true)

const totalCount = computed(() => tree.value?.count ?? mediaItems.value.length)

// 当前目录媒体：folder_path 等于选中路径，或位于其子目录下
const filteredItems = computed(() => {
  if (!selectedPath.value) return mediaItems.value
  const prefix = selectedPath.value + '/'
  return mediaItems.value.filter((m) => {
    const fp = m.folder_path || ''
    return fp === selectedPath.value || fp.startsWith(prefix)
  })
})

// 目录切换/媒体到位 → 预载可见项缩略图
watch(filteredItems, (list) => ensureThumbs(list), { immediate: true })

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function selectFolder(path) {
  selectedPath.value = path
}

function typeIcon(m) {
  if (m.is_360) return icons.pano
  return m.type === 'video' ? icons.video : icons.photo
}

async function fetchAllMedia() {
  const items = []
  let cursor = ''
  // 拉取全量（游标分页，上限保护 20 页）
  for (let i = 0; i < 20; i++) {
    const params = { limit: 200 }
    if (cursor) params.cursor = cursor
    const res = await http.get('/media', { params, timeout: 0 }) // 全量连拉上限 20 页，慢网/大库下 15s 默认超时不够
    items.push(...(res.data.items || []))
    cursor = res.data.next_cursor || ''
    if (!cursor) break
  }
  return items
}

onMounted(async () => {
  try {
    const res = await http.get('/folders/tree')
    tree.value = res.data
  } catch (e) {
    loadError.value = '目录树加载失败：' + (e.response?.data?.error?.message || '网络错误')
  } finally {
    treeLoading.value = false
  }

  try {
    mediaItems.value = await fetchAllMedia()
  } catch (e) {
    loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
  } finally {
    mediaLoading.value = false
  }
})
</script>

<style scoped>
.folders-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  height: 100%;
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

.folders-body {
  display: flex;
  gap: 20px;
  min-height: 0;
  flex: 1;
}

.grid-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
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
  word-break: break-all;
}

.muted {
  color: var(--color-text-secondary);
}

.grid-tip {
  padding: 48px 0;
  text-align: center;
}

.media-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 12px;
}

.media-tile {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 6px;
  padding: 16px 10px 12px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  overflow: hidden;
  cursor: pointer;
}

.tile-thumb {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: var(--radius-sm);
  display: block;
  background-color: var(--color-surface-hover);
}

.media-tile:hover {
  background-color: var(--color-surface-hover);
}

.tile-icon {
  color: var(--color-text-disabled);
  display: inline-flex;
}

.tile-name {
  width: 100%;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  text-align: center;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tile-meta {
  font-size: 12px;
  color: var(--color-text-secondary);
}
</style>
