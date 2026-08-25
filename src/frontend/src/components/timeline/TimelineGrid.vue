<template>
  <div ref="wrapRef" class="grid-wrap">
    <div v-if="error" class="grid-error">
      <p>加载失败：{{ error }}</p>
      <button class="retry-btn" @click="reset">重试</button>
    </div>

    <template v-else>
      <RecycleScroller
        v-if="rows.length"
        class="scroller"
        :items="rows"
        :item-size="rowHeight"
        key-field="__key"
      >
        <template #default="{ item: row }">
          <div class="row" :style="{ gridTemplateColumns: `repeat(${cols}, 1fr)` }">
            <ThumbItem v-for="m in row.cells" :key="m.id" :item="m" @open="$emit('open', $event)" />
          </div>
        </template>
        <template #after>
          <div ref="sentinelRef" class="sentinel">
            <span v-if="loading">加载中…</span>
            <span v-else-if="finished" class="muted">已加载全部 {{ items.length }} 条</span>
          </div>
        </template>
      </RecycleScroller>

      <div v-if="!loading && finished && !items.length" class="grid-empty">
        <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
          <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
          <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        </svg>
        <p>{{ emptyText }}</p>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RecycleScroller } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import http from '../../api/http'
import ThumbItem from './ThumbItem.vue'

const props = defineProps({
  view: { type: String, required: true }, // day | all
  date: { type: String, default: '' },
  type: { type: String, default: '' }, // photo | video | 360 | ''
  favorites: { type: Boolean, default: false }
})
const emit = defineEmits(['open'])

const PAGE_SIZE = 60
const GAP = 8
const MIN_CELL = 140

const items = reactive([])
const seen = new Set()
const loading = ref(false)
const finished = ref(false)
const error = ref('')
let nextCursor = null

const wrapRef = ref(null)
const sentinelRef = ref(null)
const wrapWidth = ref(0)

const cols = computed(() => {
  if (!wrapWidth.value) return 5
  return Math.max(2, Math.floor((wrapWidth.value + GAP) / (MIN_CELL + GAP)))
})

const cellSize = computed(() => {
  if (!wrapWidth.value) return MIN_CELL
  return Math.floor((wrapWidth.value - GAP * (cols.value - 1)) / cols.value)
})

const rowHeight = computed(() => cellSize.value + GAP)

const rows = computed(() => {
  const out = []
  for (let i = 0; i < items.length; i += cols.value) {
    const cells = items.slice(i, i + cols.value)
    out.push({ __key: `${cells[0].id}:${cols.value}:${i}`, cells })
  }
  return out
})

const emptyText = computed(() => {
  if (props.favorites) return '暂无收藏的媒体'
  if (props.type === 'video') return '暂无视频'
  if (props.type === '360') return '暂无 360 媒体'
  if (props.type === 'photo') return '暂无照片'
  return '该时间范围内暂无媒体'
})

async function loadMore() {
  if (loading.value || finished.value) return
  loading.value = true
  error.value = ''
  try {
    const params = { view: props.view, limit: PAGE_SIZE }
    if (props.date) params.date = props.date
    if (props.type) params.type = props.type
    if (props.favorites) params.favorites = 'true'
    if (nextCursor) params.cursor = nextCursor
    const { data } = await http.get('/media', { params })
    const list = Array.isArray(data.items) ? data.items : []
    for (const m of list) {
      if (!seen.has(m.id)) {
        seen.add(m.id)
        items.push(m)
      }
    }
    nextCursor = data.next_cursor || null
    if (!nextCursor || !list.length) finished.value = true
  } catch (e) {
    error.value = e.response
      ? `HTTP ${e.response.status} ${e.response.data?.error?.message || ''}`
      : '网络不可达'
  } finally {
    loading.value = false
  }
}

function reset() {
  items.splice(0, items.length)
  seen.clear()
  nextCursor = null
  finished.value = false
  error.value = ''
  loadMore()
}

function removeById(id) {
  const i = items.findIndex((m) => m.id === id)
  if (i >= 0) {
    items.splice(i, 1)
    seen.delete(id)
  }
}

let resizeObserver = null
let intersectionObserver = null

onMounted(() => {
  if (wrapRef.value) {
    wrapWidth.value = wrapRef.value.clientWidth
    resizeObserver = new ResizeObserver((entries) => {
      wrapWidth.value = entries[0].contentRect.width
    })
    resizeObserver.observe(wrapRef.value)
  }
  intersectionObserver = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting) loadMore()
    },
    { root: wrapRef.value, rootMargin: '240px' }
  )
  if (sentinelRef.value) intersectionObserver.observe(sentinelRef.value)
  loadMore()
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
})

watch(() => [props.view, props.date, props.type, props.favorites], reset)

defineExpose({ items, removeById, reload: reset })
</script>

<style scoped>
.grid-wrap {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  position: relative;
  display: flex;
  flex-direction: column;
}

.scroller {
  flex: 1;
}

.row {
  display: grid;
  gap: 8px;
  padding-bottom: 8px;
}

.sentinel {
  padding: 16px 0 4px;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.grid-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--color-text-disabled);
  font-size: var(--font-size-md);
}

.grid-error {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--color-danger);
  font-size: var(--font-size-md);
}

.retry-btn {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 6px 16px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.retry-btn:hover {
  background-color: var(--color-surface-hover);
}

.muted {
  color: var(--color-text-disabled);
}
</style>
