<template>
  <div class="dlg-mask" @click.self="$emit('cancel')">
    <div class="confirm-dlg" role="alertdialog">
      <h3 class="confirm-title">删除重复项</h3>
      <p class="confirm-text">
        将删除本组 {{ group.items.length - 1 }} 项，保留「{{ keepOf(group).filename }}」。
        删除为软删，会进入回收站，之后仍可恢复。
      </p>
      <p v-if="error" class="confirm-error">{{ error }}</p>
      <div class="dlg-actions">
        <button class="btn" data-testid="toolbox-confirm-cancel" :disabled="busy" @click="$emit('cancel')">
          取消
        </button>
        <button class="btn danger" data-testid="toolbox-confirm-ok" :disabled="busy" @click="$emit('confirm')">
          {{ busy ? '删除中…' : '确认删除' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
// 删除确认对话框——从 DupPanel 抽出（Job000058-4 二级拆分，守卫 300 行线）。
defineProps({
  group: { type: Object, required: null },
  keepOf: { type: Function, required: true },
  busy: { type: Boolean, default: false },
  error: { type: String, default: '' }
})
defineEmits(['cancel', 'confirm'])
</script>

<style scoped>
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

.btn.danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.confirm-dlg {
  width: 380px;
  max-width: calc(100vw - 32px);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.confirm-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 10px;
}

.confirm-text {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  margin-bottom: 16px;
  line-height: 1.7;
}

.confirm-error {
  margin-bottom: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}
</style>
