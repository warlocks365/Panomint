<template>
  <div class="tags-page">
    <header class="page-toolbar">
      <h2 class="page-title">标签</h2>
      <input
        v-model.trim="filter"
        class="filter-input"
        type="text"
        placeholder="搜索标签"
        @keydown.enter="loadTags"
      />
      <div class="spacer"></div>
      <button class="btn" :disabled="aiBusy" @click="triggerAI">
        {{ aiBusy ? 'AI 打标中…' : 'AI 打标' }}
      </button>
      <button class="btn primary" @click="onCreate">新建标签</button>
    </header>

    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="loadTags">重试</button>
    </p>

    <template v-else>
      <div v-if="!tags.length" class="empty">
        <p class="empty-title">还没有标签</p>
        <p class="empty-desc">可手动新建，或点「AI 打标」让 CLIP 自动建议（需在媒体详情里确认）</p>
      </div>

      <div v-else class="cloud">
        <button
          v-for="t in tags"
          :key="t.id"
          class="tag-chip"
          :class="{ active: selected && selected.id === t.id, ai: t.kind === 'ai' && !t.confirmed }"
          :style="chipStyle(t)"
          :title="`${t.name} · ${t.usage_count} 项${t.kind === 'ai' && !t.confirmed ? ' · 待确认' : ''}`"
          @click="selectTag(t)"
        >
          {{ t.name }}
          <span class="chip-count">{{ t.usage_count }}</span>
          <i v-if="t.kind === 'ai' && !t.confirmed" class="chip-pending" title="AI 待确认">待</i>
        </button>
      </div>

      <section v-if="selected" class="detail">
        <header class="detail-head">
          <h3 class="detail-title">
            {{ selected.name }}
            <span class="detail-count">{{ mediaTotal }} 项</span>
          </h3>
          <div class="detail-actions">
            <button class="btn sm" @click="onRename(selected)">改名</button>
            <button class="btn sm" @click="onRecolor(selected)">改色</button>
            <button class="btn sm" @click="onMerge(selected)">合并…</button>
            <button class="btn sm danger" @click="onDelete(selected)">删除</button>
          </div>
        </header>

        <p v-if="mediaLoading" class="page-tip">媒体加载中…</p>
        <p v-else-if="mediaError" class="page-tip error">{{ mediaError }}</p>
        <p v-else-if="!items.length" class="page-tip">该标签下暂无已确认的媒体</p>
        <div v-else class="media-grid">
          <MediaThumb v-for="m in items" :key="m.id" :item="m" @click="openMedia(m)" />
        </div>
        <div v-if="nextCursor" class="more-row">
          <button class="btn" :disabled="mediaLoading" @click="loadMore">加载更多</button>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api/http'
import MediaThumb from '../components/albums/MediaThumb.vue'

const router = useRouter()

const tags = ref([])
const loading = ref(false)
const loadError = ref('')
const filter = ref('')

const selected = ref(null)
const items = ref([])
const mediaTotal = ref(0)
const nextCursor = ref('')
const mediaLoading = ref(false)
const mediaError = ref('')
const aiBusy = ref(false)

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || e?.message || fallback
}

async function loadTags() {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await http.get('/tags', { params: filter.value ? { q: filter.value } : {} })
    tags.value = Array.isArray(data) ? data : Array.isArray(data?.tags) ? data.tags : []
    if (selected.value) {
      const cur = tags.value.find((t) => t.id === selected.value.id)
      if (cur) selected.value = cur
    }
  } catch (e) {
    loadError.value = errMsg(e, '标签加载失败')
  } finally {
    loading.value = false
  }
}

async function selectTag(t) {
  selected.value = t
  items.value = []
  nextCursor.value = ''
  mediaTotal.value = 0
  await loadMedia(true)
}

