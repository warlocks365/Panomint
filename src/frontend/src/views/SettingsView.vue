<template>
  <div class="settings-page">
    <header class="page-head">
      <h1 class="page-title">设置</h1>
      <p class="page-sub">账号与安全</p>
    </header>

    <!-- 账号信息 -->
    <section class="card">
      <h2 class="card-title">账号</h2>
      <dl class="info-list">
        <div class="info-row">
          <dt>邮箱</dt>
          <dd>{{ auth.user?.email || '—' }}</dd>
        </div>
        <div class="info-row">
          <dt>昵称</dt>
          <dd>{{ auth.user?.display_name || '—' }}</dd>
        </div>
        <div class="info-row">
          <dt>角色</dt>
          <dd>{{ auth.user?.role || '—' }}</dd>
        </div>
      </dl>
    </section>

    <!-- 二次验证 -->
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

        <div v-else class="setup">
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
                <button class="btn" data-testid="mfa-confirm" :disabled="busy || !code" @click="onConfirm">
                  {{ busy ? '校验中…' : '确认启用' }}
                </button>
                <button class="btn btn--ghost" data-testid="mfa-cancel" :disabled="busy" @click="onCancelSetup">取消</button>
              </div>
              <p class="hint">
                确认前二次验证<b>不会生效</b>，所以就算这一步失败，你依然可以用密码正常登录。
              </p>
            </li>
          </ol>
        </div>

        <!-- 未接二维码：渲染二维码需要一个 QR 编码器（当前前端没有该依赖），
             而把密钥发给第三方二维码服务会直接泄漏密钥 —— 绝不这么做。
             手动输入密钥在所有认证器里都支持，功能完整。 -->
        <p class="hint hint--muted">
          提示：当前版本需<b>手动输入密钥</b>（认证器均支持）。二维码渲染待接入本地 QR 编码器后再加。
        </p>
      </template>
    </section>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useAuthStore, errMessage } from '../stores/auth'

const auth = useAuthStore()

const setup = ref(null) // setup 响应：{secret, otpauth_url, digits, period}
const code = ref('')
const busy = ref(false)
const msg = ref('')
const msgKind = ref('ok')
const copyLabel = ref('复制')
const secretEl = ref(null)

// 与后端 DefaultMFAIssuer 保持一致（仅用于这里的文案展示）
const ISSUER = '全景相册 Panomint'
const issuerLabel = computed(() => ISSUER)

// 4 位分组显示，和认证器界面一致，降低手抄出错率
const groupedSecret = computed(() => {
  const s = setup.value?.secret || ''
  return s.replace(/(.{4})/g, '$1 ').trim()
})

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

async function onConfirm() {
  busy.value = true
  msg.value = ''
  try {
    await auth.mfaConfirm(code.value)
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

// 复制密钥。
// ⚠️ navigator.clipboard 只在**安全上下文**（HTTPS 或 localhost）可用，
// 而本项目在局域网里走的是 http://<ip>:8088 —— 此时它不存在。
// 所以必须有降级路径：选中文本让用户自己按 Ctrl/Cmd+C，而不是点了没反应。
async function onCopy() {
  const secret = setup.value?.secret || ''
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
.settings-page {
  max-width: 720px;
  margin: 0 auto;
  padding: 24px 20px 48px;
}

.page-head {
  margin-bottom: 20px;
}

.page-title {
  margin: 0;
  font-size: 22px;
}

.page-sub {
  margin: 4px 0 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

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

.info-list {
  margin: 12px 0 0;
}

.info-row {
  display: flex;
  gap: 12px;
  padding: 6px 0;
  font-size: var(--font-size-sm);
}

.info-row dt {
  width: 72px;
  color: var(--color-text-secondary);
  flex: none;
}

.info-row dd {
  margin: 0;
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

.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.hint--warn {
  color: var(--color-warning, #b26a00);
}

.hint--muted {
  opacity: 0.8;
}

.link {
  color: var(--color-primary);
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
