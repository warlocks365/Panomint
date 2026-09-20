<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">用户</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="users-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="users-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="users-msg">{{ msg }}</p>

    <!-- 创建用户（独立组件；守卫/加载仍在父级） -->
    <UserCreateForm :role-options="roleOptions" :busy="busy" :error="formErr" @submit="onCreate" />

    <!-- 重置密码：行内表单（选中目标后出现），避免原生 prompt（项目 P2 已登记其混用为缺陷） -->
    <form v-if="resetTarget" class="reset-form" data-testid="reset-form" @submit.prevent="onResetSubmit">
      <span class="reset-label">为 {{ resetTarget.email }} 设置新密码（至少 8 位）</span>
      <input
        v-model="resetPw"
        data-testid="reset-password"
        type="password"
        minlength="8"
        autocomplete="new-password"
        required
      />
      <button class="btn btn--primary btn--mini" type="submit" :disabled="busy" data-testid="reset-submit">
        确认重置
      </button>
      <button class="btn btn--mini" type="button" data-testid="reset-cancel" @click="resetTarget = null">
        取消
      </button>
    </form>

    <UsersTable
      v-if="users.length"
      :users="users"
      :role-options="roleOptions"
      :me-id="meId"
      :confirm-delete-id="confirmDeleteId"
      @role-change="onRoleChange"
      @toggle-status="onToggleStatus"
      @reset-password="onResetPassword"
      @delete="onDelete"
    />
    <p v-else-if="!loading && !err" class="empty">暂无用户</p>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { errCode, errMessage } from '../../stores/auth'
import { createUser, deleteUser, listRoles, listUsers, updateUser } from '../../api/admin'
import UserCreateForm from './UserCreateForm.vue'
import UsersTable from './UsersTable.vue'

const auth = useAuthStore()
const users = ref([])
const roles = ref([])
const loading = ref(false)
const busy = ref(false)
const err = ref('')
const msg = ref('')
const formErr = ref('')
const confirmDeleteId = ref('')
const resetTarget = ref(null)
const resetPw = ref('')

const meId = computed(() => auth.user?.id)
const roleOptions = computed(() => roles.value.map((r) => r.name))

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
    const [u, r] = await Promise.all([listUsers(), listRoles()])
    users.value = u.users || []
    roles.value = r.roles || []
  } catch (e) {
    err.value = errMessage(e, '加载用户列表失败')
  } finally {
    loading.value = false
  }
}

async function guard(fn, okText) {
  busy.value = true
  formErr.value = ''
  err.value = ''
  try {
    await fn()
    if (okText) flash(okText)
    await load()
    return true
  } catch (e) {
    const code = errCode(e)
    const text = errMessage(e, '操作失败')
    // 三条硬守卫的专属文案：自锁 / 最后 owner / 有资产不可删
    if (code === 'SELF_LOCKOUT' || code === 'LAST_OWNER' || code === 'USER_HAS_ASSETS') {
      err.value = text
    } else {
      formErr.value = text
    }
    return false
  } finally {
    busy.value = false
  }
}

function onCreate(payload, done) {
  guard(() => createUser(payload), `已创建用户 ${payload.email}`).then((ok) => {
    if (ok) done()
  })
}

function onRoleChange(u, role) {
  guard(() => updateUser(u.id, { role }), `已把 ${u.email} 的角色改为 ${role}`)
}

function onToggleStatus(u) {
  const next = u.status === 'active' ? 'disabled' : 'active'
  guard(() => updateUser(u.id, { status: next }), `已${next === 'disabled' ? '禁用' : '启用'} ${u.email}`)
}

function onResetPassword(u) {
  resetTarget.value = u
  resetPw.value = ''
}

function onResetSubmit() {
  const u = resetTarget.value
  if (!u) return
  guard(() => updateUser(u.id, { password: resetPw.value }), `已重置 ${u.email} 的密码`).then(
    (ok) => {
      if (ok) resetTarget.value = null
    }
  )
}

// 删除为硬删（有资产的账号会被 409 挡住并提示改用禁用）。两次点击确认防误触。
let confirmTimer = null
function onDelete(u) {
  if (confirmDeleteId.value !== u.id) {
    confirmDeleteId.value = u.id
    clearTimeout(confirmTimer)
    confirmTimer = setTimeout(() => {
      if (confirmDeleteId.value === u.id) confirmDeleteId.value = ''
    }, 4000)
    return
  }
  clearTimeout(confirmTimer)
  confirmDeleteId.value = ''
  guard(() => deleteUser(u.id, { disable: false }), `已删除用户 ${u.email}`)
}

onMounted(load)
</script>

<style scoped>
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
}
.card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}
.card-title {
  margin: 0;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
input,
select {
  padding: 7px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background: var(--color-surface);
}
input:focus,
select:focus {
  outline: 2px solid var(--color-primary-active-bg);
  border-color: var(--color-primary);
}
.msg {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}
.msg--ok {
  color: var(--color-success);
}
.empty {
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
}
.btn {
  padding: 6px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
}
.btn:hover {
  background: var(--color-surface-hover);
}
.btn:disabled {
  opacity: 0.6;
  cursor: default;
}
.btn--primary {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}
.btn--primary:hover {
  background: var(--color-primary-hover);
}
.btn--ghost {
  border-color: transparent;
  color: var(--color-primary);
}
.reset-form {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px;
  margin: 8px 0 12px;
  padding: 10px 12px;
  background: var(--color-primary-active-bg);
  border-radius: var(--radius-sm);
}
.reset-label {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
</style>
