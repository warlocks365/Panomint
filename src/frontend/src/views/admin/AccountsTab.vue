<template>
  <div>
    <!-- 账号策略（Job000128 点 6/7/8） -->
    <section class="card">
      <div class="card-head">
        <h2 class="card-title">账号策略</h2>
        <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="acct-reload" @click="load">
          {{ loading ? '加载中…' : '刷新' }}
        </button>
      </div>

      <p v-if="err" class="msg msg--error" data-testid="acct-error">{{ err }}</p>
      <p v-if="msg" class="msg msg--ok" data-testid="acct-msg">{{ msg }}</p>

      <form v-if="policy" class="policy-form" data-testid="acct-form" @submit.prevent="onSave">
        <!-- 点 6：注册开关 -->
        <fieldset class="fieldset">
          <legend class="legend">新用户注册</legend>
          <label class="check-row">
            <input v-model="policy.allow_registration" type="checkbox" data-testid="acct-allow-reg" />
            <span>允许新用户自助注册（默认关闭；关闭时注册页自动隐藏）</span>
          </label>
          <label class="check-row" :class="{ 'check-row--off': !policy.allow_registration }">
            <input
              v-model="policy.invite_required"
              type="checkbox"
              data-testid="acct-invite-required"
              :disabled="!policy.allow_registration"
            />
            <span>注册需要邀请码（管理员在下方生成后发给受邀人，一码一用）</span>
          </label>
        </fieldset>

        <!-- 点 8：密码强度策略 -->
        <fieldset class="fieldset">
          <legend class="legend">密码强度策略（对注册与管理员建号生效）</legend>
          <label class="field field--narrow">
            <span class="field-label">最小长度（8–64）</span>
            <input
              v-model.number="policy.pwd_min_length"
              type="number"
              min="8"
              max="64"
              data-testid="acct-pwd-len"
            />
          </label>
          <div class="check-grid">
            <label class="check-row">
              <input v-model="policy.pwd_require_upper" type="checkbox" data-testid="acct-pwd-upper" />
              <span>必须包含大写字母</span>
            </label>
            <label class="check-row">
              <input v-model="policy.pwd_require_lower" type="checkbox" data-testid="acct-pwd-lower" />
              <span>必须包含小写字母</span>
            </label>
            <label class="check-row">
              <input v-model="policy.pwd_require_digit" type="checkbox" data-testid="acct-pwd-digit" />
              <span>必须包含数字</span>
            </label>
            <label class="check-row">
              <input v-model="policy.pwd_require_special" type="checkbox" data-testid="acct-pwd-special" />
              <span>必须包含特殊字符</span>
            </label>
          </div>
        </fieldset>

        <!-- 点 7：登录失败锁定 -->
        <fieldset class="fieldset">
          <legend class="legend">登录失败锁定</legend>
          <div class="row-2">
            <label class="field field--narrow">
              <span class="field-label">失败次数上限（1–50）</span>
              <input
                v-model.number="policy.login_max_attempts"
                type="number"
                min="1"
                max="50"
                data-testid="acct-max-attempts"
              />
            </label>
            <label class="field field--narrow">
              <span class="field-label">锁定时长（分钟，1–1440）</span>
              <input
                v-model.number="policy.login_lock_minutes"
                type="number"
                min="1"
                max="1440"
                data-testid="acct-lock-minutes"
              />
            </label>
          </div>
          <p class="hint">连续输错密码达到上限后，该账号暂时无法登录（即使密码正确），到期自动解除。</p>
        </fieldset>

        <button class="btn btn--primary" type="submit" :disabled="busy" data-testid="acct-save">
          {{ busy ? '保存中…' : '保存策略' }}
        </button>
      </form>
    </section>

    <!-- 邀请码管理 -->
    <section class="card">
      <div class="card-head">
        <h2 class="card-title">邀请码</h2>
        <button class="btn btn--primary" type="button" :disabled="creating" data-testid="invite-create" @click="onCreate">
          {{ creating ? '生成中…' : '生成邀请码（72 小时有效）' }}
        </button>
      </div>

      <p v-if="inviteErr" class="msg msg--error" data-testid="invite-error">{{ inviteErr }}</p>
      <p v-if="inviteMsg" class="msg msg--ok" data-testid="invite-msg">{{ inviteMsg }}</p>

      <div v-if="invites.length" class="invite-list" data-testid="invite-list">
        <div v-for="it in invites" :key="it.id" class="invite-item" :data-testid="`invite-${it.code}`">
          <code class="invite-code">{{ it.code }}</code>
          <span class="invite-meta">
            {{ it.used_by_email ? `已被 ${it.used_by_email} 使用` : `有效至 ${shortTime(it.expires_at)}` }}
          </span>
          <span class="invite-meta">
            {{ it.creator_email ? `由 ${it.creator_email} 创建` : '' }}
          </span>
          <span class="badge" :class="inviteState(it).cls" :data-testid="`invite-state-${it.code}`">
            {{ inviteState(it).text }}
          </span>
        </div>
      </div>
      <p v-else-if="!loading && !inviteErr" class="empty">暂无邀请码</p>
    </section>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { createInvite, getAccountConfig, listInvites, putAccountConfig } from '../../api/admin'

