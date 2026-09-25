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

    <!-- 修改密码 -->
    <section class="card">
      <h2 class="card-title">修改密码</h2>
      <p class="card-desc">
        修改成功后，本账号在<strong>所有设备上的登录状态都会失效</strong>，需要用新密码重新登录
        —— 这样即使旧密码或旧会话曾经泄漏，也会随之作废。
      </p>

      <p v-if="pwMsg" class="msg" :class="pwMsgKind === 'error' ? 'msg--error' : 'msg--ok'" data-testid="pw-msg">{{ pwMsg }}</p>

      <div class="actions">
        <label class="inline-field">
          <span class="inline-label">当前密码</span>
          <input v-model="pwCurrent" data-testid="pw-current" type="password" autocomplete="current-password" />
        </label>
        <label class="inline-field">
          <span class="inline-label">新密码（至少 8 位）</span>
          <input v-model="pwNext" data-testid="pw-next" type="password" autocomplete="new-password" />
        </label>
        <label class="inline-field">
          <span class="inline-label">确认新密码</span>
          <input v-model="pwConfirm" data-testid="pw-confirm" type="password" autocomplete="new-password" />
        </label>
        <button class="btn" data-testid="pw-submit" :disabled="pwBusy || !pwCurrent || !pwNext || !pwConfirm" @click="onChangePassword">
          {{ pwBusy ? '提交中…' : '修改密码' }}
        </button>
      </div>
      <p class="hint">需要输入当前密码才能修改 —— 只凭开着的登录状态不能换锁。</p>
    </section>

    <!-- 二次验证（TOTP）——独立成卡（Job000058 拆分）；行为验证见 verify_2fa_ui.py -->
    <MfaSettingsCard />

    <!-- 应用密码（第三方客户端/WebDAV）——独立成卡（Job000098） -->
    <AppPasswordCard />

    <!-- 版本信息（Job000117）：当前版本 + 历史更新说明 -->
    <VersionCard />

    <!-- 操作手册（Job000119）：应用内帮助，使用方法 + 原理深入 -->
    <section class="card">
      <h2 class="card-title">帮助与操作手册</h2>
      <p class="card-desc">
        各功能的详细使用方法与原理说明：上传导入、相册与共享空间、地图、人物、语义搜索、分享、
        管理后台，以及「虚拟目录与物理存储的区别」等概念解析。
      </p>
      <div class="actions">
        <button class="btn" data-testid="open-manual" @click="router.push({ name: 'manual' })">打开操作手册</button>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore, errMessage, errCode } from '../stores/auth'
import MfaSettingsCard from './settings/MfaSettingsCard.vue'
import AppPasswordCard from './settings/AppPasswordCard.vue'
import VersionCard from './settings/VersionCard.vue'

const auth = useAuthStore()
const router = useRouter()

// ---- 修改密码（Job000034）----
const pwCurrent = ref('')
const pwNext = ref('')
const pwConfirm = ref('')
const pwBusy = ref(false)
const pwMsg = ref('')
const pwMsgKind = ref('ok')

async function onChangePassword() {
  pwMsg.value = ''
  // 客户端先挡一遍（服务端仍会再校验，这里的目的是少一次无效往返）
  if (pwNext.value.length < 8) {
    pwMsgKind.value = 'error'
    pwMsg.value = '新密码至少 8 位'
    return
  }
  if (pwNext.value !== pwConfirm.value) {
    pwMsgKind.value = 'error'
    pwMsg.value = '两次输入的新密码不一致'
    return
  }
  if (pwNext.value === pwCurrent.value) {
    pwMsgKind.value = 'error'
    pwMsg.value = '新密码不能与当前密码相同'
    return
  }
  pwBusy.value = true
  try {
    await auth.changePassword(pwCurrent.value, pwNext.value)
    // 服务端已吊销全部会话（含当前会话）——按后端契约立即登出并引导重新登录。
    // 先让用户看到成功提示再跳转，否则无声无息被踢回登录页会以为出了故障。
    pwMsgKind.value = 'ok'
    pwMsg.value = '密码已修改，所有登录状态已失效，即将跳转到登录页…'
    setTimeout(async () => {
      await auth.logout() // 会话已被服务端吊销，这里只为清理本地 token；失败不影响
      router.push({ name: 'login' })
    }, 1600)
  } catch (e) {
    pwMsg.value = errCode(e) === 'WRONG_PASSWORD'
      ? '当前密码不正确'
      : errMessage(e, '修改失败，请重试')
  } finally {
    pwBusy.value = false
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

input[type='password'] {
  height: 38px;
  width: 200px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  outline: none;
}

input[type='password']:focus {
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

.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
  line-height: 1.6;
}

.msg {
  margin: 12px 0 0;
  font-size: var(--font-size-sm);
}

.msg--error {
  color: var(--color-danger);
}

.msg--ok {
  color: var(--color-success);
}
</style>
