<template>
  <div class="login-page">
    <div class="login-card">
      <h1 class="login-title">全景相册</h1>
      <p class="login-subtitle">登录你的账号</p>

      <form class="login-form" @submit.prevent="onSubmit">
        <label class="field">
          <span class="field-label">账号</span>
          <input
            v-model.trim="email"
            type="email"
            autocomplete="username"
            placeholder="邮箱地址"
            required
          />
        </label>

        <label class="field">
          <span class="field-label">密码</span>
          <input
            v-model="password"
            type="password"
            autocomplete="current-password"
            placeholder="密码"
            required
          />
        </label>

        <!-- 动态口令：仅在服务端明确要求时才出现（MFA_REQUIRED）。
             不在页面加载时就摆出一个空格子，免得没开二次验证的人以为自己漏填了什么。
             autocomplete="one-time-code" 让 iOS/Android 能从短信/验证码自动填充里认出它；
             inputmode="numeric" 在手机上直接弹数字键盘。 -->
        <label v-if="needCode" class="field">
          <span class="field-label">动态口令</span>
          <input
            ref="codeInput"
            v-model.trim="totpCode"
            data-testid="login-totp"
            type="text"
            inputmode="numeric"
            autocomplete="one-time-code"
            maxlength="7"
            placeholder="认证器中的 6 位数字"
          />
          <span class="field-hint">打开认证器 App，输入当前显示的 6 位数字</span>
        </label>

        <label class="remember">
          <input v-model="remember" type="checkbox" />
          <span>记住我</span>
        </label>

        <p v-if="errorMsg" class="error-msg" data-testid="login-error">{{ errorMsg }}</p>

        <button class="submit-btn" data-testid="login-submit" type="submit" :disabled="loading">
          {{ loading ? '登录中…' : '登录' }}
        </button>

        <!-- SSO 登录（Job000054）：仅当后端配置了 OIDC 才渲染。整页跳转到 IdP，
             授权后 IdP 带 code/state 跳回本页，由 onMounted 里的回调分支接手。 -->
        <template v-if="ssoEnabled">
          <div class="divider"><span>或</span></div>
          <button class="sso-btn" data-testid="sso-login" type="button" @click="onSSO">
            使用 SSO 登录
          </button>
        </template>
      </form>
    </div>
  </div>
</template>

<script setup>
import { nextTick, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore, errCode, errMessage, MFA_REQUIRED, MFA_INVALID } from '../stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const email = ref('')
const password = ref('')
const remember = ref(true)
const totpCode = ref('')
const needCode = ref(false)
const loading = ref(false)
const errorMsg = ref('')
const codeInput = ref(null)
const ssoEnabled = ref(false)

// SSO 回调（Job000054）：IdP 授权后跳回 /login?code=..&state=..。
// 有回调参数就直奔 token 交换，不渲染表单流程；失败提示与登录失败同区显示。
onMounted(async () => {
  const code = typeof route.query.code === 'string' ? route.query.code : ''
  const state = typeof route.query.state === 'string' ? route.query.state : ''
  if (code && state) {
    loading.value = true
    errorMsg.value = ''
    try {
      await auth.loginWithSSO(code, state)
      router.push('/timeline')
      return
    } catch (e) {
      errorMsg.value = errMessage(e, 'SSO 登录失败，请重试')
    } finally {
      loading.value = false
    }
  } else if (route.query.error) {
    // IdP 侧拒绝（access_denied 等）：不细分原因，同形提示。
    errorMsg.value = 'SSO 登录未完成'
  }
  ssoEnabled.value = await auth.fetchSSOEnabled()
})

// 整页跳转到后端 /auth/sso/oidc/login（302 再到 IdP 授权页）。
// 不能用 axios/fetch：需要浏览器真实导航以维持 IdP 的会话 cookie。
function onSSO() {
  window.location.href = '/auth/sso/oidc/login'
}

async function onSubmit() {
  errorMsg.value = ''
  loading.value = true
  try {
    // 未要求二次验证时不传 totp_code（后端字段可选，老流程完全不受影响）
    await auth.login(email.value, password.value, remember.value, needCode.value ? totpCode.value : '')
    router.push('/timeline')
  } catch (e) {
    const code = errCode(e)
    if (code === MFA_REQUIRED) {
      // 服务端说"该账户已启用二次验证"：展开口令框并把焦点送过去，
      // 用户不必再去猜自己为什么登不上。
      needCode.value = true
      errorMsg.value = errMessage(e, '该账户已启用二次验证，请输入动态口令')
      await nextTick()
      codeInput.value?.focus()
    } else if (code === MFA_INVALID) {
      // 口令错/过期：保留输入框，清空并重新聚焦，避免用户把旧码再提交一次。
      needCode.value = true
      totpCode.value = ''
      errorMsg.value = errMessage(e, '动态口令不正确或已过期，请重试')
      await nextTick()
      codeInput.value?.focus()
    } else {
      errorMsg.value = errMessage(e, '登录失败，请检查网络后重试')
    }
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-page {
  min-height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: var(--color-bg);
}

.login-card {
  width: 360px;
  padding: 40px 36px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.login-title {
  margin: 0;
  font-size: 24px;
  text-align: center;
}

.login-subtitle {
  margin: 8px 0 28px;
  text-align: center;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.login-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.field input {
  height: 38px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  outline: none;
}

.field input:focus {
  border-color: var(--color-primary);
}

.field-hint {
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
}

.remember {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  cursor: pointer;
}

.error-msg {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.submit-btn {
  height: 40px;
  border: none;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-md);
}

.submit-btn:hover:not(:disabled) {
  background-color: var(--color-primary-hover);
}

.submit-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

/* SSO 按钮（Job000054）：与主按钮同宽，弱化一级感 */
.divider {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 18px 0 14px;
  color: var(--color-text-disabled);
  font-size: var(--font-size-sm);
}
.divider::before,
.divider::after {
  content: '';
  flex: 1;
  height: 1px;
  background-color: var(--color-border);
}
.sso-btn {
  width: 100%;
  padding: 10px 0;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.sso-btn:hover {
  background-color: var(--color-surface-hover);
}
</style>
