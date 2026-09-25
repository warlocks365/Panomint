<template>
  <section class="card">
    <div class="card-head">
      <h2 class="card-title">扫描导入</h2>
      <div class="head-actions">
        <button
          class="btn btn--primary"
          type="button"
          :disabled="running"
          data-testid="scan-start"
          @click="start"
        >
          {{ running ? '扫描中…' : '开始扫描' }}
        </button>
      </div>
    </div>

    <p class="hint">
      扫描媒体库目录，把已放进挂载目录的照片/视频入库（哈希去重，可反复扫、不重复占用）。
      目录相对媒体根，留空 = 扫描整个媒体根。导入的媒体归<strong>当前登录账号</strong>所有。
      进度与终态实时轮询下方任务行。
    </p>

    <div class="scan-form">
      <label class="scan-label" for="scan-dir">子目录（相对媒体根，可留空）</label>
      <div class="scan-input-row">
        <input
          id="scan-dir"
          v-model="dir"
          class="scan-input"
          type="text"
          placeholder="如 photos/trip；留空 = 整个媒体根"
          :disabled="running"
          data-testid="scan-dir"
          @keyup.enter="start"
        />
        <button
          class="btn"
          type="button"
          :disabled="running"
          data-testid="scan-browse"
          @click="showTree = true"
        >
          浏览…
        </button>
      </div>
      <p class="hint hint--compact">可手动输入相对路径，或点「浏览…」在目录树中选择；选中后自动开始扫描。</p>
    </div>

    <DirectoryTreeDialog
      v-if="showTree"
      @pick="onPicked"
      @close="showTree = false"
    />

    <p v-if="err" class="msg msg--error" data-testid="scan-error">{{ err }}</p>

    <div v-if="job" class="scan-progress" data-testid="scan-progress">
      <div class="scan-bar">
        <div class="scan-bar-fill" :class="{ 'scan-bar-fill--done': done, 'scan-bar-fill--failed': failed }" :style="{ width: pct + '%' }" />
      </div>
      <p class="scan-status" data-testid="scan-status">
        <template v-if="job.status === 'running'">
          扫描中 {{ job.processed || 0 }} / {{ job.total || '…' }}
        </template>
        <template v-else-if="job.status === 'done'">
          扫描完成：共处理 {{ job.total }} 个媒体文件（新导入的已入时间轴/相册，重复的按哈希跳过）
        </template>
        <template v-else-if="job.status === 'failed'">
          扫描失败，请查看服务端日志或稍后重试
        </template>
      </p>
    </div>
  </section>
</template>

<script setup>
// Job000113 / R1-b 管理端扫描导入面板：目录输入 → 触发 → 轮询进度 → 结果反馈。
// Job000117：目录树选择器（浏览…按钮）——选中目录回填输入框并自动触发扫描；
// 手动输入路径保留可用，两种方式互不冲突（输入框始终可编辑，浏览只是另一种填法）。
// 进度/终态唯一真源 = GET /admin/jobs/:id（admin:system），与后端 index_jobs 行一一对应；
// index_jobs 只有 total/processed/status 三列，故"新导入 vs 重复"的明细不展示（后续增强）。
// 竞态治理：seq 守卫——轮询期间用户再次触发/组件卸载，旧的轮询立即作废（范式见 MediaViewer）。
import { computed, onUnmounted, ref } from 'vue'
import { errMessage } from '../../stores/auth'
import { scanImport, getJob } from '../../api/admin'
import DirectoryTreeDialog from './DirectoryTreeDialog.vue'

const dir = ref('')
const err = ref('')
const job = ref(null)
const running = ref(false)
const showTree = ref(false)
let pollTimer = null
let seq = 0

const done = computed(() => job.value?.status === 'done')
const failed = computed(() => job.value?.status === 'failed')
const pct = computed(() => {
  if (!job.value) return 0
  const t = job.value.total || 0
  if (!t) return 0
  const p = job.value.processed || 0
  return Math.min(100, Math.round((p / t) * 100))
})

function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

async function start() {
  if (running.value) return
  err.value = ''
  try {
    const accepted = await scanImport(dir.value.trim())
    running.value = true
    job.value = { id: accepted.job_id, status: 'running', total: 0, processed: 0 }
    poll(accepted.job_id)
  } catch (e) {
    err.value = errMessage(e, '发起扫描失败')
  }
}

// 目录树选中（Job000117）：回填相对路径并无缝触发扫描；扫描进行中则只回填不抢跑
// （409 语义交给用户稍后手动触发，避免对话框里隐含失败）。
function onPicked(rel) {
  dir.value = rel
  showTree.value = false
  if (!running.value) start()
}

// poll 轮询任务终态；seq 守卫保证只有最后一次触发的轮询存活。
function poll(jobID) {
  stopPolling()
  const mySeq = ++seq
  pollTimer = setInterval(async () => {
    try {
      const j = await getJob(jobID)
      if (mySeq !== seq) return // 已被更新的触发/卸载取代
      job.value = { id: j.id, status: j.status, total: j.total, processed: j.processed }
      if (j.status === 'done' || j.status === 'failed') {
        stopPolling()
        running.value = false
      }
    } catch {
      // 轮询失败不致命：下一轮再试；连续失败由运维面任务表兜底
    }
  }, 1500)
}

onUnmounted(() => {
  seq++ // 作废进行中的轮询
  stopPolling()
})
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
.head-actions {
  display: flex;
  gap: 8px;
}
.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin: 0 0 14px;
}
.hint strong {
  color: var(--color-text-primary);
}

.scan-form {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 14px;
}
.scan-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.scan-input-row {
  display: flex;
  gap: 8px;
}
.scan-input {
  flex: 1;
  min-width: 0;
  padding: 8px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  font-family: monospace;
}
.scan-input:focus {
  outline: none;
  border-color: var(--color-primary);
}
.scan-input:disabled {
  opacity: 0.6;
}
.hint--compact {
  margin: 0;
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

.scan-progress {
  margin-top: 4px;
}
.scan-bar {
  height: 8px;
  border-radius: var(--radius-sm);
  background: var(--color-surface-hover);
  overflow: hidden;
}
.scan-bar-fill {
  height: 100%;
  background: var(--color-primary);
  transition: width 0.4s ease;
}
.scan-bar-fill--done {
  background: var(--color-success);
}
.scan-bar-fill--failed {
  background: var(--color-danger);
}
.scan-status {
  margin: 8px 0 0;
  font-size: var(--font-size-sm);
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
</style>
