<template>
  <div ref="wrapRef" class="grid-wrap">
    <div v-if="error" class="grid-error">
      <p>加载失败：{{ error }}</p>
      <button class="retry-btn" @click="reset">重试</button>
    </div>

    <template v-else>
      <div class="stream-area">
        <DynamicScroller
          v-if="flat.length"
          ref="scrollerRef"
          class="scroller"
          :items="flat"
          :min-item-size="34"
          key-field="__key"
        >
          <template #default="{ item, index, active }">
            <DynamicScrollerItem :item="item" :active="active" :index="index" :size-dependencies="[cols]">
              <div v-if="item.header" class="tl-header" :class="item.level">{{ item.label }}</div>
              <div
                v-else
                class="row"
                :style="{ gridTemplateColumns: `repeat(${cols}, 1fr)`, height: cellSize + 'px' }"
              >
                <ThumbItem v-for="m in item.cells" :key="m.id" :item="m" @open="$emit('open', $event)" />
              </div>
            </DynamicScrollerItem>
          </template>
          <template #after>
            <div ref="sentinelRef" class="sentinel">
              <span v-if="loading">加载中…</span>
              <span v-else-if="finished" class="muted">已加载全部 {{ items.length }} 条</span>
            </div>
          </template>
        </DynamicScroller>

        <div v-if="!loading && finished && !items.length" class="grid-empty">
          <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
            <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
            <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          </svg>
          <p>{{ emptyText }}</p>
        </div>

        <DateSlider
          class="date-slider"
          :buckets="histogram"
          :fraction="scrollFraction"
          :busy="seeking"
          @seek="seekTo"
        />
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import http from '../../api/http'
import ThumbItem from './ThumbItem.vue'
import DateSlider from './DateSlider.vue'

const props = defineProps({
  type: { type: String, default: '' }, // photo | video | 360 | ''
  favorites: { type: Boolean, default: false },
  place: { type: String, default: '' } // Job000062：地点过滤（地点页点入）
})
const emit = defineEmits(['open'])

const PAGE_SIZE = 60
const GAP = 8
const MIN_CELL = 140
const MONTH_H = 46 // 月标题高度（含下间距，与 CSS 一致）
const DAY_H = 34 // 日标题高度（含下间距，与 CSS 一致）

const items = reactive([])
const seen = new Set()
const loading = ref(false)
const finished = ref(false)
const error = ref('')
let nextCursor = null
let loadSeq = 0 // 代次守卫：reset() 递增，使在途的旧筛选响应落地时直接作废

const wrapRef = ref(null)
const scrollerRef = ref(null)
const sentinelRef = ref(null)
const wrapWidth = ref(0)

const histogram = ref([])
const scrollFraction = ref(0)
const seeking = ref(false)

const cols = computed(() => {
  if (!wrapWidth.value) return 5
  return Math.max(2, Math.floor((wrapWidth.value + GAP) / (MIN_CELL + GAP)))
})

const cellSize = computed(() => {
  if (!wrapWidth.value) return MIN_CELL
  return Math.floor((wrapWidth.value - GAP * (cols.value - 1)) / cols.value)
})

const rowHeight = computed(() => cellSize.value + GAP)

/* ---------- 连续时间流：内联 月/日 分组标题 ---------- */
function itemDate(m) {
  const t = m.taken_at || m.created_at
  if (!t) return null
  const d = new Date(t)
  return Number.isNaN(d.getTime()) ? null : d
}

function pad2(n) { return String(n).padStart(2, '0') }

const flat = computed(() => {
  const out = []
  let cells = []
  let lastMonth = ''
  let lastDay = ''

  const flushRow = () => {
    if (!cells.length) return
    const first = cells[0]
    const d = itemDate(first)
    out.push({
      __key: `row:${first.id}:${cols.value}:${out.length}`,
      header: false,
      cells,
      ts: d ? d.getTime() : 0
    })
    cells = []
  }

  for (const m of items) {
    const d = itemDate(m)
    const monthKey = d ? `${d.getFullYear()}-${pad2(d.getMonth() + 1)}` : 'unknown'
    const dayKey = d ? `${monthKey}-${pad2(d.getDate())}` : 'unknown'
    if (monthKey !== lastMonth) {
      flushRow()
      out.push({
        __key: `hm:${monthKey}`,
        header: true,
        level: 'month',
        monthKey,
        label: d ? `${d.getFullYear()} 年 ${d.getMonth() + 1} 月` : '未知日期',
        ts: d ? d.getTime() : 0
      })
      lastMonth = monthKey
      lastDay = ''
    }
    if (dayKey !== lastDay && d) {
      flushRow()
      out.push({
        __key: `hd:${dayKey}`,
        header: true,
        level: 'day',
        monthKey,
        label: `${d.getMonth() + 1} 月 ${d.getDate()} 日`,
        ts: d.getTime()
      })
      lastDay = dayKey
    }
    cells.push(m)
    if (cells.length === cols.value) flushRow()
  }
  flushRow()
  return out
})

