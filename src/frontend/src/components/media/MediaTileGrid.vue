<template>
  <div class="media-grid">
    <div
      v-for="m in items"
      :key="m.id"
      class="media-tile"
      :title="m.filename"
      data-testid="media-tile"
      @click="$emit('open', m)"
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
</template>

<script setup>
// 通用媒体磁贴网格（Job000065）：从 SpacesView/FoldersView 抽出的共用件——
// 缩略图异步加载（loadThumbUrl 是 Promise，.then 落响应式表）+ 失败回退类型图标 + 点击转发。
// 两视图的磁贴样式/语义完全一致（逐字对齐），之前各写一份是重复债。
import { reactive, watch } from 'vue'
import { loadThumbUrl } from '../timeline/mediaLoader'

const props = defineProps({
  items: { type: Array, default: () => [] },
  preload: { type: Number, default: 120 } // 预载前 N 张缩略图，超出保留类型图标
})
defineEmits(['open'])

const icons = {
  photo:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="10" r="2" stroke="currentColor" stroke-width="1.5"/><path d="M4 18l5-5 3.5 3.5L16 13l4 4" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>',
  video:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 9.5l5 2.5-5 2.5v-5z" fill="currentColor"/></svg>',
  pano:
    '<svg viewBox="0 0 24 24" width="26" height="26" fill="none"><circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.5"/><ellipse cx="12" cy="12" rx="3.5" ry="8.5" stroke="currentColor" stroke-width="1.5"/><path d="M3.5 12h17" stroke="currentColor" stroke-width="1.5"/></svg>'
}

const thumbs = reactive({})
const brokenThumbs = reactive(new Set())

function ensureThumbs(list) {
  for (const m of list.slice(0, props.preload)) {
    if (thumbs[m.id] || brokenThumbs.has(m.id)) continue
    loadThumbUrl({ id: m.id }, 'sm')
      .then((url) => { thumbs[m.id] = url })
      .catch(() => { brokenThumbs.add(m.id) })
  }
}

function thumbOf(id) {
  return thumbs[id] || ''
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

watch(() => props.items, (list) => ensureThumbs(list), { immediate: true })
</script>

<style scoped>
/* 样式逐字对齐 SpacesView/FoldersView 原磁贴样式（两视图已一致） */
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
