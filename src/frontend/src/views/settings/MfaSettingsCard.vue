<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">二次验证（TOTP）</h2>
      <span class="state" :class="stateClass" data-testid="mfa-state">{{ stateLabel }}</span>
    </div>
    <p class="card-desc">
      在认证器 App（Google Authenticator / Authy / 1Password / Bitwarden 等）里添加本账号，
      之后登录时除密码外还需输入 App 显示的 6 位动态口令。即使密码泄漏，攻击者仍进不来。
    </p>

    <p v-if="msg" class="msg" :class="msgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="mfa-msg">{{ msg }}</p>

    <!-- ① 已启用：关闭入口 -->
    <template v-if="auth.mfaEnabled">
      <div class="actions">
        <label class="inline-field">
          <span class="inline-label">当前动态口令</span>
          <input
            v-model.trim="code"
            data-testid="mfa-disable-code"
            type="text"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="7"
            placeholder="6 位数字"
          />
        </label>
        <button class="btn btn--danger" data-testid="mfa-disable" :disabled="busy || !code" @click="onDisable">
          {{ busy ? '处理中…' : '关闭二次验证' }}
        </button>
      </div>
      <!-- 关闭也必须输码：否则会话一旦泄漏，攻击者可以一键解除这道防线，
           而它恰恰是为「密码或会话已泄漏」这个场景准备的。 -->
      <p class="hint">关闭需要输入一次有效口令 —— 这样即使会话被窃取，对方也无法单方面解除保护。</p>
    </template>

    <!-- ② 未启用：启用入口（含待确认的密钥展示） -->
    <template v-else>
      <!-- 密钥只在生成时下发一次；刷新后拿不到，必须让用户能重新生成，否则会卡死在这里 -->
      <div v-if="!setup" class="actions">
        <button class="btn" data-testid="mfa-setup" :disabled="busy" @click="onStartSetup">
          {{ busy ? '生成中…' : (auth.mfaPending ? '重新生成密钥' : '启用二次验证') }}
        </button>
        <p v-if="auth.mfaPending" class="hint hint--warn">
          上一次设置没有完成确认。密钥只在生成时显示一次、服务端无法再取回，
          因此这里需要重新生成（旧的待确认密钥会被覆盖，不影响账号）。
        </p>
      </div>

      <MfaSetupWizard v-else :setup="setup" :busy="busy" @confirm="onConfirm" @cancel="onCancelSetup" />
    </template>
  </section>
</template>

<script setup>
// 二次验证（TOTP）设置卡——SettingsView 的拆分件（Job000058 超大文件拆分试点），
// 启用向导再下沉给 MfaSetupWizard。testid 全部保留（verify_2fa_ui.py 行为验证钉死它们）。
import { computed, ref } from 'vue'
import { useAuthStore, errMessage } from '../../stores/auth'
import MfaSetupWizard from './MfaSetupWizard.vue'

const auth = useAuthStore()

const setup = ref(null) // setup 响应：{secret, otpauth_url, digits, period}
const code = ref('')
const busy = ref(false)
const msg = ref('')
const msgKind = ref('ok')

const stateLabel = computed(() => {
  if (auth.mfaEnabled) return '已启用'
  if (auth.mfaPending) return '设置未完成'
  return '未启用'
})
const stateClass = computed(() => ({
  'state--on': auth.mfaEnabled,
  'state--warn': !auth.mfaEnabled && auth.mfaPending,
  'state--off': !auth.mfaEnabled && !auth.mfaPending
}))

function ok(text) {
  msgKind.value = 'ok'
  msg.value = text
}
function fail(e, fallback) {
  msgKind.value = 'error'
  msg.value = errMessage(e, fallback)
}

async function onStartSetup() {
  busy.value = true
  msg.value = ''
  try {
    setup.value = await auth.mfaSetup()
    code.value = ''
    ok('密钥已生成。请先把它加进认证器，再输入口令确认。')
  } catch (e) {
    fail(e, '生成密钥失败，请重试')
  } finally {
    busy.value = false
  }
}

async function onConfirm(codeFromWizard) {
  busy.value = true
  msg.value = ''
  try {
    await auth.mfaConfirm(codeFromWizard)
    setup.value = null
    code.value = ''
    ok('二次验证已启用。下次登录需要额外输入动态口令。')
  } catch (e) {
    fail(e, '口令不正确，请确认认证器里的数字后重试')
  } finally {
    busy.value = false
  }
}

function onCancelSetup() {
  setup.value = null
  code.value = ''
  msg.value = ''
}

async function onDisable() {
  busy.value = true
  msg.value = ''
  try {
    await auth.mfaDisable(code.value)
    code.value = ''
    ok('二次验证已关闭，该账号现在只需密码即可登录。')
  } catch (e) {
    fail(e, '关闭失败：请确认输入的是当前有效的动态口令')
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.card {
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: 20px;
  margin-bottom: 16px;
}

.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.card-title {
  margin: 0;
  font-size: 16px;
}

.card-desc {
  margin: 10px 0 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.6;
}

.state {
  font-size: var(--font-size-sm);
  padding: 2px 8px;
  border-radius: 999px;
  border: 1px solid var(--color-border);
}

.state--on {
  color: var(--color-success, #2e7d32);
  border-color: var(--color-success, #2e7d32);
}

.state--warn {
  color: var(--color-warning, #b26a00);
  border-color: var(--color-warning, #b26a00);
}

.state--off {
  color: var(--color-text-secondary);
}

.actions {
  display: flex;
  align-items: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  margin-top: 14px;
}

.inline-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
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

.btn--danger {
  background-color: var(--color-danger);
}

.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.hint--warn {
  color: var(--color-warning, #b26a00);
}

.msg {
  margin: 12px 0 0;
  font-size: var(--font-size-sm);
}

.msg--error {
  color: var(--color-danger);
}

.msg--ok {
  color: var(--color-success, #2e7d32);
}
</style>
