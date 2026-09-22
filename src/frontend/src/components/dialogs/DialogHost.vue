<template>
  <!-- 全局对话框宿主（Job000077）：消费 dialogs.js 单例状态栈，替代原生 prompt/confirm/alert。 -->
  <div v-if="cur" class="dlg-mask" data-testid="dialog-host" @click.self="onMask">
    <div class="dlg" role="dialog" :aria-label="cur.title || '对话框'">
      <h3 v-if="cur.title" class="dlg-title" data-testid="dlg-title">{{ cur.title }}</h3>
      <p v-if="cur.text" class="dlg-text">{{ cur.text }}</p>

      <label v-if="cur.kind === 'prompt'" class="field">
        <span v-if="cur.label || cur.text" class="field-label">{{ cur.label || '输入' }}</span>
        <input
          v-model="cur.value"
          class="input"
          type="text"
          data-testid="dlg-input"
          :placeholder="cur.placeholder || ''"
          @keyup.enter="dialogSubmit"
        />
      </label>

      <template v-else-if="cur.kind === 'form'">
        <label v-for="f in cur.fields" :key="f.key" class="field">
          <span class="field-label">{{ f.label }}</span>
          <input
            v-model="cur.values[f.key]"
            class="input"
            type="text"
            :data-testid="'dlg-field-' + f.key"
            :placeholder="f.placeholder || ''"
            @keyup.enter="dialogSubmit"
          />
        </label>
      </template>

      <p v-if="cur.error" class="dlg-error" data-testid="dlg-error">{{ cur.error }}</p>

      <div class="dlg-actions">
        <button v-if="cur.kind !== 'alert'" class="btn" data-testid="dlg-cancel" @click="dialogCancel">
          {{ cur.cancelText }}
        </button>
        <button
          class="btn primary"
          :class="{ danger: cur.danger }"
          data-testid="dlg-ok"
          @click="dialogSubmit"
        >
          {{ cur.confirmText }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { dialogState, dialogSubmit, dialogCancel } from './dialogs'

const cur = computed(() => dialogState.current)

// 遮罩点击=取消（alert 视为确定关闭）
function onMask() {
  dialogCancel()
}
</script>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.dlg {
  width: 400px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  overflow: auto;
  padding: 20px;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.dlg-title {
  margin-bottom: 10px;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.dlg-text {
  margin-bottom: 14px;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 12px;
}

.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.input {
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  font-family: var(--font-family);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
}

.input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.dlg-error {
  margin-bottom: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.btn {
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

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary.danger {
  border-color: var(--color-danger);
  background-color: var(--color-danger);
}
</style>
