<template>
  <div
    class="tree-row"
    :class="{ 'tree-row--selected': selected, 'tree-row--locked': !node.readable }"
    :style="{ paddingLeft: 8 + depth * 18 + 'px' }"
    :data-testid="'tree-row-' + (node.rel || 'root')"
    :data-rel="node.rel"
    @click="$emit('select', node)"
  >
    <button
      class="tree-chevron"
      type="button"
      :disabled="!node.readable"
      :aria-label="node.expanded ? '折叠' : '展开'"
      :data-testid="'tree-expand-' + (node.rel || 'root')"
      @click.stop="$emit('toggle', node)"
    >
      <svg v-if="node.loading" class="tree-spin" viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" stroke-width="2" opacity="0.25" />
        <path d="M14 8a6 6 0 0 0-6-6" fill="none" stroke="currentColor" stroke-width="2" />
      </svg>
      <svg v-else viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
        <path
          v-if="node.readable"
          d="M6 4l4 4-4 4"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
          :transform="node.expanded ? 'rotate(90 8 8)' : ''"
        />
        <path v-else d="M5 8h6" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
      </svg>
    </button>

    <svg class="tree-folder" viewBox="0 0 16 16" width="16" height="16" aria-hidden="true">
      <path
        d="M1.5 4.5a1 1 0 0 1 1-1h3l1.5 1.5h6.5a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1h-11a1 1 0 0 1-1-1z"
        fill="none"
        stroke="currentColor"
        stroke-width="1.4"
        stroke-linejoin="round"
      />
    </svg>

    <span class="tree-name">{{ node.rel === '' ? '（媒体根）' : node.name }}</span>

    <svg
      v-if="!node.readable"
      class="tree-lock"
      viewBox="0 0 16 16"
      width="14"
      height="14"
      aria-hidden="true"
      :data-testid="'tree-lock-' + node.rel"
    >
      <rect x="3.5" y="7" width="9" height="6" rx="1" fill="none" stroke="currentColor" stroke-width="1.4" />
      <path d="M5.5 7V5.5a2.5 2.5 0 0 1 5 0V7" fill="none" stroke="currentColor" stroke-width="1.4" />
    </svg>
  </div>
</template>

<script setup>
// 目录树单行节点（Job000117）：由 DirectoryTreeDialog 平铺渲染，行内只负责展示与
// 事件上抛（select/toggle），展开/懒加载状态变更仍归对话框持有（同一 node 引用）。
defineProps({
  node: { type: Object, required: true },
  depth: { type: Number, default: 0 },
  selected: { type: Boolean, default: false },
})
defineEmits(['select', 'toggle'])
</script>

<style scoped>
.tree-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-top: 4px;
  padding-bottom: 4px;
  padding-right: 8px;
  border-radius: var(--radius-sm);
  cursor: pointer;
  color: var(--color-text-primary);
}
.tree-row:hover {
  background-color: var(--color-surface-hover);
}
.tree-row--selected {
  background-color: var(--color-primary-active-bg);
}
.tree-row--locked {
  color: var(--color-text-secondary);
  cursor: not-allowed;
}
.tree-chevron {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
  padding: 0;
  border: none;
  background: transparent;
  color: inherit;
  cursor: pointer;
}
.tree-chevron:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}
.tree-spin {
  animation: tree-rot 0.9s linear infinite;
}
@keyframes tree-rot {
  to {
    transform: rotate(360deg);
  }
}
.tree-folder {
  flex: none;
  color: var(--color-text-secondary);
}
.tree-name {
  flex: 1;
  font-size: var(--font-size-md);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tree-lock {
  flex: none;
  color: var(--color-text-secondary);
}
</style>
