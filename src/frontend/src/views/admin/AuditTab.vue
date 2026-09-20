<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">审计日志</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="audit-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <div class="filters">
      <label class="field">
        <span class="field-label">动作</span>
        <select v-model="actionFilter" data-testid="audit-action" @change="load">
          <option value="">全部</option>
          <option v-for="a in ACTIONS" :key="a" :value="a">{{ a }}</option>
        </select>
      </label>
      <label class="field">
        <span class="field-label">对象类型</span>
        <select v-model="targetFilter" data-testid="audit-target" @change="load">
          <option value="">全部</option>
          <option v-for="t in TARGETS" :key="t" :value="t">{{ t }}</option>
        </select>
      </label>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="audit-error">{{ err }}</p>

    <div v-if="items.length" class="table-wrap">
      <table class="table" data-testid="audit-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>动作</th>
            <th>操作者</th>
            <th>对象</th>
            <th>详情</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="e in items" :key="e.id" :data-testid="`audit-${e.id}`">
            <td>{{ formatTime(e.at) }}</td>
            <td><code class="action">{{ e.action }}</code></td>
            <td class="col-id">{{ e.actor_user_id || '—' }}</td>
            <td>{{ e.target_type || '—' }}<span v-if="e.target_id"> / {{ e.target_id }}</span></td>
            <td class="col-detail">{{ formatDetail(e.detail) }}</td>
          </tr>
        </tbody>
      </table>
      <div class="pager">
        <span class="pager-info">本页 {{ items.length }} 条 / 共 {{ total }} 条</span>
        <button
          class="btn btn--mini"
          type="button"
          :disabled="loading || !nextCursor"
          data-testid="audit-next"
          @click="load(nextCursor)"
        >
          下一页
        </button>
      </div>
    </div>
    <p v-else-if="!loading && !err" class="empty">没有匹配的审计记录</p>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { listAudit } from '../../api/admin'

// 与 internal/audit/audit.go 的动作/对象词表保持一致（后端 Normalize 会拒词表外取值）
const ACTIONS = [
  'auth.login', 'auth.logout', 'auth.token.rotate',
  'auth.mfa.setup', 'auth.mfa.enable', 'auth.mfa.disable',
  'user.password.change',
  'admin.user.create', 'admin.user.update', 'admin.user.delete', 'admin.role.change',
  'admin.audit.read', 'admin.settings.patch', 'admin.index.rebuild',
  'share.create', 'share.revoke',
  'media.delete', 'media.purge', 'media.restore'
]
const TARGETS = ['user', 'role', 'share', 'media', 'setting', 'compute_node', 'audit_log']

const items = ref([])
const total = ref(0)
const nextCursor = ref('')
const loading = ref(false)
const err = ref('')
const actionFilter = ref('')
const targetFilter = ref('')

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

// detail 是任意 JSON 对象；审计行要能在表格里快速扫读 → 紧凑 JSON，截断防超长
function formatDetail(detail) {
  if (!detail || typeof detail !== 'object' || !Object.keys(detail).length) return '—'
  try {
    const s = JSON.stringify(detail)
    return s.length > 120 ? `${s.slice(0, 120)}…` : s
  } catch {
    return '—'
  }
}

async function load(cursor = '') {
  loading.value = true
  err.value = ''
  try {
    const params = { limit: 50 }
    if (actionFilter.value) params.action = actionFilter.value
    if (targetFilter.value) params.target_type = targetFilter.value
    if (cursor) params.cursor = cursor
    const r = await listAudit(params)
    items.value = r.items || []
    total.value = r.total ?? 0
    nextCursor.value = r.next_cursor || ''
  } catch (e) {
    err.value = errMessage(e, '加载审计日志失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => load())
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
.filters {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 12px;
}
.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.field-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
select {
  padding: 7px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background: var(--color-surface);
}
.table-wrap {
  overflow-x: auto;
}
.table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-md);
}
.table th,
.table td {
  text-align: left;
  padding: 8px 10px;
  border-bottom: 1px solid var(--color-border);
  color: var(--color-text-primary);
}
.table th {
  color: var(--color-text-secondary);
  font-weight: 500;
  white-space: nowrap;
}
.col-id {
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.col-detail {
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-secondary);
  font-family: monospace;
  font-size: var(--font-size-sm);
}
.action {
  font-size: var(--font-size-sm);
  background: var(--color-surface-hover);
  padding: 2px 6px;
  border-radius: var(--radius-sm);
}
.pager {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 10px;
}
.pager-info {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.msg {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
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
.btn--mini {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}
.btn--ghost {
  border-color: transparent;
  color: var(--color-primary);
}
</style>
