<template>
  <section class="scan-panel" data-testid="member-scan-panel">
    <!-- 未分配扫描根：fail-closed 引导（后端三个成员端点同样 403，这里只是前置提示） -->
    <div v-if="rootState === 'unassigned'" class="scan-guide" data-testid="member-scan-unassigned">
      <h3 class="guide-title">扫描导入</h3>
      <p class="guide-text">
        扫描导入需要管理员先为你分配扫描根目录（媒体库下的一个真实物理子目录）。
        当前你的账号尚未分配，请联系管理员在「管理后台 → 用户」中设置后再回来。
      </p>
    </div>

    <p v-else-if="rootState === 'error'" class="msg msg--error" data-testid="member-scan-root-error">
      {{ rootErr }}
    </p>

    <template v-else>
      <div class="scan-form">
        <label class="scan-label" for="member-scan-dir">子目录（相对你的扫描根，可留空）</label>
        <div class="scan-input-row">
          <input
            id="member-scan-dir"
            v-model="dir"
            class="scan-input"
            type="text"
            placeholder="留空 = 扫描整个扫描根"
            :disabled="running"
            data-testid="member-scan-dir"
            @keyup.enter="start"
          />
          <button
            class="btn"
            type="button"
            :disabled="running"
            data-testid="member-scan-browse"
            @click="showTree = true"
          >
            浏览…
          </button>
        </div>
        <p class="scan-root-label" data-testid="member-scan-root">
          你的扫描根目录：<strong>{{ rootLabel }}</strong>
        </p>
        <p class="hint hint--compact">
          扫描把已放进该目录的照片/视频导入你的媒体库（哈希去重，可反复扫）。
          你只能访问扫描根以内的目录，系统保留区不可见。
        </p>
      </div>

      <div class="scan-actions">
        <button
          class="btn primary"
          type="button"
          :disabled="running"
          data-testid="member-scan-start"
          @click="start"
        >
          {{ running ? '扫描中…' : '开始扫描' }}
        </button>
      </div>

      <DirectoryTreeDialog
        v-if="showTree"
        :loader="listMyDirTree"
        @pick="onPicked"
        @close="showTree = false"
      />

      <p v-if="err" class="msg msg--error" data-testid="member-scan-error">{{ err }}</p>

      <div v-if="job" class="scan-progress" data-testid="member-scan-progress">
        <div class="scan-bar">
          <div
            class="scan-bar-fill"
            :class="{ 'scan-bar-fill--done': done, 'scan-bar-fill--failed': failed }"
            :style="{ width: pct + '%' }"
          />
        </div>
        <p class="scan-status" data-testid="member-scan-status">
          <template v-if="job.status === 'running'">扫描中 {{ job.processed || 0 }} / {{ job.total || '…' }}</template>
          <template v-else-if="job.status === 'done'">扫描完成：共处理 {{ job.total }} 个媒体文件</template>
          <template v-else-if="job.status === 'failed'">扫描失败，请稍后重试或联系管理员</template>
        </p>
      </div>
    </template>
  </section>
</template>

<script setup>
// 成员扫描导入面板（Job000123）：管理员分配 scan_root 后，普通账号在此自助导入。
// 与管理端 StorageScanPanel 同范式（seq 守卫轮询/目录树/进度条），差别：数据源=
// 成员端 /scan /fs/tree /jobs/:id（边界=扫描根，越界服务端 400）；挂载前先查
// GET /user/scan-root，未分配直接引导；轮询期间分配被收回 → 403 → 停轮询回引导态。
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { errCode, errMessage } from '../../stores/auth'
import { getMyScanRoot, listMyDirTree, scanMine, getMyJob } from '../../api/scan'
import DirectoryTreeDialog from '../admin/DirectoryTreeDialog.vue'

const rootState = ref('loading') // loading | ready | unassigned | error
const rootErr = ref('')
const rootDisplay = ref('') // 后端 scan_root 原值：''=媒体根，其余=相对路径
const dir = ref('')
const err = ref('')
const job = ref(null)
const running = ref(false)
const showTree = ref(false)
let pollTimer = null
let seq = 0

