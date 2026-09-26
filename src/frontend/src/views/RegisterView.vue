<template>
  <div class="login-page">
    <div class="login-card">
      <h1 class="login-title">全景相册</h1>
      <p class="login-subtitle">注册新账号</p>

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
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { register, getRegisterStatus } from '../api/admin'
import { errMessage } from '../stores/auth'

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
