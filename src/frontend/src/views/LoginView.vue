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

        <label class="remember">
          <input v-model="remember" type="checkbox" />
          <span>记住我</span>
        </label>

        <p v-if="errorMsg" class="error-msg">{{ errorMsg }}</p>

        <button class="submit-btn" type="submit" :disabled="loading">
          {{ loading ? '登录中…' : '登录' }}
        </button>
      </form>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const auth = useAuthStore()

const email = ref('')
const password = ref('')
const remember = ref(true)
const loading = ref(false)
const errorMsg = ref('')

async function onSubmit() {
  errorMsg.value = ''
  loading.value = true
  try {
    await auth.login(email.value, password.value, remember.value)
    router.push('/timeline')
  } catch (e) {
    const msg = e.response?.data?.error?.message
    errorMsg.value = msg || '登录失败，请检查网络后重试'
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
</style>