async function loadMedia(reset) {
  if (!selected.value) return
  mediaLoading.value = true
  mediaError.value = ''
  try {
    const params = { limit: 60 }
    if (!reset && nextCursor.value) params.cursor = nextCursor.value
    const { data } = await http.get(`/tags/${selected.value.id}/media`, { params })
    const list = Array.isArray(data?.items) ? data.items : []
    items.value = reset ? list : items.value.concat(list)
    nextCursor.value = data?.next_cursor || ''
    mediaTotal.value = data?.total ?? items.value.length
  } catch (e) {
    mediaError.value = errMsg(e, '媒体加载失败')
  } finally {
    mediaLoading.value = false
  }
}

function loadMore() {
  loadMedia(false)
}

function openMedia(m) {
  router.push({ name: 'player', params: { id: m.id } })
}

function chipStyle(t) {
  if (t.color) {
    return { backgroundColor: t.color + '22', color: t.color, borderColor: t.color + '55' }
  }
  return null
}

async function onCreate() {
  const name = window.prompt('新标签名')
  if (!name || !name.trim()) return
  try {
    await http.post('/tags', { name: name.trim() })
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, '创建失败'))
  }
}

async function onRename(t) {
  const name = window.prompt('重命名标签', t.name)
  if (!name || !name.trim() || name.trim() === t.name) return
  try {
    await http.patch(`/tags/${t.id}`, { name: name.trim() })
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, '改名失败'))
  }
}

async function onRecolor(t) {
  const color = window.prompt('标签颜色（#RRGGBB，留空清除）', t.color || '#3b82f6')
  if (color === null) return
  try {
    await http.patch(`/tags/${t.id}`, { color: color.trim() })
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, '改色失败（需为 #RRGGBB）'))
  }
}

async function onMerge(t) {
  const target = window.prompt('合并到哪个标签（输入标签名）', '')
  if (!target || !target.trim()) return
  const dst = tags.value.find((x) => x.name === target.trim() && x.id !== t.id)
  if (!dst) {
    window.alert('未找到同名标签')
    return
  }
  if (!window.confirm(`将「${t.name}」的全部关联合并到「${dst.name}」并删除「${t.name}」？`)) return
  try {
    await http.delete(`/tags/${t.id}`, { params: { into: dst.id } })
    selected.value = null
    items.value = []
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, '合并失败'))
  }
}

async function onDelete(t) {
  if (!window.confirm(`删除标签「${t.name}」？其媒体关联会一并移除，媒体本身不受影响。`)) return
  try {
    await http.delete(`/tags/${t.id}`)
    selected.value = null
    items.value = []
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, '删除失败'))
  }
}

async function triggerAI() {
  if (aiBusy.value) return
  aiBusy.value = true
  try {
    const { data } = await http.post('/ai/tags', { scope: 'all' })
    window.alert(`AI 打标完成：处理 ${data?.processed ?? 0} 项，写入 ${data?.tagged ?? 0} 个标签（AI 标签需在媒体详情确认）`)
    await loadTags()
  } catch (e) {
    window.alert(errMsg(e, 'AI 打标失败（可能未启用 CLIP）'))
  } finally {
    aiBusy.value = false
  }
}

onMounted(loadTags)
</script>

<style scoped>
.tags-page {
  padding: 20px 24px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

.page-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.spacer {
  flex: 1;
}

.filter-input {
  height: 32px;
  width: 200px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 10px;
}

.filter-input:focus {
  outline: none;
  border-color: var(--color-primary);
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

.btn.sm {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
}

.empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--color-text-disabled);
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 22px;
}

.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 14px;
  border: 1px solid transparent;
  font-size: var(--font-size-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  cursor: pointer;
}

.tag-chip.ai {
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}

.tag-chip.active {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}

.chip-count {
  font-size: 11px;
  opacity: 0.75;
}

.chip-pending {
  font-style: normal;
  font-size: 10px;
  font-weight: 700;
  padding: 0 4px;
  border-radius: 4px;
  background-color: var(--color-warning-text);
  color: var(--color-warning-bg);
}

.detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.detail-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.detail-count {
  margin-left: 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: 400;
}

.detail-actions {
  margin-left: auto;
  display: flex;
  gap: 8px;
}

.media-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  gap: 10px;
}

.more-row {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}
</style>
