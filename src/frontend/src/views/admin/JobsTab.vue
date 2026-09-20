<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">任务队列</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="jobs-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <div class="filters">
      <label class="field">
        <span class="field-label">类型</span>
        <select v-model="typeFilter" data-testid="jobs-type" @change="load">
          <option value="all">全部</option>
          <option value="index">索引</option>
          <option value="transcode">转码</option>
        </select>
      </label>
      <label class="field">
        <span class="field-label">状态</span>
        <select v-model="statusFilter" data-testid="jobs-status" @change="load">
          <option value="">全部</option>
          <option value="done">完成</option>
          <option value="failed">失败</option>
          <option value="pending">排队中</option>
          <option value="processing">处理中</option>
        </select>
      </label>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="jobs-error">{{ err }}</p>
    <p v-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法查看任务队列。</p>

    <div v-if="items.length" class="table-wrap">
      <table class="table" data-testid="jobs-table">
        <thead>
          <tr>
            <th>类型</th>
            <th>任务</th>
            <th>状态</th>
            <th>进度</th>
            <th>媒体</th>
            <th>开始时间</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="j in items" :key="j.job_type + j.id" :data-testid="`job-${j.id}`">
            <td>{{ j.job_type }}</td>
            <td>{{ j.kind }}</td>
            <td>
              <span class="state" :class="stateClass(j.status)">{{ j.status }}</span>
            </td>
            <td>
              <template v-if="j.progress != null">{{ Math.round(j.progress * 100) }}%</template>
              <template v-else-if="j.total != null">{{ j.processed ?? 0 }}/{{ j.total }}</template>
              <template v-else>—</template>
            </td>
            <td class="col-id">{{ j.media_id || '—' }}</td>
            <td>{{ formatTime(j.started_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="!loading && !err && !forbidden" class="empty">没有匹配的任务</p>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { listJobs } from '../../api/admin'

const items = ref([])
const loading = ref(false)
const err = ref('')
const forbidden = ref(false)
const typeFilter = ref('all')
const statusFilter = ref('')

function stateClass(s) {
  return {
    done: 'state--ok',
    failed: 'state--err',
    processing: 'state--run',
    pending: 'state--wait'
  }[s] || ''
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

async function load() {
  loading.value = true
  err.value = ''
  try {
    const params = { type: typeFilter.value }
    if (statusFilter.value) params.status = statusFilter.value
    const r = await listJobs(params)
    items.value = r.items || []
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = errMessage(e, '加载任务列表失败')
    }
  } finally {
    loading.value = false
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
.filters {
  display: flex;
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
  white-space: nowrap;
}
.table th {
  color: var(--color-text-secondary);
  font-weight: 500;
}
.col-id {
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}
.state {
  font-size: var(--font-size-sm);
}
.state--ok {
  color: var(--color-success);
}
.state--err {
  color: var(--color-danger);
}
.state--run {
  color: var(--color-primary);
}
.state--wait {
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
.btn--ghost {
  border-color: transparent;
  color: var(--color-primary);
}
</style>
