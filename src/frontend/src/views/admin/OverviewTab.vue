<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">系统概览</h2>
      <button class="btn btn--ghost" type="button" :disabled="loading" data-testid="stats-reload" @click="load">
        {{ loading ? '加载中…' : '刷新' }}
      </button>
    </div>

    <p v-if="err" class="msg msg--error" data-testid="stats-error">{{ err }}</p>
    <p v-else-if="forbidden" class="msg msg--error">当前账号缺少 admin:system 权限，无法查看系统概览。</p>

    <template v-if="stats">
      <dl class="stat-grid">
        <div class="stat-item">
          <dt>媒体总数</dt>
          <dd data-testid="stats-media">{{ stats.media_total }}</dd>
        </div>
        <div class="stat-item">
          <dt>用户数</dt>
          <dd data-testid="stats-users">{{ stats.users }}</dd>
        </div>
        <div class="stat-item">
          <dt>存储占用</dt>
          <dd data-testid="stats-storage">{{ formatBytes(stats.storage_used) }}</dd>
        </div>
        <div class="stat-item">
          <dt>索引状态</dt>
          <dd data-testid="stats-index-state">
            {{ indexStateLabel(stats.index_status?.state) }}
            <span v-if="stats.index_status?.running" class="state state--running">
              运行中 {{ stats.index_status.running }}
            </span>
          </dd>
        </div>
      </dl>

      <div v-if="stats.index_status?.last_job" class="last-job">
        <h3 class="sub-title">最近一次索引任务</h3>
        <dl class="info-list">
          <div class="info-row">
            <dt>类型</dt>
            <dd>{{ stats.index_status.last_job.job_type }}</dd>
          </div>
          <div class="info-row">
            <dt>状态</dt>
            <dd>{{ jobStatusLabel(stats.index_status.last_job.status) }}</dd>
          </div>
          <div class="info-row">
            <dt>进度</dt>
            <dd>
              <template v-if="stats.index_status.last_job.progress != null">
                {{ Math.round(stats.index_status.last_job.progress * 100) }}%
              </template>
              <template v-else>—</template>
            </dd>
          </div>
          <div class="info-row">
            <dt>开始时间</dt>
            <dd>{{ formatTime(stats.index_status.last_job.started_at) }}</dd>
          </div>
        </dl>
      </div>
    </template>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { getStats } from '../../api/admin'
import { errMessage } from '../../stores/auth'

const stats = ref(null)
const loading = ref(false)
const err = ref('')
const forbidden = ref(false)

// 展示层中文映射（API 码值保持英文不动）：idle=最近一次任务完成且队列清空，属正常运行态
const INDEX_STATE_LABEL = { idle: '空闲', running: '运行中', failed: '失败', unknown: '未知' }
const JOB_STATUS_LABEL = { pending: '排队中', running: '运行中', done: '已完成', failed: '失败', canceled: '已取消' }

function indexStateLabel(s) {
  if (!s) return '—'
  return INDEX_STATE_LABEL[s] ?? s
}

function jobStatusLabel(s) {
  if (!s) return '—'
  return JOB_STATUS_LABEL[s] ?? s
}

function formatBytes(n) {
  if (!Number.isFinite(n) || n < 0) return '—'
  if (n < 1024) return `${n} B`
  const units = ['KiB', 'MiB', 'GiB', 'TiB']
  let v = n / 1024
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v.toFixed(1)} ${units[i]}`
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
    stats.value = await getStats()
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    } else {
      err.value = errMessage(e, '加载系统概览失败')
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
.stat-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin: 0 0 8px;
}
.stat-item {
  background: var(--color-surface-hover);
  border-radius: var(--radius-sm);
  padding: 12px 14px;
}
.stat-item dt {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 4px;
}
.stat-item dd {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--color-text-primary);
}
.state--running {
  margin-left: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-success);
}
.sub-title {
  margin: 16px 0 8px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
}
.info-list {
  margin: 0;
}
.info-row {
  display: flex;
  gap: 12px;
  padding: 6px 0;
  border-bottom: 1px solid var(--color-border);
}
.info-row:last-child {
  border-bottom: none;
}
.info-row dt {
  width: 90px;
  flex-shrink: 0;
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
}
.info-row dd {
  margin: 0;
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  word-break: break-all;
}
.msg {
  margin: 0 0 8px;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
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
