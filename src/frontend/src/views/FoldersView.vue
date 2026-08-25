<template>
  <div class="folders-view">
    <h1 class="page-title">文件夹</h1>

    <div v-if="loadError" class="error-banner">{{ loadError }}</div>

    <div class="folders-body">
      <aside class="tree-panel">
        <div v-if="treeLoading" class="muted tree-tip">目录加载中…</div>
        <template v-else>
          <button
            class="tree-row"
            :class="{ 'tree-row--active': selectedPath === '' }"
            :style="{ paddingLeft: '12px' }"
            @click="selectFolder('')"
          >
            <span class="tree-caret tree-caret--leaf"></span>
            <span class="tree-icon" v-html="icons.folder"></span>
            <span class="tree-name">全部</span>
            <span class="tree-count">{{ totalCount }}</span>
          </button>
          <button
            v-for="row in flatRows"
            :key="row.node.path"
            class="tree-row"
            :class="{ 'tree-row--active': selectedPath === row.node.path }"
            :style="{ paddingLeft: 12 + row.depth * 18 + 'px' }"
            @click="onRowClick(row)"
          >
            <span
              class="tree-caret"
              :class="{ 'tree-caret--leaf': row.node.children.length === 0 }"
              @click.stop="toggleExpand(row.node.path)"
              v-html="caretIcon(row.node)"
            ></span>
            <span class="tree-icon" v-html="icons.folder"></span>
            <span class="tree-name">{{ row.node.name }}</span>
            <span class="tree-count">{{ row.node.count }}</span>
          </button>
        </template>
      </aside>

      <section class="grid-panel">
        <div class="grid-header">
          <h2 class="section-title">{{ selectedPath || '全部媒体' }}</h2>
          <span v-if="!mediaLoading" class="muted">{{ filteredItems.length }} 项</span>
        </div>

        <div v-if="mediaLoading" class="muted grid-tip">加载中…</div>
        <div v-else-if="filteredItems.length === 0" class="muted grid-tip">该目录暂无媒体</div>

        <div v-else class="media-grid">
          <div v-for="m in filteredItems" :key="m.id" class="media-tile" :title="m.filename">
            <span class="tile-icon" v-html="typeIcon(m)"></span>
            <span class="tile-name">{{ m.filename }}</span>
            <span class="tile-meta">{{ formatDate(m.taken_at) }}</span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import http from '../api/http'

const icons = {
  folder:
    '<svg viewBox="0 0 16 16" width="15" height="15" fill="none"><path d="M2 4a1 1 0 0 1 1-1h3.6l1.6 2H13a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/></svg>',
  caretRight:
    '<svg viewBox="0 0 10 10" width="10" height="10" fill="none"><path d="M3.5 2l3.5 3-3.5 3" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  caretDown:
    '<svg viewBox="0 0 10 10" width="10" height="10" fill="none"><path d="M2 3.5l3 3.5 3-3.5" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  photo:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="10" r="2" stroke="currentColor" stroke-width="1.5"/><path d="M4 18l5-5 3.5 3.5L16 13l4 4" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
  video:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 9.5l5 2.5-5 2.5v-5z" fill="currentColor"/></svg>',
  pano:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.5"/><ellipse cx="12" cy="12" rx="3.5" ry="8.5" stroke="currentColor" stroke-width="1.5"/><path d="M3.5 12h17" stroke="currentColor" stroke-width="1.5"/></svg>'
}

const tree = ref(null)
const treeLoading = ref(true)
const expanded = ref(new Set())
const selectedPath = ref('')
const loadError = ref('')

const mediaItems = ref([])
const mediaLoading = ref(true)

const totalCount = computed(() => tree.value?.count ?? mediaItems.value.length)

// 拍平目录树为可见行（含层级），按展开状态裁剪子树
const flatRows = computed(() => {
  const rows = []
  const walk = (nodes, depth) => {
    for (const n of nodes || []) {
      rows.push({ node: n, depth })
      if (n.children.length > 0 && expanded.value.has(n.path)) {
        walk(n.children, depth + 1)
      }
    }
  }
  walk(tree.value?.children || [], 0)
  return rows
})

// 当前目录媒体：folder_path 等于选中路径，或位于其子目录下
const filteredItems = computed(() => {
  if (!selectedPath.value) return mediaItems.value
  const prefix = selectedPath.value + '/'
  return mediaItems.value.filter((m) => {
    const fp = m.folder_path || ''
    return fp === selectedPath.value || fp.startsWith(prefix)
  })
})

function caretIcon(node) {
  if (node.children.length === 0) return ''
  return expanded.value.has(node.path) ? icons.caretDown : icons.caretRight
}

function toggleExpand(path) {
  const s = new Set(expanded.value)
  if (s.has(path)) {
    s.delete(path)
  } else {
    s.add(path)
  }
  expanded.value = s
}

function onRowClick(row) {
  selectFolder(row.node.path)
  if (row.node.children.length > 0 && !expanded.value.has(row.node.path)) {
    toggleExpand(row.node.path)
  }
}

function selectFolder(path) {
  selectedPath.value = path
}

function typeIcon(m) {
  if (m.is_360) return icons.pano
  return m.type === 'video' ? icons.video : icons.photo
}

function formatDate(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

async function fetchAllMedia() {
  const items = []
  let cursor = ''
  // 拉取全量（游标分页，上限保护 20 页）
  for (let i = 0; i < 20; i++) {
    const params = { limit: 200 }
    if (cursor) params.cursor = cursor
    const res = await http.get('/media', { params })
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
    // 默认展开第一层
    const s = new Set()
    for (const n of res.data.children || []) s.add(n.path)
    expanded.value = s
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

.tree-panel {
  width: 260px;
  flex-shrink: 0;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 8px;
  overflow-y: auto;
  max-height: calc(100vh - var(--topbar-height) - 140px);
}

.tree-tip {
  padding: 16px 12px;
  font-size: var(--font-size-sm);
}

.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 7px 12px;
  border: none;
  background: transparent;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  cursor: pointer;
  font-family: var(--font-family);
  text-align: left;
}

.tree-row:hover {
  background-color: var(--color-surface-hover);
}

.tree-row--active {
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
}

.tree-caret {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 14px;
  color: var(--color-text-secondary);
  flex-shrink: 0;
}

.tree-caret--leaf {
  visibility: hidden;
}

.tree-row--active .tree-caret {
  color: var(--color-primary);
}

.tree-icon {
  display: inline-flex;
  color: var(--color-text-secondary);
  flex-shrink: 0;
}

.tree-row--active .tree-icon {
  color: var(--color-primary);
}

.tree-name {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.tree-count {
  font-size: 12px;
  color: var(--color-text-disabled);
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
