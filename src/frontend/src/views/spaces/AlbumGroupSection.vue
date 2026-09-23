<template>
  <section class="album-group" data-testid="album-group-section">
    <header class="group-header">
      <div class="group-title">
        <h3 class="group-name">{{ name }}</h3>
        <span class="group-count muted">{{ count }} 项</span>
      </div>
      <!-- 相册组：truncated 时给「查看全部」入口（跳相册详情页）；未分组桶 hasMore 仅提示 -->
      <button
        v-if="truncated && albumId"
        type="button"
        class="link-btn"
        data-testid="view-all-album"
        @click="emit('view-album', albumId)"
      >查看全部</button>
      <span v-else-if="truncated && !albumId" class="muted more-hint">仅显示前 {{ items.length }} 项</span>
    </header>

    <MediaTileGrid :items="items" :preload="24" @open="emit('open', $event)" />
  </section>
</template>

<script setup>
// 单个相册分组节（Job000101）：组头（名称/计数/查看全部）+ 前 N 项磁贴网格。
// 未分组桶复用同一组件（albumId 为空 → 无跳转入口，仅截断提示）。
// v1 分组内不开批量勾选：批量操作仍在时间轴视图；避免跨组选择的状态归属歧义。
import MediaTileGrid from '../../components/media/MediaTileGrid.vue'

defineProps({
  albumId: { type: String, default: '' }, // 空串 = 未分组桶
  name: { type: String, required: true },
  kind: { type: String, default: 'manual' }, // manual|favorites|smart|ungrouped（v1 仅展示语义）
  count: { type: Number, default: 0 },
  items: { type: Array, default: () => [] }, // MediaRef[]
  truncated: { type: Boolean, default: false } // true=还有未展示项
})

const emit = defineEmits(['open', 'view-album'])
</script>

<style scoped>
.album-group {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.group-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.group-title {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}

.group-name {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-count {
  font-size: var(--font-size-sm);
  flex-shrink: 0;
}

.link-btn {
  border: none;
  background: none;
  padding: 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  font-family: var(--font-family);
  flex-shrink: 0;
}

.link-btn:hover {
  color: var(--color-text-primary);
  text-decoration: underline;
}

.more-hint {
  font-size: var(--font-size-sm);
  flex-shrink: 0;
}

.muted {
  color: var(--color-text-secondary);
}
</style>
