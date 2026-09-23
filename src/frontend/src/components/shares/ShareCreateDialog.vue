<template>
  <div class="dlg-mask" @click.self="c.onCancel">
    <div class="share-dlg" role="dialog" aria-label="创建分享">
      <!-- 创建表单 -->
      <template v-if="!c.created.value">
        <h3 class="dlg-title">创建分享</h3>

        <label class="field">
          <span class="field-label">标题</span>
          <input v-model.trim="c.form.title" class="input" type="text" maxlength="60" placeholder="分享标题（可选）" />
        </label>

        <div class="field">
          <span class="field-label">有效期</span>
          <div class="expire-options">
            <label v-for="opt in expireOptions" :key="opt.label" class="radio-item">
              <input v-model="c.form.expireDays" type="radio" :value="opt.days" />
              <span>{{ opt.label }}</span>
            </label>
          </div>
        </div>

        <label class="field">
          <span class="field-label">访问密码（可选）</span>
          <input
            v-model.trim="c.form.password"
            class="input"
            type="text"
            maxlength="8"
            placeholder="4-8 位密码，留空则免密访问"
            data-testid="share-password"
          />
          <span v-if="c.passwordError.value" class="field-error" data-testid="share-pwd-error">{{ c.passwordError.value }}</span>
        </label>

        <label class="wechat-row">
          <input v-model="c.form.isWechat" type="checkbox" data-testid="share-wechat" />
          <span>微信 H5 分享</span>
        </label>
        <p v-if="c.form.isWechat" class="wechat-tip" data-testid="share-wechat-tip">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
            <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.6" />
            <path d="M12 8v5M12 16.5v.01" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          </svg>
          微信 H5 分享仅支持在线播放，不提供文件下载
        </p>

        <p v-if="c.submitError.value" class="submit-error">{{ c.submitError.value }}</p>

        <div class="dlg-actions">
          <button class="btn" @click="c.onCancel">取消</button>
          <button class="btn primary" data-testid="share-submit" :disabled="c.submitting.value || !!c.passwordError.value" @click="c.submit">
            {{ c.submitting.value ? '创建中…' : '创建分享' }}
          </button>
        </div>
      </template>

      <!-- 创建成功：链接展示面板（剪贴板交互自持） -->
      <ShareCreatedPanel
        v-else
        :created="c.created.value"
        :password="c.form.password"
        @cancel="c.onCancel"
      />
    </div>
  </div>
</template>

<script setup>
// 创建分享对话框宿主（Job000094：状态机/提交编排走 useShareCreate，成功态拆 ShareCreatedPanel）。
// 宿主职责：遮罩+对话框壳、创建表单模板、两态分发；密码校验/提交/payload 组装在 composable，
// 成功面板剪贴板交互全自持（084 生命周期内聚范式）。
import ShareCreatedPanel from './ShareCreatedPanel.vue'
import { useShareCreate, expireOptions } from './useShareCreate'

const props = defineProps({
  kind: { type: String, required: true }, // album | media
  targetId: { type: [String, Number], required: true },
  defaultTitle: { type: String, default: '' }
})

const emit = defineEmits(['cancel', 'created'])

const c = useShareCreate(props, emit)
</script>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.share-dlg {
  width: 420px;
  max-width: calc(100vw - 32px);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.dlg-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
}

.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.field-error {
  font-size: var(--font-size-sm);
  color: var(--color-danger);
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

.expire-options {
  display: flex;
  gap: 14px;
  flex-wrap: wrap;
}

.radio-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  cursor: pointer;
}

.wechat-row {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  cursor: pointer;
}

.wechat-tip {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 8px;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
}

.submit-error {
  margin-top: 12px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
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

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
