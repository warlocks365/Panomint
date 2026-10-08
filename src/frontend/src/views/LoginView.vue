<template>
  <div class="login-page">
    <span class="wordmark">PANOMINT</span>
    <div class="login-card">
      <!-- 品牌 Logo（Job000144）：72px 是全标记版的最低可辨高度（此尺寸下字标仍清晰）；
           透明底衬 --color-surface。居中与留白由组件的 block 形态提供。 -->
      <BrandLogo variant="full" :height="72" block alt="全景相册" />
      <p class="card-eyebrow">PANOMINT · 自托管全景相册</p>
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
        <template v-if="needCode">
          <div class="mfa-note">
            <p class="mfa-title">动态口令 · 已要求二次验证</p>
            <p class="mfa-desc">打开认证器 App，输入当前显示的 6 位数字</p>
          </div>
          <label class="field">
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
          </label>
        </template>

        <label class="remember">
          <input v-model="remember" type="checkbox" />
          <span>记住我</span>
        </label>

        <p v-if="errorMsg" class="error-msg" data-testid="login-error">{{ errorMsg }}</p>
        <p v-if="lockMsg" class="lock-msg" data-testid="login-locked">{{ lockMsg }}</p>

        <button class="submit-btn" data-testid="login-submit" type="submit" :disabled="loading || !!lockUntil">
          {{ loading ? '登录中…' : '登录' }}
        </button>

        <!-- 注册入口（Job000128 点 6）：仅当服务端注册开关开启时渲染 -->
        <p v-if="registerOpen" class="alt-link">
          还没有账号？
          <RouterLink to="/register" data-testid="login-to-register">注册新账号</RouterLink>
        </p>

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
    <span class="vermark">self-hosted</span>
  </div>
</template>

<script setup>
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import gsap from 'gsap'
import { useAuthStore, errCode, errMessage, MFA_REQUIRED, MFA_INVALID } from '../stores/auth'
import { getRegisterStatus } from '../api/admin'
import { safeInternalPath } from '../utils/url'
import BrandLogo from '../components/brand/BrandLogo.vue'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

// 登录成功后的落点（Job000125）：守卫拦截时带 ?redirect=<原目标>，
// 这里经 safeInternalPath 清洗后回跳原页 —— 拒绝外链/协议相对/回 login|setup
// 本身（开放重定向防御），非法或缺省一律落 /timeline。
// 注意：SSO 整页跳去 IdP 时 redirect 参数不跟随，回跳自然落回 /timeline，属预期。
function goAfterLogin() {
  const raw = typeof route.query.redirect === 'string' ? route.query.redirect : ''
  router.push(safeInternalPath(raw) || '/timeline')
}

const email = ref('')
const password = ref('')
const remember = ref(true)
const totpCode = ref('')
const needCode = ref(false)
const loading = ref(false)
const errorMsg = ref('')
const codeInput = ref(null)
const ssoEnabled = ref(false)
// ---- 账号锁定与注册入口（Job000128）----
const lockMsg = ref('')
const lockUntil = ref(0) // 锁定截止时间戳（ms）；0 = 未锁定。锁定期间禁用登录按钮
const lockTimer = ref(null)
const registerOpen = ref(false)

// 锁定倒计时：每秒刷新提示，到期自动解锁并清空提示。
// 不做"替用户隐藏锁定"——服务端才是裁决者，这里只是把 423 响应里的信息变得可读。
function startLockCountdown(seconds) {
  clearInterval(lockTimer.value)
  const deadline = Date.now() + seconds * 1000
  lockUntil.value = deadline
  const tick = () => {
    const left = Math.ceil((deadline - Date.now()) / 1000)
    if (left <= 0) {
      lockUntil.value = 0
      lockMsg.value = ''
      clearInterval(lockTimer.value)
      return
    }
    lockMsg.value = `登录失败次数过多，账户已暂时锁定，请约 ${Math.ceil(left / 60)} 分钟后再试`
  }
  tick()
  lockTimer.value = setInterval(tick, 1000)
}

