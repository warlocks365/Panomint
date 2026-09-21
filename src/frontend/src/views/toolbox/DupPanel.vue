<template>
  <section class="tb-panel">
    <p class="tb-note">
      分组依据是感知哈希（pHash），<strong>只是建议</strong>：视频只取首帧参与比对，
      同一场景的 360 照片/视频也会被判为重复。请逐组确认后再删除，
      <strong>本页不会自动删除任何内容</strong>。
    </p>

    <p v-if="notice" class="tb-notice" data-testid="toolbox-notice">{{ notice }}</p>

    <p v-if="loading" class="tb-tip">正在比对，媒体较多时需要一点时间…</p>

    <div v-else-if="error" class="tb-tip error" data-testid="toolbox-error">
      {{ error }}
      <button class="retry-btn" @click="load">重试</button>
    </div>

    <div v-else-if="!groups.length" class="tb-empty" data-testid="toolbox-empty">
      <p class="empty-title">未发现重复项</p>
      <p class="empty-desc">
        已比对 {{ scanned }} 项媒体的感知哈希（阈值 {{ threshold }}），没有达到重复判定的分组。
      </p>
    </div>

    <template v-else>
      <p class="tb-summary">
        已比对 {{ scanned }} 项，命中 {{ total }} 组疑似重复
        <span v-if="truncated">（只显示前 {{ groups.length }} 组）</span>
      </p>

      <div class="tb-groups" data-testid="toolbox-dup-list">
        <DupGroupCard
          v-for="(g, gi) in groups"
          :key="g.key"
          :group="g"
          :index="gi"
          :deleting="deleting"
          :keep-of="keepOf"
          @ask-delete="askDelete"
          @set-keep="setKeep"
        />
      </div>
    </template>

    <DupConfirmDialog
      v-if="confirmGroup"
      :group="confirmGroup"
      :keep-of="keepOf"
      :busy="deleting"
      :error="deleteError"
      @cancel="cancelDelete"
      @confirm="confirmDelete"
    />
  </section>
</template>

<script setup>
// 重复项目面板（pHash 分组 + 保留改选 + 删除其余）——从 ToolboxView 抽出（Job000058-4）。
// 删除是逐条软删非事务：已删的先摘出组，重试不会重复点已删条目（语义原样迁移）。
import { onMounted, ref } from 'vue'
import DupConfirmDialog from './DupConfirmDialog.vue'
import DupGroupCard from './DupGroupCard.vue'
import { deleteMedia, getDuplicates } from '../api/media'

const groups = ref([])
const loading = ref(false)
const error = ref('')
const scanned = ref(0)
const threshold = ref(10)
const total = ref(0)
const truncated = ref(false)
const notice = ref('')

const confirmGroup = ref(null)
const deleting = ref(false)
const deleteError = ref('')

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || fallback
}

async function load() {
  loading.value = true
  error.value = ''
  notice.value = ''
  try {
    const data = await getDuplicates()
    threshold.value = data?.threshold ?? 10
    scanned.value = data?.scanned ?? 0
    total.value = data?.total ?? 0
    truncated.value = !!data?.truncated
    // keepId 取出到本地状态：用户可改选，改选只影响本页的建议，不回写后端
    groups.value = (Array.isArray(data?.groups) ? data.groups : []).map((g) => ({
      key: `${g.keep_id}:${g.phash || ''}`,
      phash: g.phash || '',
      items: Array.isArray(g.items) ? g.items.slice() : [],
      keepId: g.keep_id || g.items?.[0]?.id || ''
    }))
  } catch (e) {
    // TOO_MANY_MEDIA 的 message 自带真实规模（后端刻意带回），原样透出比换成"失败"更有用
    error.value = errMsg(e, '重复项检测失败')
    groups.value = []
  } finally {
    loading.value = false
  }
}

function keepOf(g) {
  return g.items.find((it) => it.id === g.keepId) || g.items[0] || {}
}

function setKeep(g, it) {
  // keepId 是本地建议状态（不回写后端），改选只影响本页
  g.keepId = it.id
}

function itemMeta(it) {
  const parts = []
  if (it.width && it.height) parts.push(`${it.width}×${it.height}`)
  parts.push(it.type === 'video' ? '视频' : '照片')
  if (it.taken_at) parts.push(String(it.taken_at).slice(0, 10))
  return parts.join(' · ')
}

function askDelete(g) {
  deleteError.value = ''
  confirmGroup.value = g
}

function cancelDelete() {
  if (deleting.value) return
  confirmGroup.value = null
}

async function confirmDelete() {
  const g = confirmGroup.value
  if (!g || deleting.value) return
  deleting.value = true
  deleteError.value = ''
  const targets = g.items.filter((it) => it.id !== g.keepId)
  const done = []
  try {
    for (const it of targets) {
      await deleteMedia(it.id)
      done.push(it.id)
    }
    groups.value = groups.value.filter((x) => x !== g)
    confirmGroup.value = null
    notice.value = `已将 ${done.length} 项移入回收站，可在时间轴回收站中恢复。`
  } catch (e) {
    deleteError.value = errMsg(e, '删除失败，请稍后重试')
    if (done.length) {
      g.items = g.items.filter((it) => !done.includes(it.id))
      if (g.items.length < 2) {
        groups.value = groups.value.filter((x) => x !== g)
        confirmGroup.value = null
        notice.value = `已将 ${done.length} 项移入回收站，可在时间轴回收站中恢复。`
      }
    }
  } finally {
    deleting.value = false
  }
}

onMounted(load)
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

.tb-notice {
  margin-top: 12px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
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

.tb-groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.tb-guide {
  max-width: 620px;
}

.guide-title {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.guide-text {
  font-size: var(--font-size-md);
  line-height: 1.8;
  color: var(--color-text-secondary);
  margin-bottom: 14px;
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

</style>
