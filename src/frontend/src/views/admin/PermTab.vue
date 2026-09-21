<template>
  <!-- 权限管理（Job000067 / F1）：权限词典（中文化/自定义分类）+ 用户级增量授权。
       展示层在此收敛；enforcement 不变（role_permissions + 服务端白名单）。 -->
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">权限管理</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="perms-reload" @click="loadAll">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="perms-error">{{ err }}</p>
    <p v-if="msg" class="msg msg--ok" data-testid="perms-msg">{{ msg }}</p>

    <!-- ①② 权限词典：中文名/分类/描述，可改（自定义分类=改 category 即建新组） -->
    <h3 class="sub-title">权限词典（中文名与分类可改；只影响展示，不改变权限本身）</h3>
    <div class="dict" data-testid="perm-dict">
      <div v-for="g in grouped" :key="g.category" class="dict-group">
        <h4 class="dict-cat">{{ g.category }}</h4>
        <div v-for="m in g.items" :key="m.perm" class="dict-row" :data-testid="`dict-${m.perm}`">
          <span class="dict-label">{{ m.label }}</span>
          <code class="dict-perm">{{ m.perm }}</code>
          <span v-if="m.orphan" class="dict-orphan">（白名单外，建议清理）</span>
          <span class="dict-desc">{{ m.description }}</span>
          <button class="btn btn--mini" :data-testid="`dict-edit-${m.perm}`" @click="onEdit(m)">编辑</button>
        </div>
      </div>
    </div>

    <!-- ③ 用户级增量授权：选用户 → 授予/收回（只能授予自己已有的权限，服务端强校验） -->
    <h3 class="sub-title">用户临时授权（在角色权限之上增量授予，适合项目制协作）</h3>
    <div class="grant-row">
      <select v-model="grantUser" class="sel" data-testid="grant-user">
        <option value="" disabled>选择用户</option>
        <option v-for="u in users" :key="u.id" :value="u.id">{{ u.display_name || u.email }}（{{ u.role }}）</option>
      </select>
      <button class="btn" :disabled="!grantUser" data-testid="grant-load" @click="loadUserPerms">查看授权</button>
    </div>

    <div v-if="grantUser" class="grant-panel" data-testid="grant-panel">
      <p class="field-label">已额外授予：</p>
      <div class="perm-chips">
        <span v-for="p in userPerms" :key="p" class="chip chip--granted">
          {{ labelOf(p) }}
          <button class="chip-x" :data-testid="`revoke-${p}`" @click="onRevoke(p)">×</button>
        </span>
        <span v-if="!userPerms.length" class="dict-desc">（无额外授权，全部跟随角色）</span>
      </div>
      <div class="grant-add">
        <select v-model="grantPerm" class="sel" data-testid="grant-perm">
          <option value="" disabled>选择要授予的权限</option>
          <option v-for="p in grantablePerms" :key="p.perm" :value="p.perm">{{ p.label }}（{{ p.perm }}）</option>
        </select>
        <button class="btn btn--primary" :disabled="!grantPerm" data-testid="grant-submit" @click="onGrant">授予</button>
      </div>
    </div>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useAuthStore, errMessage } from '../../stores/auth'
import { listPermMeta, listRoles, listUsers, listUserPerms, putUserPerm, putPermMeta } from '../../api/admin'

const auth = useAuthStore()
const loading = ref(false)
const err = ref('')
const msg = ref('')
const meta = ref([])
const users = ref([])
const roles = ref([])

const grantUser = ref('')
const grantPerm = ref('')
const userPerms = ref([])

// 分组展示（自定义分类在此生效）
const grouped = computed(() => {
  const m = new Map()
  for (const p of meta.value) {
    if (!m.has(p.category)) m.set(p.category, [])
    m.get(p.category).push(p)
  }
  return Array.from(m.entries()).map(([category, items]) => ({ category, items }));
})

function labelOf(perm) {
  const m = meta.value.find((x) => x.perm === perm)
  return m ? m.label : perm
}

// 可授予集 = 调用者角色权限（与后端 PermCovered 同一口径）
const grantablePerms = computed(() => {
  const mine = roles.value.find((r) => r.name === auth.user?.role)
  const perms = mine ? mine.permissions : []
  return meta.value.filter((m) => !m.orphan && perms.includes(m.perm))
})

