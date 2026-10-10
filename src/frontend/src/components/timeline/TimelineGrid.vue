<template>
  <div ref="wrapRef" class="grid-wrap" :style="{ '--tl-header-h': headerHeight + 'px' }">
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
                  @thumb-action="(a, it) => $emit('thumb-action', a, it)"
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
          <img src="/brand/logo-disc-64.png" width="40" height="40" alt="" />
          <p>{{ emptyText }}</p>
        </div>

        <DateSlider
          class="date-slider"
          :buckets="pager.histogram.value"
          :dimension="dimension"
          :fraction="seek.scrollFraction.value"
          :busy="seek.seeking.value"
          @seek="(p) => seek.seekTo(p, scrollEl)"
          @drag-state="(v) => emit('drag-state', v)"
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
// Job000088 拆解（逻辑密集走 composable，DOM 结构薄不拆 presentation）：
// - useTimelineStream：pager 分页 / grouping 分组 / seek 锚点三层接线
// 宿主保留：布局列宽行高、空态文案、虚拟滚动 DOM 生命周期。
// Job000145：档位由宿主 TimelineView 经 prop 下传，本组件**不持有档位状态**。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { DynamicScroller, DynamicScrollerItem } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import gsap from 'gsap'
import ThumbItem from './ThumbItem.vue'
import DateSlider from './DateSlider.vue'
import BatchBar from '../media/BatchBar.vue'
import { useBatchOps } from '../media/useBatchOps'
import { useTimelineStream } from './useTimelineStream'

const props = defineProps({
  type: { type: String, default: '' }, // photo | video | 360 | ''
  favorites: { type: Boolean, default: false },
  place: { type: String, default: '' }, // Job000062：地点过滤（地点页点入）
  selectable: { type: Boolean, default: false }, // Job000079 时间轴批量操作
  dimension: { type: String, default: 'month' } // Job000145：分组维度，宿主 TimelineView 持有
})
const emit = defineEmits(['open', 'changed', 'thumb-action', 'drag-state'])

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

// Job000145：档位真源是 props，本组件只消费不持有；接线见 useTimelineStream 顶部的不变式说明
const { pager, grouping, seek, headerHeight, captureAnchor, restoreAnchor } = useTimelineStream(props, cols, rowHeight)

// DESIGN.md §8 grid-stagger（虚拟滚动安全版）：仅对**首屏首批**渲染的缩略图做一次瀑布进入，
// 后续滚动加载不做动画（DynamicScroller DOM 复用会让重复动画错位）；clearProps 防残留 transform。
//
// 🔴 位置约束（勿上移）：此 watch **必须** 排在上面 useTimelineStream 解构之后。
// Vue 的 watch(source, cb) 会**同步求值** source（首次收集依赖时立即执行一次 getter），
// 而 `pager` 是 const 声明 —— 若把本 watch 放到解构之前，`() => pager.items.length`
// 会在 const 的暂时性死区（TDZ）里被同步读取，抛
// `ReferenceError: Cannot access 'pano' before initialization`（线上实测，压缩后
// 变量名变成 `$`，一度误判为 page-agent 或循环依赖）。
// 对照：上面 allIds 同样是引用 pager 的箭头函数，但因为是**延迟调用**（只在用户点全选时
// 才求值）所以不报错 —— 这正是「同文件里一个抛一个不抛」的原因，别以为是随机现象。
let firstStaggerDone = false
watch(
  () => pager.items.length,
  async (n) => {
    if (n > 0 && !firstStaggerDone) {
      firstStaggerDone = true
      await nextTick()
      requestAnimationFrame(() => {
        const mm = gsap.matchMedia()
        mm.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.from('.grid-wrap .row .thumb', {
            y: 24,
            opacity: 0,
            duration: 0.55,
            ease: 'power2.out',
            stagger: 0.05,
            clearProps: 'transform,opacity'
          })
        })
      })
    }
  }
)

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
  pager.loadHistograms() // 三档一次取齐：切档零请求（AC-04）
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

// 宿主（TimelineView）用它做切档锚点与日历跳转：capture 抓视口顶部媒体，
// restore 按新档位重定位，seekTo 接收 {dimension,key}；dayBuckets 与滑块刻度同源（§2.4 规则 2）。
defineExpose({
  items: pager.items,
  removeById: pager.removeById,
  reload: pager.reset,
  loading: pager.loading,
  // 用 getter 而非直接取值：defineExpose 的对象在 setup 时求值一次，
  // 直接写 pager.histograms.day 会把**当时的**数组引用（初始空数组）固化下来，
  // 之后 histograms.day 被整体替换也不会反映到宿主 —— 直方图到不了日历，
  // 表现为「所有日期都点不动」。函数形式把求值推迟到调用时，
  // 且宿主放在 computed 里调用时能正确建立依赖。
  dayBuckets: () => pager.histograms.day,
  unknownCount: () => pager.unknownCount,
  captureAnchor: () => captureAnchor(scrollEl),
  restoreAnchor: (a) => restoreAnchor(scrollEl, a),
  seekTo: (payload) => seek.seekTo(payload, scrollEl)
})
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
  /* 高度来自 timelineDimensions.HEADER_HEIGHT（JS 经 --tl-header-h 下发，单一真源） */
  height: var(--tl-header-h, 46px);
  padding-bottom: 8px;
  font-size: var(--font-size-lg);
  font-weight: 600;
}

.tl-header.day {
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

/* 空状态品牌标记（Job000145 替换原40px 线框图标）：盘面版，透明底衬暖灰 */
.grid-empty img { object-fit: contain; opacity: 0.82; }

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
