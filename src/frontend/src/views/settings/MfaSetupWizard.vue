<template>
  <div class="setup">
    <ol class="steps">
      <li>
        <span class="step-label">在认证器里手动输入这个密钥</span>
        <div class="secret-box">
          <code class="secret" ref="secretEl" data-testid="mfa-secret">{{ groupedSecret }}</code>
          <button class="btn btn--ghost" type="button" @click="onCopy">{{ copyLabel }}</button>
        </div>
        <p class="hint">
          服务名 <b>{{ issuerLabel }}</b>，账户 <b>{{ auth.user?.email }}</b>，
          类型选择「基于时间 / TOTP」，位数 6，周期 30 秒。
        </p>
        <p class="hint">
          也可以点这里直接唤起认证器：
          <a class="link" :href="setup.otpauth_url">打开认证器 App</a>
          （在手机上有效；桌面浏览器可能无反应）
        </p>
      </li>
      <li>
        <span class="step-label">输入 App 中显示的口令完成确认</span>
        <div class="actions">
          <input
            v-model.trim="code"
            data-testid="mfa-confirm-code"
            type="text"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="7"
            placeholder="6 位数字"
          />
          <button class="btn" data-testid="mfa-confirm" :disabled="busy || !code" @click="$emit('confirm', code)">
            {{ busy ? '校验中…' : '确认启用' }}
          </button>
          <button class="btn btn--ghost" data-testid="mfa-cancel" :disabled="busy" @click="$emit('cancel')">取消</button>
        </div>
        <p class="hint">
          确认前二次验证<b>不会生效</b>，所以就算这一步失败，你依然可以用密码正常登录。
        </p>
      </li>
    </ol>
    <!-- 未接二维码：渲染二维码需要一个 QR 编码器（当前前端没有该依赖），
         而把密钥发给第三方二维码服务会直接泄漏密钥 —— 绝不这么做。
         手动输入密钥在所有认证器里都支持，功能完整。 -->
    <p class="hint hint--muted">
      提示：当前版本需<b>手动输入密钥</b>（认证器均支持）。二维码渲染待接入本地 QR 编码器后再加。
    </p>
  </div>
</template>

<script setup>
// MFA 启用向导（两步：密钥展示 → 口令确认）——MfaSettingsCard 的子组件（Job000058 拆分）。
// 复制安全语义（含 http 局域网 clipboard 降级路径）整体迁移，一行不改。
import { computed, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'

const props = defineProps({
  setup: { type: Object, required: true }, // {secret, otpauth_url, digits, period}
  busy: { type: Boolean, default: false }
})
defineEmits(['confirm', 'cancel'])

const auth = useAuthStore()
const code = ref('')
const copyLabel = ref('复制')
const secretEl = ref(null)

// 与后端 DefaultMFAIssuer 保持一致（仅用于这里的文案展示）
const ISSUER = '全景相册 Panomint'
const issuerLabel = computed(() => ISSUER)

// 4 位分组显示，和认证器界面一致，降低手抄出错率
const groupedSecret = computed(() => {
  const s = props.setup?.secret || ''
  return s.replace(/(.{4})/g, '$1 ').trim()
})

// 复制密钥。
// ⚠️ navigator.clipboard 只在**安全上下文**（HTTPS 或 localhost）可用，
// 而本项目在局域网里走的是 http://<ip>:8088 —— 此时它不存在。
// 所以必须有降级路径：选中文本让用户自己按 Ctrl/Cmd+C，而不是点了没反应。
async function onCopy() {
  const secret = props.setup?.secret || ''
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(secret)
      copyLabel.value = '已复制'
      setTimeout(() => (copyLabel.value = '复制'), 2000)
      return
    }
    throw new Error('clipboard unavailable')
  } catch {
    const el = secretEl.value
    if (el) {
      const range = document.createRange()
      range.selectNodeContents(el)
      const sel = window.getSelection()
      sel.removeAllRanges()
      sel.addRange(range)
    }
    copyLabel.value = '请按 Ctrl/Cmd+C'
    setTimeout(() => (copyLabel.value = '复制'), 3000)
  }
}
</script>

<style scoped>
.setup {
  margin-top: 14px;
}

.steps {
  margin: 0;
  padding-left: 20px;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.step-label {
  display: block;
  font-size: var(--font-size-sm);
  margin-bottom: 8px;
}

.secret-box {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.secret {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 15px;
  letter-spacing: 0.06em;
  padding: 8px 12px;
  background-color: var(--color-bg);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  user-select: all;
}

.actions {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
}

.inline-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

input[type='text'] {
  height: 38px;
  width: 160px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  outline: none;
}

input[type='text']:focus {
  border-color: var(--color-primary);
}

.btn {
  height: 38px;
  padding: 0 16px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-sm);
}

.btn:hover:not(:disabled) {
  background-color: var(--color-primary-hover);
}

.btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.btn--ghost {
  background-color: transparent;
  color: var(--color-text-primary, inherit);
  border-color: var(--color-border);
}

.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.hint--muted {
  opacity: 0.8;
}

.link {
  color: var(--color-primary);
}
</style>
