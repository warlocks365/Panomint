<template>
  <section class="tb-panel">
    <!-- 两条限制必须照实写出来：这个列表"看起来完整而其实不然"，不交代就等于欺骗 -->
    <p class="tb-note" data-testid="toolbox-restored-notice">
      只显示<strong>由你本人执行</strong>的恢复（管理员代你恢复的记录归管理员，这里看不到）；
      且只显示<strong>路径</strong> —— 审计里没有文件名，媒体若之后被彻底删除，这条路径也不再指向任何东西，
      因此这里不提供「打开」入口。
    </p>

    <p v-if="restoredLoading" class="tb-tip">加载中…</p>

    <div v-else-if="restoredError" class="tb-tip error" data-testid="toolbox-restored-error">
      {{ restoredError }}
      <button class="retry-btn" @click="loadRestored">重试</button>
    </div>

    <div v-else-if="!restoredItems.length" class="tb-empty" data-testid="toolbox-restored-empty">
      <p class="empty-title">暂无恢复记录</p>
      <p class="empty-desc">你从回收站恢复过的媒体会按时间倒序列在这里。</p>
    </div>

    <template v-else>
      <p class="tb-summary">共 {{ restoredTotal }} 条恢复记录（按时间倒序）</p>
      <ul class="rs-list" data-testid="toolbox-restored-list">
        <li v-for="it in restoredItems" :key="it.id" class="rs-item" data-testid="toolbox-restored-item">
          <span class="rs-time">{{ fmtTime(it.at) }}</span>
          <span class="rs-path" :title="pathOf(it)">{{ pathOf(it) }}</span>
        </li>
      </ul>
      <div v-if="restoredCursor" class="more-row">
        <button class="btn" :disabled="restoredLoadingMore" @click="loadMoreRestored">
          {{ restoredLoadingMore ? '加载中…' : '加载更多' }}
        </button>
      </div>
    </template>
  </section>
</template>

<script setup>
// 「已恢复」面板（恢复历史，懒加载）——从 ToolboxView 抽出（Job000058-4）。
// 后端只按 actor 过滤，故这里天然只有本人执行的记录（见 api/media.js 注释）。
import { ref, watch } from 'vue'
import { getRestoreHistory } from '../../api/media'
import { dialogs } from '../../components/dialogs/dialogs'

const active = defineModel({ type: Boolean, default: false }) // 父级标签激活时才首载

const restoredItems = ref([])
const restoredTotal = ref(0)
const restoredCursor = ref('')
const restoredLoading = ref(false)
const restoredLoadingMore = ref(false)
const restoredError = ref('')
const restoredLoaded = ref(false)

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || fallback
}

// 恢复历史项的路径（detail 里只有 path，没有 filename）
function pathOf(it) {
  const p = it?.detail?.path
  return p ? String(p) : '（未记录路径）'
}

function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return String(t)
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function loadRestored() {
  restoredLoading.value = true
  restoredError.value = ''
  try {
    const data = await getRestoreHistory({ limit: 50 })
    restoredItems.value = Array.isArray(data?.items) ? data.items : []
    restoredTotal.value = data?.total ?? restoredItems.value.length
    restoredCursor.value = data?.next_cursor || ''
    restoredLoaded.value = true
  } catch (e) {
    restoredError.value = errMsg(e, '恢复历史加载失败')
  } finally {
    restoredLoading.value = false
  }
}

async function loadMoreRestored() {
  if (!restoredCursor.value || restoredLoadingMore.value) return
  restoredLoadingMore.value = true
  try {
    const data = await getRestoreHistory({ limit: 50, cursor: restoredCursor.value })
    const list = Array.isArray(data?.items) ? data.items : []
    restoredItems.value = restoredItems.value.concat(list)
    restoredTotal.value = data?.total ?? restoredTotal.value
    restoredCursor.value = data?.next_cursor || ''
  } catch (e) {
    // 不把已加载的列表换成错误页，故用弹窗提示
    await dialogs.alert(errMsg(e, '加载更多失败'))
  } finally {
    restoredLoadingMore.value = false
  }
}

// 懒加载：只有真的切到该标签才请求（工具箱首屏不该为没人看的标签付一次往返）
watch(active, (on) => {
  if (on && !restoredLoaded.value && !restoredLoading.value) loadRestored()
})
</script>

<style scoped>
.tb-note {
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-surface-hover);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  line-height: 1.7;
}

.tb-summary {
  margin: 14px 0 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tb-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.tb-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
}

.tb-empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.empty-desc {
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.rs-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.rs-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 9px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
}

.rs-time {
  flex-shrink: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-variant-numeric: tabular-nums;
}

.rs-path {
  min-width: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  word-break: break-all;
}

.more-row {
  display: flex;
  justify-content: center;
  padding: 14px 0;
}

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

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