function flash(text) {
  msg.value = text
  setTimeout(() => {
    if (msg.value === text) msg.value = ''
  }, 4000)
}

async function loadAll() {
  loading.value = true
  err.value = ''
  try {
    const [m, u, r] = await Promise.all([listPermMeta(), listUsers(), listRoles()])
    meta.value = m.perms || []
    users.value = u.users || []
    roles.value = r.roles || []
  } catch (e) {
    err.value = errMessage(e, '加载失败')
  } finally {
    loading.value = false
  }
}

async function onEdit(m) {
  const label = window.prompt('中文名', m.label)
  if (label === null) return
  const category = window.prompt('分类（可输入新分类名）', m.category)
  if (category === null) return
  const description = window.prompt('描述', m.description || '')
  if (description === null) return
  try {
    await putPermMeta(m.perm, {
      label: label.trim() || m.label,
      category: category.trim() || m.category,
      description: description.trim()
    })
    flash(`已更新 ${m.perm} 的展示信息`)
    await loadAll()
  } catch (e) {
    err.value = errMessage(e, '保存失败')
  }
}

async function loadUserPerms() {
  if (!grantUser.value) return
  try {
    const r = await listUserPerms(grantUser.value)
    userPerms.value = r.perms || []
  } catch (e) {
    err.value = errMessage(e, '读取用户授权失败')
  }
}

async function onGrant() {
  if (!grantUser.value || !grantPerm.value) return
  try {
    await putUserPerm(grantUser.value, grantPerm.value, true)
    flash('已授予（立即生效）')
    grantPerm.value = ''
    await loadUserPerms()
  } catch (e) {
    err.value = errMessage(e, '授予失败（只能授予自己已有的权限）')
  }
}

async function onRevoke(perm) {
  try {
    await putUserPerm(grantUser.value, perm, false)
    flash('已收回')
    await loadUserPerms()
  } catch (e) {
    err.value = errMessage(e, '收回失败')
  }
}

onMounted(loadAll)
</script>

<style scoped>
.card { background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-md); padding: 20px; }
.card-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.card-title { margin: 0; font-size: var(--font-size-lg); color: var(--color-text-primary); }
.sub-title { margin: 16px 0 10px; font-size: var(--font-size-md); color: var(--color-text-primary); }
.dict { display: flex; flex-direction: column; gap: 12px; }
.dict-cat { margin: 0 0 6px; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.dict-row { display: flex; align-items: center; gap: 10px; padding: 6px 8px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); margin-bottom: 4px; flex-wrap: wrap; }
.dict-label { font-size: var(--font-size-md); color: var(--color-text-primary); min-width: 90px; }
.dict-perm { font-size: 12px; color: var(--color-text-secondary); }
.dict-orphan { font-size: 12px; color: var(--color-danger); }
.dict-desc { flex: 1; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.grant-row { display: flex; gap: 10px; margin-bottom: 10px; }
.grant-panel { border: 1px solid var(--color-border); border-radius: var(--radius-sm); padding: 10px 12px; }
.grant-add { display: flex; gap: 10px; margin-top: 8px; }
.perm-chips { display: flex; flex-wrap: wrap; gap: 6px; margin: 6px 0; }
.chip { display: inline-flex; align-items: center; gap: 4px; font-size: var(--font-size-sm); background: var(--color-primary-active-bg); color: var(--color-primary); border-radius: 999px; padding: 2px 10px; }
.chip-x { border: none; background: none; color: inherit; cursor: pointer; font-size: 13px; padding: 0; }
.sel { padding: 7px 10px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); font-size: var(--font-size-md); color: var(--color-text-primary); background: var(--color-surface); }
.btn { padding: 6px 14px; border: 1px solid var(--color-border); border-radius: var(--radius-sm); background: var(--color-surface); color: var(--color-text-primary); font-size: var(--font-size-md); cursor: pointer; }
.btn:hover { background: var(--color-surface-hover); }
.btn:disabled { opacity: 0.6; cursor: default; }
.btn--primary { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-surface); }
.btn--ghost { border-color: transparent; color: var(--color-primary); }
.btn--mini { padding: 2px 8px; font-size: var(--font-size-sm); }
.msg { margin: 0 0 8px; font-size: var(--font-size-md); }
.msg--error { color: var(--color-danger); }
.msg--ok { color: var(--color-success); }
.field-label { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
</style>
