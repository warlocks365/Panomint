<template>
  <div>
    <div class="media-grid" :class="{ 'media-grid--selecting': selectable && batch.count.value > 0 }">
      <div
        v-for="m in items"
        :key="m.id"
        class="media-tile"
        :class="{ 'media-tile--selected': batch.selected.has(m.id) }"
        :title="m.filename"
        data-testid="media-tile"
        @click="onTileClick(m)"
      >
        <label v-if="selectable" class="tile-check" @click.stop>
          <input
            type="checkbox"
            :checked="batch.selected.has(m.id)"
            data-testid="tile-check"
            @change="batch.toggle(m.id)"
          />
        </label>
        <button
          v-if="removable"
          class="tile-remove"
          title="移除"
          data-testid="tile-remove"
          @click.stop="emit('remove', m)"
        >
          <svg viewBox="0 0 24 24" width="12" height="12" fill="none">
            <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          </svg>
        </button>
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

    <BatchBar
      v-if="selectable && batch.count.value > 0"
      :count="batch.count.value"
      show-select-all
      :all-selected="batch.isAllSelected(allIds())"
      show-invert
      @toggle-all="batch.toggleAll(allIds())"
      @invert="batch.invertAll(allIds())"
      @meta="batch.onMeta"
      @tags="batch.onTags"
      @move="batch.onMove"
      @copy="batch.onCopy"
      @share="batch.onShare"
      @space="batch.onShareSpace"
      @delete="batch.onDelete"
      @clear="batch.clear"
    />
    <p v-if="selectable && batch.lastResult.value" class="batch-result" data-testid="batch-result">
      {{ batch.lastResult.value }}
    </p>
  </div>
</template>

<script setup>
// 通用媒体磁贴网格（Job000065 抽出；Job000066 增批量选择）：selectable 开启勾选模式，
// 勾选后吸底操作栏——七模块行为统一（useBatchOps → /media/batch）。
import { reactive, watch } from 'vue'
import { loadThumbUrl } from '../timeline/mediaLoader'
import BatchBar from './BatchBar.vue'
import { useBatchOps } from './useBatchOps'

const props = defineProps({
  items: { type: Array, default: () => [] },
  preload: { type: Number, default: 120 }, // 预载前 N 张缩略图，超出保留类型图标
  selectable: { type: Boolean, default: false }, // 批量操作模式（勾选 + 操作栏）
  removable: { type: Boolean, default: false } // 逐项移除（如从相册移除；悬停显示 ×）
})
const emit = defineEmits(['open', 'changed', 'remove'])

const batch = useBatchOps(() => emit('changed'))

// Job000099 全选/反选全集 = 当前 items（宿主传入的网格集合）
function allIds() {
  return props.items.map((m) => m.id)
}

function onTileClick(m) {
  // 勾选模式下点击=打开预览；勾选由复选框负责（stop 传播）
  emit('open', m)
}

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

.media-tile--selected {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}

.tile-check {
  position: absolute;
  top: 6px;
  left: 6px;
  z-index: 2;
  display: inline-flex;
  padding: 2px;
  background-color: rgba(255, 255, 255, 0.9);
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.tile-check input {
  width: 15px;
  height: 15px;
  accent-color: var(--color-primary);
  cursor: pointer;
}

/* 逐项移除（相册场景）：悬停显示，样式对齐 MediaThumb.mthumb-remove */
.tile-remove {
  position: absolute;
  top: 6px;
  right: 6px;
  z-index: 2;
  width: 22px;
  height: 22px;
  border: none;
  border-radius: 50%;
  display: none;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
  cursor: pointer;
}

.media-tile:hover .tile-remove {
  display: flex;
}

.tile-remove:hover {
  background-color: var(--color-danger);
}

.batch-result {
  margin: 8px 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.media-tile {
  position: relative;
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
