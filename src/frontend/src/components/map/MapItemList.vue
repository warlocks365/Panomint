<template>
  <div class="item-list">
    <div class="il-head">
      <span class="il-title">此处 {{ items.length }} 项</span>
      <button class="il-close" type="button" @click="$emit('close')">关闭</button>
    </div>

    <div v-if="loading" class="il-hint">加载中…</div>
    <div v-else-if="!items.length" class="il-hint">没有匹配的媒体</div>

    <div v-else class="il-grid">
      <button
        v-for="it in items"
        :key="it.id"
        class="il-cell"
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
</template>

<script setup>
import { onBeforeUnmount, reactive, watch } from 'vue'
// 复用 mediaLoader：缓存 + pending 去重 + 404 回退（原 thumbBlobUrl 每次挂载重拉）
import { loadThumbUrl } from '../timeline/mediaLoader'

const props = defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false }
})
defineEmits(['close', 'open'])

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
  margin-bottom: 8px;
}

.il-title {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}

.il-close {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: #475569;
  cursor: pointer;
}

.il-hint {
  font-size: 12px;
  color: #94a3b8;
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
  background: #e2e8f0;
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
  color: #64748b;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
