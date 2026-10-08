<template>
  <div class="login-page">
    <span class="wordmark">PANOMINT</span>
    <div class="login-card">
      <BrandLogo variant="full" :height="72" block alt="全景相册" />
      <p class="card-eyebrow">PANOMINT · 自托管全景相册</p>
      <h1 class="login-title">创建账号</h1>
      <p class="login-subtitle">注册后即可上传与管理你的媒体</p>

      <form class="login-form" @submit.prevent="onSubmit">
        <label class="field">
          <span class="field-label">邮箱</span>
          <input
            v-model.trim="email"
            type="email"
            autocomplete="username"
            placeholder="邮箱地址"
            data-testid="reg-email"
            required
          />
        </label>

        <label class="field">
          <span class="field-label">昵称（可选）</span>
          <input
            v-model.trim="displayName"
            type="text"
            maxlength="64"
            placeholder="显示名称"
            data-testid="reg-display-name"
          />
        </label>

        <!-- 邀请码：仅在服务端要求时出现（与登录页动态口令同一设计：
             不让不需要它的人看到多余的输入框） -->
        <label v-if="inviteRequired" class="field">
          <span class="field-label">邀请码</span>
          <input
            v-model.trim="inviteCode"
            type="text"
            placeholder="inv-…"
            data-testid="reg-invite"
            required
          />
          <span class="field-hint">请向管理员索取邀请码，每个邀请码只能使用一次</span>
        </label>

        <label class="field">
          <span class="field-label">密码</span>
          <input
            v-model="password"
            type="password"
            autocomplete="new-password"
            placeholder="密码"
            data-testid="reg-password"
            required
          />
        </label>

        <label class="field">
          <span class="field-label">确认密码</span>
          <input
            v-model="password2"
            type="password"
            autocomplete="new-password"
            placeholder="再次输入密码"
            data-testid="reg-password2"
            required
          />
        </label>

        <p v-if="errorMsg" class="error-msg" data-testid="reg-error">{{ errorMsg }}</p>

        <button class="submit-btn" data-testid="reg-submit" type="submit" :disabled="loading">
          {{ loading ? '注册中…' : '注册' }}
        </button>

        <p class="alt-link">
          已有账号？
          <RouterLink to="/login" data-testid="reg-to-login">去登录</RouterLink>
        </p>
      </form>
    </div>
    <span class="vermark">self-hosted</span>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import gsap from 'gsap'
import { useRouter } from 'vue-router'
import { register, getRegisterStatus } from '../api/admin'
import { errMessage } from '../stores/auth'
import BrandLogo from '../components/brand/BrandLogo.vue'

const router = useRouter()

const email = ref('')
const displayName = ref('')
const inviteCode = ref('')
const password = ref('')
const password2 = ref('')
const inviteRequired = ref(false)
const loading = ref(false)
const errorMsg = ref('')

// 注册是否开放由服务端裁决：状态查询失败（网络错/读库失败）按关闭处理——
// 直接把用户送回登录页（不渲染一个注定失败的表单）。
onMounted(async () => {
  try {
    const st = await getRegisterStatus()
    if (!st?.allow_registration) {
      router.replace('/login')
      return
    }
    inviteRequired.value = !!st?.invite_required
  } catch {
    router.replace('/login')
  }
})

// DESIGN.md §8 card-in：注册卡 y16 淡入（reduced-motion 跳过）
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    gsap.from('.login-card', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out', clearProps: 'transform,opacity' })
  })
})

async function onSubmit() {
  errorMsg.value = ''
  if (password.value !== password2.value) {
    errorMsg.value = '两次输入的密码不一致'
    return
  }
  loading.value = true
  try {
    await register(
      email.value,
      displayName.value,
      password.value,
      inviteRequired.value ? inviteCode.value : ''
    )
    // 注册成功 → 去登录（注册不自动登录：邀请制下管理员可能还要调整角色/配额）
    router.push({ path: '/login', query: { registered: '1' } })
  } catch (e) {
    errorMsg.value = errMessage(e, '注册失败，请稍后重试')
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
  gap: 14px;
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

.alt-link {
  margin: 4px 0 0;
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
</style>