const policy = ref(null)
const loading = ref(false)
const busy = ref(false)
const err = ref('')
const msg = ref('')

const invites = ref([])
const creating = ref(false)
const inviteErr = ref('')
const inviteMsg = ref('')

function flash(text) {
  msg.value = text
  setTimeout(() => {
    if (msg.value === text) msg.value = ''
  }, 4000)
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const [cfg, inv] = await Promise.all([getAccountConfig(), listInvites()])
    policy.value = cfg.policy
    invites.value = inv.invites || []
  } catch (e) {
    err.value = errMessage(e, '加载账号策略失败')
  } finally {
    loading.value = false
  }
}

async function onSave() {
  busy.value = true
  err.value = ''
  try {
    const res = await putAccountConfig(policy.value)
    policy.value = res.policy
    flash('账号策略已保存，立即生效')
  } catch (e) {
    err.value = errMessage(e, '保存失败')
  } finally {
    busy.value = false
  }
}

async function onCreate() {
  creating.value = true
  inviteErr.value = ''
  inviteMsg.value = ''
  try {
    await createInvite(72)
    inviteMsg.value = '已生成邀请码（72 小时有效），点击条目即可复制'
    const inv = await listInvites()
    invites.value = inv.invites || []
  } catch (e) {
    inviteErr.value = errMessage(e, '生成邀请码失败')
  } finally {
    creating.value = false
  }
}

function inviteState(it) {
  if (it.used_by_email) return { text: '已使用', cls: 'badge--muted' }
  if (new Date(it.expires_at).getTime() <= Date.now()) return { text: '已过期', cls: 'badge--danger' }
  return { text: '可使用', cls: 'badge--ok' }
}

function shortTime(iso) {
  const d = new Date(iso)
  return `${d.getMonth() + 1}-${String(d.getDate()).padStart(2, '0')} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

onMounted(load)
</script>

<style scoped>
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
  margin-bottom: 16px;
}
.card-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}
.card-title {
  margin: 0;
  font-size: var(--font-size-md);
}
.policy-form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}
.fieldset {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: 12px 14px;
  margin: 0;
}
.legend {
  padding: 0 6px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.check-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--font-size-sm);
  padding: 4px 0;
  cursor: pointer;
}
.check-row--off {
  opacity: 0.55;
}
.check-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.field--narrow {
  max-width: 220px;
}
.field input {
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
}
.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.row-2 {
  display: flex;
  gap: 16px;
  flex-wrap: wrap;
}
.hint {
  margin: 8px 0 0;
  font-size: var(--font-size-xs, 12px);
  color: var(--color-text-secondary);
}
.msg {
  margin: 0 0 10px;
  font-size: var(--font-size-sm);
}
.msg--error {
  color: var(--color-danger);
}
.msg--ok {
  color: var(--color-ok, #2e7d32);
}
.invite-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.invite-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  flex-wrap: wrap;
}
.invite-code {
  font-family: var(--font-mono, monospace);
  user-select: all;
}
.invite-meta {
  color: var(--color-text-secondary);
}
.badge {
  padding: 2px 8px;
  border-radius: 999px;
  font-size: var(--font-size-xs, 12px);
}
.badge--ok {
  background: rgba(46, 125, 50, 0.12);
  color: var(--color-ok, #2e7d32);
}
.badge--danger {
  background: rgba(211, 47, 47, 0.12);
  color: var(--color-danger);
}
.badge--muted {
  background: var(--color-bg);
  color: var(--color-text-secondary);
}
.empty {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
</style>
