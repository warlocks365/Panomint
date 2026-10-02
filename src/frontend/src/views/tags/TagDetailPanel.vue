<template>
  <section class="detail">
    <header class="detail-head">
      <h3 class="detail-title">
        {{ tag.name }}
        <span class="detail-count">{{ mediaTotal }} 项</span>
      </h3>
      <div class="detail-actions">
        <button class="btn sm" data-testid="tag-rename" @click="onRename(tag)">改名</button>
        <button class="btn sm" data-testid="tag-recolor" @click="onRecolor(tag)">改色</button>
        <button class="btn sm" data-testid="tag-merge" @click="onMerge(tag)">合并…</button>
        <button class="btn sm danger" data-testid="tag-delete" @click="onDelete(tag)">删除</button>
      </div>
    </header>

    <p v-if="mediaLoading" class="page-tip">媒体加载中…</p>
    <p v-else-if="mediaError" class="page-tip error">{{ mediaError }}</p>
    <p v-else-if="!items.length" class="page-tip">该标签下暂无已确认的媒体</p>
    <template v-else>
      <MediaTileGrid :items="items" selectable @open="openMedia" @changed="onMediaChanged" />
      <div v-if="nextCursor" class="more-row">
        <button class="btn" :disabled="mediaLoading" data-testid="tag-load-more" @click="loadMore">加载更多</button>
      </div>
    </template>
  </section>
</template>

<script setup>
// 选中标签的详情面板（媒体网格 + 改名/改色/合并/删除）——从 TagsView 抽出（Job000058-3）。
// Job000077：媒体网格换 MediaTileGrid selectable（批量操作七项同后端单点语义）；
// 原生 prompt/confirm/alert 全部换统一对话框宿主。
const DEFAULT_TAG_COLOR = '#3b82f6' // PIN_COLOR_PALETTE 同族豁免：改色弹窗的示例默认色（UI 文案数据）
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import MediaTileGrid from '../../components/media/MediaTileGrid.vue'
import { dialogs } from '../../components/dialogs/dialogs'

const props = defineProps({
  tag: { type: Object, required: true },
  tags: { type: Array, default: () => [] } // 合并查找目标用
})
const emit = defineEmits(['changed', 'cleared'])

const router = useRouter()

const items = ref([])
const mediaTotal = ref(0)
const nextCursor = ref('')
const mediaLoading = ref(false)
const mediaError = ref('')

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || e?.message || fallback
}

async function loadMedia(reset) {
  mediaLoading.value = true
  mediaError.value = ''
  try {
    const params = { limit: 60 }
    if (!reset && nextCursor.value) params.cursor = nextCursor.value
    const { data } = await http.get(`/tags/${props.tag.id}/media`, { params })
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

function onMediaChanged() {
  loadMedia(true)
}

// 切换标签即重载（父级 v-if 重建本组件，watch 兜同标签刷新场景）
watch(() => props.tag.id, () => loadMedia(true), { immediate: true })

async function onRename(t) {
  const name = await dialogs.prompt({
    title: '重命名标签',
    label: '标签名',
    initial: t.name,
    validate: (v) => (v.trim() ? '' : '名称不能为空')
  })
  if (name === null || name.trim() === t.name) return
  try {
    await http.patch(`/tags/${t.id}`, { name: name.trim() })
    emit('changed')
  } catch (e) {
    await dialogs.alert(errMsg(e, '改名失败'))
  }
}

async function onRecolor(t) {
  const color = await dialogs.prompt({
    title: '标签颜色',
    label: '#RRGGBB（留空清除）',
    initial: t.color || DEFAULT_TAG_COLOR
  })
  if (color === null) return
  try {
    await http.patch(`/tags/${t.id}`, { color: color.trim() })
    emit('changed')
  } catch (e) {
    await dialogs.alert(errMsg(e, '改色失败（需为 #RRGGBB）'))
  }
}

async function onMerge(t) {
  const target = await dialogs.prompt({
    title: '合并标签',
    label: '合并到哪个标签（输入标签名）',
    validate: (v) => (v.trim() ? '' : '请输入目标标签名')
  })
  if (target === null) return
  const dst = props.tags.find((x) => x.name === target.trim() && x.id !== t.id)
  if (!dst) {
    await dialogs.alert('未找到同名标签')
    return
  }
  const ok = await dialogs.confirm({
    title: '合并标签',
    text: `将「${t.name}」的全部关联合并到「${dst.name}」并删除「${t.name}」？`,
    confirmText: '合并',
    danger: true
  })
  if (!ok) return
  try {
    await http.delete(`/tags/${t.id}`, { params: { into: dst.id } })
    emit('cleared')
    emit('changed')
  } catch (e) {
    await dialogs.alert(errMsg(e, '合并失败'))
  }
}

async function onDelete(t) {
  const ok = await dialogs.confirm({
    title: '删除标签',
    text: `删除标签「${t.name}」？其媒体关联会一并移除，媒体本身不受影响。`,
    confirmText: '删除',
    danger: true
  })
  if (!ok) return
  try {
    await http.delete(`/tags/${t.id}`)
    emit('cleared')
    emit('changed')
  } catch (e) {
    await dialogs.alert(errMsg(e, '删除失败'))
  }
}
</script>

<style scoped>
.detail-head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.detail-title {
  font-size: var(--font-size-lg);
  letter-spacing: 0.04em;
  font-weight: 500;
  color: var(--color-text-primary);
}

.detail-count {
  margin-left: 8px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
  font-weight: 400;
}

.detail-actions {
  margin-left: auto;
  display: flex;
  gap: 8px;
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

.more-row {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}
</style>