const rootLabel = computed(() => (rootDisplay.value === '' ? '（整个媒体根）' : rootDisplay.value))
const done = computed(() => job.value?.status === 'done')
const failed = computed(() => job.value?.status === 'failed')
const pct = computed(() => {
  if (!job.value) return 0
  const t = job.value.total || 0
  if (!t) return 0
  return Math.min(100, Math.round(((job.value.processed || 0) / t) * 100))
})

async function loadRoot() {
  rootState.value = 'loading'
  rootErr.value = ''
  try {
    const r = await getMyScanRoot()
    if (!r.assigned) {
      rootState.value = 'unassigned'
      return
    }
    rootDisplay.value = r.scan_root || ''
    rootState.value = 'ready'
  } catch (e) {
    rootState.value = 'error'
    rootErr.value = errMessage(e, '查询扫描根目录失败')
  }
}

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
    const accepted = await scanMine(dir.value.trim())
    running.value = true
    job.value = { id: accepted.job_id, status: 'running', total: 0, processed: 0 }
    poll(accepted.job_id)
  } catch (e) {
    if (e?.response?.status === 409) {
      // Job000133 防重入：后端进程内互斥 409——如实提示，避免误以为可重复触发
      err.value = '已有扫描任务进行中，请等待完成后再试（进度见管理后台任务页）'
    } else {
      err.value = errMessage(e, '发起扫描失败')
    }
  }
}

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
      const j = await getMyJob(jobID)
      if (mySeq !== seq) return
      job.value = { id: j.id, status: j.status, total: j.total, processed: j.processed }
      if (j.status === 'done' || j.status === 'failed') {
        stopPolling()
        running.value = false
      }
    } catch (e) {
      if (mySeq !== seq) return
      if (errCode(e) === 'SCAN_ROOT_REQUIRED') {
        // 扫描进行中分配被管理员收回：停止轮询，回到未分配引导态。
        stopPolling()
        running.value = false
        job.value = null
        rootState.value = 'unassigned'
      }
      // 其它轮询失败不致命：下一轮再试。
    }
  }, 1500)
}

onMounted(loadRoot)
onUnmounted(() => {
  seq++
  stopPolling()
})
</script>

<style scoped>
.scan-panel { max-width: 720px; }
.guide-title { font-size: var(--font-size-md); color: var(--color-text-primary); margin-bottom: 8px; }
.guide-text { font-size: var(--font-size-md); line-height: 1.8; color: var(--color-text-secondary); }
.scan-form { display: flex; flex-direction: column; gap: 6px; margin-bottom: 14px; }
.scan-label { font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.scan-input-row { display: flex; gap: 8px; }
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
.scan-input:focus { outline: none; border-color: var(--color-primary); }
.scan-input:disabled { opacity: 0.6; }
.scan-root-label { margin: 4px 0 0; font-size: var(--font-size-sm); color: var(--color-text-secondary); font-family: monospace; }
.scan-root-label strong { color: var(--color-text-primary); }
.hint--compact { margin: 0; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.scan-actions { margin-bottom: 14px; }
.msg { margin: 0 0 8px; font-size: var(--font-size-md); }
.msg--error { color: var(--color-danger); }
.scan-progress { margin-top: 4px; }
.scan-bar { height: 8px; border-radius: var(--radius-sm); background: var(--color-surface-hover); overflow: hidden; }
.scan-bar-fill { height: 100%; background: var(--color-primary); transition: width 0.4s ease; }
.scan-bar-fill--done { background: var(--color-success); }
.scan-bar-fill--failed { background: var(--color-danger); }
.scan-status { margin: 8px 0 0; font-size: var(--font-size-sm); color: var(--color-text-secondary); }
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}
.btn:hover { background-color: var(--color-surface-hover); }
.btn:disabled { opacity: 0.6; cursor: default; }
.btn.primary { border-color: var(--color-primary); color: #fff; background-color: var(--color-primary); }
</style>