/* 每个流元素的精确纵向偏移（行高与标题高度均为定值） */
const offsets = computed(() => {
  const arr = new Array(flat.value.length)
  let y = 0
  for (let i = 0; i < flat.value.length; i++) {
    arr[i] = y
    const it = flat.value[i]
    y += it.header ? (it.level === 'month' ? MONTH_H : DAY_H) : rowHeight.value
  }
  return arr
})

/* 月标题 → 流索引（用于滑块跳转） */
const monthIndex = computed(() => {
  const map = new Map()
  flat.value.forEach((it, i) => {
    if (it.header && it.level === 'month') map.set(it.monthKey, i)
  })
  return map
})

const emptyText = computed(() => {
  if (props.favorites) return '暂无收藏的媒体'
  if (props.type === 'video') return '暂无视频'
  if (props.type === '360') return '暂无 360 媒体'
  if (props.type === 'photo') return '暂无照片'
  return '暂无媒体，导入照片与视频后将在此按时间排列'
})

/* ---------- 分页加载 ---------- */
async function loadMore() {
  if (loading.value || finished.value) return
  const seq = loadSeq
  loading.value = true
  error.value = ''
  try {
    const params = { view: 'all', limit: PAGE_SIZE }
    if (props.type) params.type = props.type
    if (props.favorites) params.favorites = 'true'
    if (props.place) params.place = props.place
    if (nextCursor) params.cursor = nextCursor
    const { data } = await http.get('/media', { params })
    if (seq !== loadSeq) return // reset() 已递增代次：旧筛选的响应丢弃
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
    if (seq !== loadSeq) return
    error.value = e.response
      ? `HTTP ${e.response.status} ${e.response.data?.error?.message || ''}`
      : '网络不可达'
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

async function loadHistogram() {
  try {
    const { data } = await http.get('/media/date-histogram', { params: { granularity: 'month' } })
    histogram.value = Array.isArray(data) ? data : (Array.isArray(data?.buckets) ? data.buckets : [])
  } catch {
    histogram.value = [] // 接口未就绪或失败：隐藏滑块，不影响主时间流
  }
}

function reset() {
  loadSeq++ // 作废在途响应；同时放行 loading 守卫，让新筛选的首页请求立刻可发
  loading.value = false
  items.splice(0, items.length)
  seen.clear()
  nextCursor = null
  finished.value = false
  error.value = ''
  scrollFraction.value = 0
  loadMore()
  loadHistogram()
}

function removeById(id) {
  const i = items.findIndex((m) => m.id === id)
  if (i >= 0) {
    items.splice(i, 1)
    seen.delete(id)
  }
}

/* ---------- 滚动 → 滑块 同步 ---------- */
let scrollEl = null
let rafId = 0

function onScroll() {
  if (rafId) return
  rafId = requestAnimationFrame(() => {
    rafId = 0
    if (!scrollEl) return
    const top = scrollEl.scrollTop + 4
    const offs = offsets.value
    if (!offs.length) return
    // 二分：最后一个 offset <= top 的元素
    let lo = 0, hi = offs.length - 1, idx = 0
    while (lo <= hi) {
      const mid = (lo + hi) >> 1
      if (offs[mid] <= top) { idx = mid; lo = mid + 1 } else hi = mid - 1
    }
    const it = flat.value[idx]
    if (!it || !it.ts || !histogram.value.length) return
    const keys = histogram.value
      .map((b) => b.bucket)
      .filter((k) => /^\d{4}-\d{2}$/.test(k))
      .sort()
    if (!keys.length) return
    const [oy, om] = keys[0].split('-').map(Number)
    const [ny, nm] = keys[keys.length - 1].split('-').map(Number)
    const oldest = new Date(oy, om - 1, 1).getTime()
    const newest = new Date(ny, nm, 1).getTime()
    const range = Math.max(1, newest - oldest)
    scrollFraction.value = Math.min(1, Math.max(0, (newest - it.ts) / range))
  })
}

/* ---------- 滑块 → 滚动 跳转 ---------- */
function lastLoadedMonth() {
  for (let i = items.length - 1; i >= 0; i--) {
    const d = itemDate(items[i])
    if (d) return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}`
  }
  return ''
}

async function seekTo(monthKey) {
  if (seeking.value) return
  seeking.value = true
  try {
    let guard = 0
    while (!finished.value && guard < 300) {
      const last = lastLoadedMonth()
      // 媒体按时间倒序：last <= monthKey 说明目标月已在加载范围内；unknown = 无媒体可定位
      if (last === 'unknown' || (last && last <= monthKey)) break
      await loadMore()
      guard++
    }
    let idx = monthIndex.value.get(monthKey)
    if (idx == null) {
      // 当月无媒体：找不晚于该月的最近一个月份分组
      for (let i = 0; i < flat.value.length; i++) {
        const it = flat.value[i]
        if (it.header && it.level === 'month' && it.monthKey !== 'unknown' && it.monthKey <= monthKey) {
          idx = i
          break
        }
      }
    }
    if (idx != null && scrollEl) {
      scrollEl.scrollTo({ top: offsets.value[idx], behavior: 'smooth' })
    }
  } finally {
    seeking.value = false
  }
}

/* ---------- 生命周期 ---------- */
let resizeObserver = null
let intersectionObserver = null

onMounted(() => {
  if (wrapRef.value) {
    wrapWidth.value = wrapRef.value.clientWidth - 26 // 预留右侧滑块宽度
    resizeObserver = new ResizeObserver((entries) => {
      wrapWidth.value = entries[0].contentRect.width - 26
    })
    resizeObserver.observe(wrapRef.value)
  }
  // DynamicScroller 根元素即滚动容器（.vue-recycle-scroller）
  scrollEl = wrapRef.value?.querySelector('.vue-recycle-scroller') || null
  if (scrollEl) scrollEl.addEventListener('scroll', onScroll, { passive: true })
  intersectionObserver = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting) loadMore()
    },
    { root: scrollEl, rootMargin: '240px' }
  )
  if (sentinelRef.value) intersectionObserver.observe(sentinelRef.value)
  loadMore()
  loadHistogram()
})

// scroller 与 sentinel 随首屏渲染挂载后补挂
watch(scrollerRef, (comp) => {
  const el = comp?.$el || wrapRef.value?.querySelector('.vue-recycle-scroller')
  if (el && el !== scrollEl) {
    if (scrollEl) scrollEl.removeEventListener('scroll', onScroll)
    scrollEl = el
    scrollEl.addEventListener('scroll', onScroll, { passive: true })
    intersectionObserver?.disconnect()
    intersectionObserver = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting) loadMore()
      },
      { root: scrollEl, rootMargin: '240px' }
    )
    if (sentinelRef.value) intersectionObserver.observe(sentinelRef.value)
  }
})

watch(sentinelRef, (el, prev) => {
  if (prev) intersectionObserver?.unobserve(prev)
  if (el) intersectionObserver?.observe(el)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
  if (scrollEl) scrollEl.removeEventListener('scroll', onScroll)
  if (rafId) cancelAnimationFrame(rafId)
})

watch(() => [props.type, props.favorites, props.place], reset)

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

.stream-area {
  flex: 1;
  min-height: 0;
  display: flex;
  gap: 4px;
}

.scroller {
  flex: 1;
  min-width: 0;
}

.tl-header {
  display: flex;
  align-items: flex-end;
  color: var(--color-text-primary);
  overflow: hidden;
}

.tl-header.month {
  height: 46px;
  padding-bottom: 8px;
  font-size: var(--font-size-lg);
  font-weight: 600;
}

.tl-header.day {
  height: 34px;
  padding-bottom: 6px;
  font-size: var(--font-size-sm);
  font-weight: 500;
  color: var(--color-text-secondary);
}

.row {
  display: grid;
  gap: 8px;
  margin-bottom: 8px;
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
