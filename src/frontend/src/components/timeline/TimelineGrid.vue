<template>
  <div ref="wrapRef" class="grid-wrap">
    <div v-if="pager.error.value" class="grid-error">
      <p>加载失败：{{ pager.error.value }}</p>
      <button class="retry-btn" @click="pager.reset">重试</button>
    </div>

    <template v-else>
      <div class="stream-area">
        <DynamicScroller
          v-if="grouping.flat.value.length"
          ref="scrollerRef"
          class="scroller"
          :items="grouping.flat.value"
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
                <ThumbItem
                  v-for="m in item.cells"
                  :key="m.id"
                  :item="m"
                  :selectable="selectable"
                  :selected="batch.selected.has(m.id)"
                  @toggle="batch.toggle($event.id)"
                  @open="$emit('open', $event)"
                />
              </div>
            </DynamicScrollerItem>
          </template>
          <template #after>
            <div ref="sentinelRef" class="sentinel">
              <span v-if="pager.loading.value">加载中…</span>
              <span v-else-if="pager.finished.value" class="muted">已加载全部 {{ pager.items.length }} 条</span>
            </div>
          </template>
        </DynamicScroller>

        <div v-if="!pager.loading.value && pager.finished.value && !pager.items.length" class="grid-empty">
          <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
            <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.5" />
            <path d="M3 16l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
          </svg>
          <p>{{ emptyText }}</p>
        </div>

        <DateSlider
          class="date-slider"
          :buckets="pager.histogram.value"
          :fraction="seek.scrollFraction.value"
          :busy="seek.seeking.value"
          @seek="(k) => seek.seekTo(k, scrollEl)"
        />
      </div>

      <BatchBar
        v-if="selectable && batch.count.value > 0"
        :count="batch.count.value"
        show-select-all
        :all-selected="batch.isAllSelected(allIds())"
        show-invert
        @toggle-all="batch.toggleAll(allIds())"
        @invert="batch.invertAll(allIds())"
        @meta="batch.onMeta"
        @tags="batch.onTags"
        @move="batch.onMove"
        @copy="batch.onCopy"
        @share="batch.onShare"
        @space="batch.onShareSpace"
        @delete="batch.onDelete"
        @clear="batch.clear"
      />
      <p v-if="selectable && batch.lastResult.value" class="batch-result" data-testid="batch-result">
        {{ batch.lastResult.value }}
      </p>
    </template>
  </div>
</template>

<script setup>
// Job000088 拆解（逻辑密集组件走 composable 路线，DOM 结构薄不拆 presentation）：
// - useTimelinePager：分页状态机（items 去重/cursor/三态/代次守卫/histogram）
// - useTimelineGrouping：月日分组（flat/offsets/monthIndex）
// - useTimelineSeek：日期滑块双向同步（rAF 节流二分 + seekTo 按需翻页跳转）
// 宿主保留：布局列宽/行高、空态文案、虚拟滚动 DOM 生命周期（RO/IO/scroller 换绑/清理）。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import ThumbItem from './ThumbItem.vue'
import DateSlider from './DateSlider.vue'
import BatchBar from '../media/BatchBar.vue'
import { useBatchOps } from '../media/useBatchOps'
import { useTimelinePager } from './useTimelinePager'
import { useTimelineGrouping } from './useTimelineGrouping'
import { useTimelineSeek } from './useTimelineSeek'

const props = defineProps({
  type: { type: String, default: '' }, // photo | video | 360 | ''
  favorites: { type: Boolean, default: false },
  place: { type: String, default: '' }, // Job000062：地点过滤（地点页点入）
  selectable: { type: Boolean, default: false } // Job000079 时间轴批量操作（勾选 + 吸底操作栏）
})
const emit = defineEmits(['open', 'changed'])

// 选中集按媒体 id 存于本组件（虚拟滚动只复用 DOM，选中态不丢）；操作完成后 emit changed 让宿主 reload
const batch = useBatchOps(() => emit('changed'))

// Job000099 全选/反选全集 = 已加载集（pager 游标累积的 items；加载未完成时=「当前已加载」语义）
const allIds = () => pager.items.map((m) => m.id)

const GAP = 8
const MIN_CELL = 140

const wrapRef = ref(null)
const scrollerRef = ref(null)
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

const pager = useTimelinePager(props)
const grouping = useTimelineGrouping(pager.items, cols, rowHeight)
const seek = useTimelineSeek({
  flat: grouping.flat,
  offsets: grouping.offsets,
  monthIndex: grouping.monthIndex,
  histogram: pager.histogram,
  items: pager.items,
  loadMore: pager.loadMore,
  finished: pager.finished
})

const emptyText = computed(() => {
  if (props.favorites) return '暂无收藏的媒体'
  if (props.type === 'video') return '暂无视频'
  if (props.type === '360') return '暂无 360 媒体'
  if (props.type === 'photo') return '暂无照片'
  return '暂无媒体，导入照片与视频后将在此按时间排列'
})

/* ---------- 虚拟滚动 DOM 生命周期（RO 列宽 / IO 翻页 / 滚动监听换绑） ---------- */
let resizeObserver = null
let intersectionObserver = null
let scrollEl = null

function bindScroll(el) {
  if (scrollEl) scrollEl.removeEventListener('scroll', onScrollHandler)
  scrollEl = el
  if (scrollEl) scrollEl.addEventListener('scroll', onScrollHandler, { passive: true })
}

function onScrollHandler() {
  seek.onScroll(scrollEl)
}

function makeSentinelIO() {
  intersectionObserver?.disconnect()
  intersectionObserver = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting) pager.loadMore()
    },
    { root: scrollEl, rootMargin: '240px' }
  )
  if (sentinelRef.value) intersectionObserver.observe(sentinelRef.value)
}

onMounted(() => {
  if (wrapRef.value) {
    wrapWidth.value = wrapRef.value.clientWidth - 26 // 预留右侧滑块宽度
    resizeObserver = new ResizeObserver((entries) => {
      wrapWidth.value = entries[0].contentRect.width - 26
    })
    resizeObserver.observe(wrapRef.value)
  }
  // DynamicScroller 根元素即滚动容器（.vue-recycle-scroller）
  bindScroll(wrapRef.value?.querySelector('.vue-recycle-scroller') || null)
  makeSentinelIO()
  pager.loadMore()
  pager.loadHistogram()
})

// scroller 与 sentinel 随首屏渲染挂载后补挂
watch(scrollerRef, (comp) => {
  const el = comp?.$el || wrapRef.value?.querySelector('.vue-recycle-scroller')
  if (el && el !== scrollEl) {
    bindScroll(el)
    makeSentinelIO()
  }
})

watch(sentinelRef, (el, prev) => {
  if (prev) intersectionObserver?.unobserve(prev)
  if (el) intersectionObserver?.observe(el)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
  if (scrollEl) scrollEl.removeEventListener('scroll', onScrollHandler)
  seek.destroy()
})

watch(() => [props.type, props.favorites, props.place], () => pager.reset())

defineExpose({ items: pager.items, removeById: pager.removeById, reload: pager.reset })
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

.batch-result {
  margin: 8px 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
</style>
