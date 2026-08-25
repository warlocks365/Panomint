<template>
  <div class="dlg-mask" @click.self="onCancel">
    <div class="share-dlg" role="dialog" aria-label="创建分享">
      <!-- 创建表单 -->
      <template v-if="!created">
        <h3 class="dlg-title">创建分享</h3>

        <label class="field">
          <span class="field-label">标题</span>
          <input v-model.trim="form.title" class="input" type="text" maxlength="60" placeholder="分享标题（可选）" />
        </label>

        <div class="field">
          <span class="field-label">有效期</span>
          <div class="expire-options">
            <label v-for="opt in expireOptions" :key="opt.label" class="radio-item">
              <input v-model="form.expireDays" type="radio" :value="opt.days" />
              <span>{{ opt.label }}</span>
            </label>
          </div>
        </div>

        <label class="field">
          <span class="field-label">访问密码（可选）</span>
          <input
            v-model.trim="form.password"
            class="input"
            type="text"
            maxlength="8"
            placeholder="4-8 位密码，留空则免密访问"
          />
          <span v-if="passwordError" class="field-error">{{ passwordError }}</span>
        </label>

        <label class="wechat-row">
          <input v-model="form.isWechat" type="checkbox" />
          <span>微信 H5 分享</span>
        </label>
        <p v-if="form.isWechat" class="wechat-tip">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
            <circle cx="12" cy="12" r="9" stroke="currentColor" stroke-width="1.6" />
            <path d="M12 8v5M12 16.5v.01" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          </svg>
          微信 H5 分享仅支持在线播放，不提供文件下载
        </p>

        <p v-if="submitError" class="submit-error">{{ submitError }}</p>

        <div class="dlg-actions">
          <button class="btn" @click="onCancel">取消</button>
          <button class="btn primary" :disabled="submitting || !!passwordError" @click="submit">
            {{ submitting ? '创建中…' : '创建分享' }}
          </button>
        </div>
      </template>

      <!-- 创建成功：展示链接 -->
      <template v-else>
        <h3 class="dlg-title">分享创建成功</h3>
        <p class="success-desc">复制以下链接发送给好友，对方无需登录即可访问：</p>
        <div class="link-row">
          <input class="input link-input" type="text" readonly :value="createdLink" @focus="$event.target.select()" />
          <button class="btn primary" @click="copyLink">{{ copied ? '已复制' : '复制' }}</button>
        </div>
        <p v-if="form.password" class="pwd-hint">访问密码：{{ form.password }}</p>
        <div class="dlg-actions">
          <button class="btn primary" @click="onCancel">完成</button>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { createShare, errMsg, shareLink } from './shareApi'

const props = defineProps({
  kind: { type: String, required: true }, // album | media
  targetId: { type: [String, Number], required: true },
  defaultTitle: { type: String, default: '' }
})

const emit = defineEmits(['cancel', 'created'])

const expireOptions = [
  { label: '1 天', days: 1 },
  { label: '7 天', days: 7 },
  { label: '30 天', days: 30 },
  { label: '永久', days: 0 }
]

const form = reactive({
  title: props.defaultTitle,
  expireDays: 7,
  password: '',
  isWechat: false
})

const submitting = ref(false)
const submitError = ref('')
const created = ref(null)
const copied = ref(false)

const passwordError = computed(() => {
  if (!form.password) return ''
  if (form.password.length < 4) return '密码长度需为 4-8 位'
  return ''
})

const createdLink = computed(() => (created.value ? shareLink(created.value.token) : ''))

async function submit() {
  if (submitting.value || passwordError.value) return
  submitting.value = true
  submitError.value = ''
  try {
    const payload = { kind: props.kind, target_id: props.targetId }
    if (form.title) payload.title = form.title
    if (form.expireDays > 0) {
      payload.expire_at = new Date(Date.now() + form.expireDays * 86400000).toISOString()
    }
    if (form.password) payload.password = form.password
    if (form.isWechat) payload.is_wechat = true
    created.value = await createShare(payload)
    emit('created', created.value)
  } catch (e) {
    submitError.value = errMsg(e, '创建分享失败')
  } finally {
    submitting.value = false
  }
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(createdLink.value)
  } catch (e) {
    // 剪贴板 API 不可用（非安全上下文等）时回退
    const ta = document.createElement('textarea')
    ta.value = createdLink.value
    document.body.appendChild(ta)
    ta.select()
    document.execCommand('copy')
    document.body.removeChild(ta)
  }
  copied.value = true
  setTimeout(() => (copied.value = false), 2000)
}

function onCancel() {
  emit('cancel')
}
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

.success-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  margin-bottom: 12px;
}

.link-row {
  display: flex;
  gap: 8px;
}

.link-input {
  flex: 1;
  min-width: 0;
  color: var(--color-text-secondary);
}

.pwd-hint {
  margin-top: 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
</style>
