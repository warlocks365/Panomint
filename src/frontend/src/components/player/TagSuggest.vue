<template>
  <div v-if="open" class="suggest">
    <div v-if="loading" class="suggest-tip">搜索中…</div>
    <template v-else>
      <button
        v-for="s in suggestions"
        :key="s.id"
        class="suggest-item"
        @mousedown.prevent="$emit('pick', s)"
      >
        <span class="suggest-name" :class="{ ai: s.kind === 'ai' }">{{ s.name }}</span>
        <span v-if="s.usage_count != null || s.count != null" class="suggest-count">{{ s.usage_count ?? s.count }}</span>
      </button>
      <button
        v-if="inputText.trim() && !suggestions.some((s) => s.name === inputText.trim())"
        class="suggest-item create"
        @mousedown.prevent="$emit('create', inputText.trim())"
      >
        创建标签「{{ inputText.trim() }}」
      </button>
      <div v-if="!suggestions.length && !inputText.trim()" class="suggest-tip">输入以搜索已有标签</div>
    </template>
  </div>
</template>

<script setup>
// MediaInfoPanel 拆解（Job000082）：标签自动补全下拉。纯展示+点击上报，防抖搜索逻辑在父组件。
defineProps({
  open: { type: Boolean, default: false },
  loading: { type: Boolean, default: false },
  suggestions: { type: Array, default: () => [] },
  inputText: { type: String, default: '' }
})

defineEmits(['pick', 'create'])
</script>

<style scoped>
.suggest {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  z-index: 20;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-card);
  max-height: 200px;
  overflow-y: auto;
  padding: 4px;
}

.suggest-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.suggest-item:hover {
  background-color: var(--color-surface-hover);
}

.suggest-item.create {
  color: var(--color-primary);
}

.suggest-name.ai {
  color: var(--color-warning-text);
}

.suggest-count {
  margin-left: auto;
  font-size: 11px;
  color: var(--color-text-disabled);
}

.suggest-tip {
  padding: 8px 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}
</style>
