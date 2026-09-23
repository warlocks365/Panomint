<template>
  <button class="back-btn" data-testid="back-btn" title="返回" @click="emit('click')">
    <svg viewBox="0 0 24 24" width="16" height="16" fill="none">
      <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  </button>
</template>

<script setup>
// Job000102 层级返回导航：统一返回钮纯展示件（左箭头）。
// 导航职责不在本组件——只 emit click，宿主握 goBack（useBackNavigation）决定
// router.back() 还是 replace 到逻辑父级（router 属视图职责，Job000090 范式）。
defineOptions({ name: 'BackButton' })
const emit = defineEmits(['click'])
</script>

<style scoped>
.back-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  padding: 0;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-secondary);
  cursor: pointer;
}

.back-btn:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}
</style>
