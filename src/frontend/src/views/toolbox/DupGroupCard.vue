<template>
  <article class="dup-group" data-testid="toolbox-group">
    <header class="dup-head">
      <span class="dup-index">第 {{ index + 1 }} 组</span>
      <span class="dup-count">{{ group.items.length }} 项</span>
      <span class="dup-phash" :title="`感知哈希 ${group.phash}`">{{ group.phash }}</span>
      <button
        class="btn danger sm"
        data-testid="toolbox-delete-rest"
        :disabled="deleting"
        @click="$emit('ask-delete', group)"
      >
        删除其余 {{ group.items.length - 1 }} 项
      </button>
    </header>

    <p class="dup-hint">
      建议保留「{{ keepOf(group).filename }}」，其余 {{ group.items.length - 1 }} 项可删除。
      想换一张保留？点缩略图即可改选。
    </p>

    <div class="dup-grid">
      <div
        v-for="it in group.items"
        :key="it.id"
        class="dup-item"
        :class="{ keep: it.id === group.keepId }"
        data-testid="toolbox-item"
        @click="$emit('set-keep', group, it)"
      >
        <span v-if="it.id === group.keepId" class="dup-keep-badge" data-testid="toolbox-keep-badge">
          建议保留
        </span>
        <MediaThumb :item="it" size="md" />
        <p class="dup-name" :title="it.filename">{{ it.filename }}</p>
        <p class="dup-meta">{{ itemMeta(it) }}</p>
      </div>
    </div>
  </article>
</template>

<script setup>
// 单个重复分组卡片——从 DupPanel 抽出（Job000058-4 二级拆分，守卫 300 行线）。
// keepId 改选是本地状态（不回写后端），直接改 props.group.keepId（父级状态即页面状态）。
import MediaThumb from '../../components/albums/MediaThumb.vue'

defineProps({
  group: { type: Object, required: true },
  index: { type: Number, default: 0 },
  deleting: { type: Boolean, default: false },
  keepOf: { type: Function, required: true }
})
defineEmits(['ask-delete'])

function itemMeta(it) {
  const parts = []
  if (it.width && it.height) parts.push(`${it.width}×${it.height}`)
  parts.push(it.type === 'video' ? '视频' : '照片')
  if (it.taken_at) parts.push(String(it.taken_at).slice(0, 10))
  return parts.join(' · ')
}
</script>

<style scoped>
.dup-group {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  padding: 12px 14px 14px;
}

.dup-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.dup-index {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  font-weight: 600;
}

.dup-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dup-phash {
  font-size: 11px;
  color: var(--color-text-disabled);
  font-family: monospace;
}

.dup-head .btn {
  margin-left: auto;
}

.dup-hint {
  margin: 8px 0 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.dup-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 10px;
}

.dup-item {
  position: relative;
  padding: 6px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  cursor: pointer;
}

.dup-item:hover {
  border-color: var(--color-border);
}

.dup-item.keep {
  border-color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.dup-keep-badge {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 2;
  padding: 1px 6px;
  border-radius: var(--radius-sm);
  font-size: 11px;
  color: #fff;
  background-color: var(--color-primary);
  pointer-events: none;
}

.dup-name {
  margin: 6px 0 2px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.dup-meta {
  margin: 0;
  font-size: 11px;
  color: var(--color-text-secondary);
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.sm {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}

.btn.danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
