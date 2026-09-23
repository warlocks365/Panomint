<template>
  <!-- 批量操作栏（Job000066）：选中数 > 0 时吸底出现；七模块行为统一（后端 /media/batch 单点语义）。
       Job000099 增统一全选/反选：宿主传 allSelected 状态与 showInvert，行为与地图「全选此处」同源。 -->
  <div class="batch-bar" data-testid="batch-bar">
    <span class="bb-count">已选 {{ count }} 项</span>
    <button
      v-if="showSelectAll"
      class="bb-btn"
      data-testid="bb-select-all"
      @click="$emit('toggle-all')"
    >{{ allSelected ? '取消全选' : '全选' }}</button>
    <button
      v-if="showInvert"
      class="bb-btn"
      data-testid="bb-invert"
      @click="$emit('invert')"
    >反选</button>
    <button class="bb-btn" data-testid="bb-meta" @click="$emit('meta')">修改元数据</button>
    <button class="bb-btn" data-testid="bb-tags" @click="$emit('tags')">编辑标签</button>
    <button class="bb-btn" data-testid="bb-move" @click="$emit('move')">移动</button>
    <button class="bb-btn" data-testid="bb-copy" @click="$emit('copy')">复制</button>
    <button class="bb-btn" data-testid="bb-share" @click="$emit('share')">分享</button>
    <button class="bb-btn" data-testid="bb-space" @click="$emit('space')">共享</button>
    <button class="bb-btn bb-btn--danger" data-testid="bb-delete" @click="$emit('delete')">删除</button>
    <button class="bb-clear" data-testid="bb-clear" @click="$emit('clear')">取消选择</button>
  </div>
</template>

<script setup>
defineProps({
  count: { type: Number, default: 0 },
  // Job000099：全选/反选（宿主传全集状态；不传 showSelectAll 则不渲染，向后兼容）
  showSelectAll: { type: Boolean, default: false },
  allSelected: { type: Boolean, default: false },
  showInvert: { type: Boolean, default: false }
})
defineEmits(['toggle-all', 'invert', 'meta', 'tags', 'move', 'copy', 'share', 'space', 'delete', 'clear'])
</script>

<style scoped>
.batch-bar {
  position: sticky;
  bottom: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 10px 14px;
  margin-top: 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  box-shadow: var(--shadow-card);
  z-index: 20;
}

.bb-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-right: 4px;
}

.bb-btn {
  padding: 6px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.bb-btn:hover {
  background-color: var(--color-surface-hover);
}

.bb-btn--danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.bb-clear {
  margin-left: auto;
  border: none;
  background: none;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}
</style>