// DESIGN.md §8 card-in：登录卡 y16 淡入（页面加载一次；reduced-motion 跳过）
let cardCtx = null
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    cardCtx = gsap.context(() => {
      gsap.from('.login-card', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out' })
    })
  })
})
onUnmounted(() => {
  if (cardCtx) cardCtx.revert()
})

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
      goAfterLogin()
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
  // 注册入口可见性（Job000128）：失败按关闭处理（入口不出现比出现一个点不动的入口好）
  try {
    registerOpen.value = !!(await getRegisterStatus())?.allow_registration
  } catch {
    registerOpen.value = false
  }
  // 刚注册成功跳转过来：给一条正向提示
  if (route.query.registered) {
    lockMsg.value = ''
    errorMsg.value = ''
  }
})

onUnmounted(() => clearInterval(lockTimer.value))

// 整页跳转到后端 /auth/sso/oidc/login（302 再到 IdP 授权页）。
// 不能用 axios/fetch：需要浏览器真实导航以维持 IdP 的会话 cookie。
function onSSO() {
  window.location.href = '/auth/sso/oidc/login'
}

async function onSubmit() {
  if (lockUntil.value) return // 锁定期间按钮已禁用，这里双保险
  errorMsg.value = ''
  loading.value = true
  try {
    // 未要求二次验证时不传 totp_code（后端字段可选，老流程完全不受影响）
    await auth.login(email.value, password.value, remember.value, needCode.value ? totpCode.value : '')
    goAfterLogin()
  } catch (e) {
    const code = errCode(e)
    if (code === 'ACCOUNT_LOCKED') {
      // 账号锁定（Job000128）：显示服务端文案并按 remaining_minute 跑倒计时，
      // 到期前禁用登录按钮——省得用户反复提交注定失败的请求。
      startLockCountdown((e?.response?.data?.remaining_minute || 1) * 60)
      errorMsg.value = ''
    } else if (code === MFA_REQUIRED) {
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
  position: relative;
  background-color: var(--color-bg);
  /* mist-wash：双团超淡雾色（bg 色相 ±3% 明度内，非装饰渐变横幅） */
  background-image: radial-gradient(900px 420px at 12% -8%, rgba(244, 241, 237, 0.55), transparent 60%),
    radial-gradient(760px 380px at 92% 108%, rgba(74, 90, 106, 0.06), transparent 60%);
}

.wordmark {
  position: absolute;
  top: 18px;
  left: 36px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.22em;
  color: var(--color-text-disabled);
}

.vermark {
  position: absolute;
  bottom: 16px;
  right: 36px;
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.1em;
  color: var(--color-text-disabled);
}

/* 卡片规范（DESIGN.md §4）：无边框米白面 + 双层漫射阴影 */
.login-card {
  width: 380px;
  padding: 40px 36px 30px;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.card-eyebrow {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.2em;
  text-align: center;
  color: var(--color-text-disabled);
  margin-bottom: 10px;
}

.login-title {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: 0.12em;
  text-align: center;
  color: var(--color-text-primary);
}

.login-subtitle {
  margin: 6px 0 26px;
  text-align: center;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
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

.lock-msg {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.alt-link {
  margin: 0;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.alt-link a {
  color: var(--color-primary);
  text-decoration: none;
}

.alt-link a:hover {
  text-decoration: underline;
}

.submit-btn {
  height: 40px;
  border: none;
  border-radius: 10px;
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-md);
  letter-spacing: 0.08em;
  transition: background-color 0.5s cubic-bezier(0.32, 0.72, 0, 1), transform 0.15s ease;
}

.submit-btn:active:not(:disabled) {
  transform: scale(0.98);
}

/* 动态口令按需提示块（燕麦左线，DESIGN.md 深化稿 VIEW.11） */
.mfa-note {
  border-left: 2px solid var(--stat-pano-photo);
  padding-left: 10px;
  margin-bottom: 14px;
}

.mfa-title {
  font-size: 11.5px;
  font-weight: 500;
  color: var(--color-warning-text);
}

.mfa-desc {
  font-size: 11px;
  color: var(--color-text-disabled);
  margin-top: 2px;
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
