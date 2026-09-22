<template>
  <div class="item-list">
    <div class="il-head">
      <span class="il-title">此处 {{ items.length }} 项</span>
      <button
        v-if="selectable && items.length"
        class="il-all"
        type="button"
        data-testid="map-list-select-all"
        @click="toggleAll"
      >{{ allSelected ? '取消全选' : '全选此处' }}</button>
      <button class="il-close" type="button" @click="$emit('close')">关闭</button>
    </div>

    <div v-if="loading" class="il-hint">加载中…</div>
    <div v-else-if="!items.length" class="il-hint">没有匹配的媒体</div>

    <div v-else class="il-grid">
      <div v-for="it in items" :key="it.id" class="il-cell-wrap">
        <label v-if="selectable" class="il-check" @click.stop>
          <input
            type="checkbox"
            :checked="batch.selected.has(it.id)"
            data-testid="map-item-check"
            @change="batch.toggle(it.id)"
          />
        </label>
        <button
          class="il-cell"
          :class="{ 'il-cell--selected': batch.selected.has(it.id) }"
          type="button"
          @click="$emit('open', it)"
        >
          <img v-if="thumbs[it.id]" :src="thumbs[it.id]" :alt="it.filename" loading="lazy" />
          <div v-else class="il-ph"></div>
          <span v-if="it.is_360" class="il-badge">360</span>
          <span class="il-name">{{ it.filename }}</span>
        </button>
      </div>
    </div>

    <BatchBar
      v-if="selectable && batch.count.value > 0"
      :count="batch.count.value"
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
import { computed, onBeforeUnmount, reactive, watch } from 'vue'
// 复用 mediaLoader：缓存 + pending 去重 + 404 回退（原 thumbBlobUrl 每次挂载重拉）
import { loadThumbUrl } from '../timeline/mediaLoader'
import BatchBar from '../media/BatchBar.vue'
import { useBatchOps } from '../media/useBatchOps'

const props = defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  selectable: { type: Boolean, default: true } // Job000080 地图批量：列表=cluster 内容物，「全选此处」=整簇
})
const emit = defineEmits(['close', 'open', 'changed'])

// 选中集按媒体 id 存于本组件（面板关闭即弃，天然清空）；操作完成 emit changed 让宿主重取列表+簇
const batch = useBatchOps(() => emit('changed'))

const allSelected = computed(
  () => props.items.length > 0 && props.items.every((it) => batch.selected.has(it.id))
)

function toggleAll() {
  if (allSelected.value) {
    batch.clear()
  } else {
    for (const it of props.items) batch.selected.add(it.id)
  }
}

// 缩略图需 Bearer 鉴权 → 经 mediaLoader 取 blob 转本地 URL（img src 无法带 header）。
// objectURL 生命周期由 mediaLoader 的 LRU 统一管理，组件不 revoke
const thumbs = reactive({})
let alive = true

watch(
  () => props.items,
  (list) => {
    for (const it of list) {
      if (thumbs[it.id]) continue
      loadThumbUrl({ id: it.id }, 'sm')
        .then((url) => {
          // 卸载守卫：慢响应到达时组件可能已卸载，不得再写
          if (alive) thumbs[it.id] = url
        })
        .catch(() => {})
    }
  },
  { immediate: true }
)

onBeforeUnmount(() => {
  alive = false
})
</script>

<style scoped>
.item-list {
  position: absolute;
  left: 12px;
  bottom: 12px;
  width: min(360px, calc(100% - 24px));
  max-height: 46%;
  overflow: auto;
  background: rgba(255, 255, 255, 0.97);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.12);
  padding: 10px 12px 12px;
  z-index: 5;
}

.il-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.il-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-primary);
}

.il-all {
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: var(--color-text-primary);
  cursor: pointer;
}

.il-cell-wrap {
  position: relative;
}

.il-check {
  position: absolute;
  top: 3px;
  left: 3px;
  z-index: 2;
  display: inline-flex;
  padding: 2px;
  background-color: rgba(255, 255, 255, 0.9);
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.il-check input {
  width: 14px;
  height: 14px;
  accent-color: var(--color-primary);
  cursor: pointer;
}

.il-cell--selected img,
.il-cell--selected .il-ph {
  outline: 2px solid var(--color-primary);
  outline-offset: -2px;
}

.il-close {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.il-hint {
  font-size: 12px;
  color: var(--color-text-disabled);
  padding: 8px 0;
}

.il-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 6px;
}

.il-cell {
  position: relative;
  border: none;
  background: none;
  padding: 0;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.il-cell img,
.il-ph {
  width: 100%;
  aspect-ratio: 1;
  object-fit: cover;
  border-radius: 6px;
  background: var(--color-surface-hover);
}

.il-badge {
  position: absolute;
  top: 3px;
  left: 3px;
  background: rgba(37, 99, 235, 0.92);
  color: #fff;
  font-size: 10px;
  border-radius: 4px;
  padding: 0 4px;
}

.il-name {
  font-size: 10px;
  color: var(--color-text-secondary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
