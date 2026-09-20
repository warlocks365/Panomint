<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">角色</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="roles-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="roles-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="roles-msg">{{ msg }}</p>

    <div v-if="roles.length" class="role-list" data-testid="roles-list">
      <article v-for="r in roles" :key="r.name" class="role-item" :data-testid="`role-${r.name}`">
        <div class="role-head">
          <h3 class="role-name">{{ r.name }}</h3>
          <span class="role-count">{{ r.users }} 个用户</span>
        </div>
        <p v-if="r.description" class="role-desc">{{ r.description }}</p>
        <div class="perm-chips">
          <span v-for="p in r.permissions" :key="p" class="chip">{{ p }}</span>
          <span v-if="!r.permissions.length" class="role-desc">无权限</span>
        </div>
      </article>
    </div>
    <p v-else-if="!loading && !err" class="empty">暂无角色</p>

    <!-- 新建角色：权限多选只允许授予调用者自己拥有的（服务端同样强校验，双重防提权） -->
    <form class="create-form" data-testid="role-create-form" @submit.prevent="onCreate">
      <h3 class="sub-title">新建角色</h3>
      <div class="form-row">
        <label class="field">
          <span class="field-label">角色名</span>
          <input v-model.trim="form.name" data-testid="rc-name" type="text" required />
        </label>
        <label class="field field--wide">
          <span class="field-label">描述</span>
          <input v-model.trim="form.description" data-testid="rc-desc" type="text" />
        </label>
      </div>
      <div class="perm-picker">
        <span class="field-label">权限（只能授予本账号已有的权限）</span>
        <div class="perm-options">
          <label v-for="p in grantable" :key="p" class="perm-option">
            <input v-model="form.permissions" type="checkbox" :value="p" :data-testid="`rc-perm-${p}`" />
            <span>{{ p }}</span>
          </label>
        </div>
      </div>
      <button class="btn btn--primary" type="submit" :disabled="busy" data-testid="rc-submit">
        {{ busy ? '创建中…' : '创建角色' }}
      </button>
      <p v-if="formErr" class="msg msg--error" data-testid="rc-error">{{ formErr }}</p>
    </form>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useAuthStore } from '../../stores/auth'
import { errMessage } from '../../stores/auth'
import { createRole, listRoles } from '../../api/admin'

const auth = useAuthStore()
const roles = ref([])
const loading = ref(false)
const busy = ref(false)
const err = ref('')
const msg = ref('')
const formErr = ref('')
const form = ref({ name: '', description: '', permissions: [] })

// 可授予权限 = 调用者自身角色权限（与后端 PermCovered 提权守卫同一口径）。
// 数据源：角色列表里本账号角色的权限集。
const grantable = computed(() => {
  const mine = roles.value.find((r) => r.name === auth.user?.role)
  return mine ? [...mine.permissions].sort() : []
})

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
    const r = await listRoles()
    roles.value = r.roles || []
  } catch (e) {
    err.value = errMessage(e, '加载角色列表失败')
  } finally {
    loading.value = false
  }
}

async function onCreate() {
  busy.value = true
  formErr.value = ''
  try {
    await createRole({
      name: form.value.name,
      description: form.value.description,
      permissions: form.value.permissions
    })
    flash(`已创建角色 ${form.value.name}`)
    form.value = { name: '', description: '', permissions: [] }
    await load()
  } catch (e) {
    formErr.value = errMessage(e, '创建角色失败')
  } finally {
    busy.value = false
  }
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
.role-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-bottom: 16px;
}
.role-item {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  padding: 12px 14px;
}
.role-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.role-name {
  margin: 0;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
.role-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.role-desc {
  margin: 4px 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.perm-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}
.chip {
  font-size: var(--font-size-sm);
  background: var(--color-primary-active-bg);
  color: var(--color-primary);
  border-radius: 999px;
  padding: 2px 10px;
}
.sub-title {
  margin: 16px 0 10px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
.create-form {
  border-top: 1px solid var(--color-border);
  padding-top: 8px;
}
.form-row {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 10px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 160px;
}
.field--wide {
  flex: 1;
}
.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
input {
  padding: 7px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
input:focus {
  outline: 2px solid var(--color-primary-active-bg);
  border-color: var(--color-primary);
}
.perm-picker {
  margin-bottom: 12px;
}
.perm-options {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 6px;
}
.perm-option {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
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
</style>
