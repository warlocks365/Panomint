<template>
  <div class="setup-page">
    <span class="wordmark">PANOMINT</span>
    <div class="setup-card">
      <!-- 品牌 Logo（Job000144）：置于两个分支之上，初始化中与完成后都可见 -->
      <BrandLogo variant="full" :height="72" block alt="全景相册" />
      <template v-if="!done">
        <p class="setup-eyebrow">PANOMINT · 首次初始化</p>
        <h1 class="setup-title">欢迎使用全景相册</h1>
        <p class="setup-subtitle">
          首次使用需要创建一个管理员账号<span v-if="version">（{{ version }}）</span>
        </p>

        <form class="setup-form" @submit.prevent="onSubmit">
          <label class="field">
            <span class="field-label">邮箱</span>
            <input
              v-model.trim="email"
              data-testid="setup-email"
              type="email"
              autocomplete="username"
              placeholder="作为登录账号"
              required
            />
          </label>

          <label class="field">
            <span class="field-label">显示名</span>
            <input
              v-model.trim="displayName"
              data-testid="setup-name"
              type="text"
              maxlength="64"
              placeholder="选填，默认可留空"
            />
          </label>

          <label class="field">
            <span class="field-label">密码</span>
            <input
              v-model="password"
              data-testid="setup-password"
              type="password"
              autocomplete="new-password"
              placeholder="至少 8 位"
              required
            />
          </label>

          <label class="field">
            <span class="field-label">确认密码</span>
            <input
              v-model="password2"
              data-testid="setup-password2"
              type="password"
              autocomplete="new-password"
              placeholder="再输入一遍"
              required
            />
          </label>

          <p v-if="errorMsg" class="error-msg" data-testid="setup-error">{{ errorMsg }}</p>

          <button class="submit-btn" data-testid="setup-submit" type="submit" :disabled="loading">
            {{ loading ? '创建中…' : '完成初始化' }}
          </button>
        </form>
      </template>

      <template v-else>
        <p class="setup-eyebrow">PANOMINT · 首次初始化</p>
        <h1 class="setup-title">初始化完成</h1>
        <p class="setup-subtitle">管理员账号已创建，现在可以登录了。</p>
        <button class="submit-btn" data-testid="setup-done" type="button" @click="goLogin">
          去登录
        </button>
      </template>
    </div>
    <span class="vermark">SETUP WIZARD · 仅首启可达</span>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import gsap from 'gsap'
import { useRouter } from 'vue-router'
import { errCode, errMessage } from '../stores/auth'
import { getSetupStatus, invalidateSetupStatus, runSetup } from '../api/setup'
// 路由闸门的 15s TTL 缓存（router/index.js）：初始化成功后必须主动失效，
// 否则接下来 15 秒内导航会被 setup 闸门反向弹回 /login（闸门认为已初始化）。
import { invalidateSetupCache } from '../router'
import BrandLogo from '../components/brand/BrandLogo.vue'

const router = useRouter()

const email = ref('')
const displayName = ref('')
const password = ref('')
const password2 = ref('')
const loading = ref(false)
const errorMsg = ref('')
const done = ref(false)
const version = ref('')

// 状态自检（后端为权威，前端守卫只是第一道闸）：
// 已初始化 → 直接去登录；查不到状态 → 页面仍可操作（提交时后端会把关）。
onMounted(async () => {
  try {
    const st = await getSetupStatus()
    if (st.initialized) {
      router.replace({ name: 'login' })
      return
    }
    version.value = st.version || ''
  } catch {
    // 状态查询失败不阻塞：表单照常渲染，提交由后端裁决
  }
})

// DESIGN.md §8 card-in：初始化卡 y16 淡入（reduced-motion 跳过）
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    gsap.from('.setup-card', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out', clearProps: 'transform,opacity' })
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
    await runSetup({
      email: email.value,
      display_name: displayName.value || undefined,
      password: password.value
    })
    invalidateSetupStatus()
    invalidateSetupCache() // 同步失效路由闸门缓存，跳转 /login 不再被弹回
    done.value = true
  } catch (e) {
    errorMsg.value = errMessage(e, '初始化失败，请重试')
    // SETUP_COMPLETED（别的窗口抢先完成）也视为完成 —— 引导不再出现，直接给登录入口。
    if (errCode(e) === 'SETUP_COMPLETED') {
      invalidateSetupStatus()
      invalidateSetupCache()
      done.value = true
    }
  } finally {
    loading.value = false
  }
}

function goLogin() {
  router.replace({ name: 'login' })
}
</script>

<style scoped>
.setup-page {
  min-height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
  background-color: var(--color-bg);
  /* mist-wash：双团超淡雾色（对齐登录页氛围） */
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

.setup-eyebrow {
  font-family: var(--font-mono);
  font-size: 10px;
  letter-spacing: 0.2em;
  text-align: center;
  color: var(--color-text-disabled);
  margin-bottom: 10px;
}

.setup-card {
  width: 380px;
  padding: 40px 36px;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

.setup-title {
  margin: 0;
  font-size: 20px;
  font-weight: 500;
  letter-spacing: 0.12em;
  text-align: center;
  color: var(--color-text-primary);
}

.setup-subtitle {
  margin: 8px 0 28px;
  text-align: center;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.setup-form {
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
  cursor: pointer;
}

.submit-btn:hover:not(:disabled) {
  background-color: var(--color-primary-hover);
}

.submit-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}
</style>
