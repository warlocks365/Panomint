<template>
  <div
    v-if="buckets.length"
    ref="trackRef"
    class="date-slider"
    :class="{ busy, dragging }"
    :title="`拖动跳转到${unitName}`"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
  >
    <div class="track">
      <div
        v-for="t in ticks"
        :key="t.key"
        class="tick"
        :style="{ top: t.top + '%', width: t.w + 'px', opacity: t.o }"
      ></div>
    </div>
    <div class="handle" :style="{ top: handleTop + '%' }"></div>
    <div v-if="dragging" class="bubble" :style="{ top: handleTop + '%' }">{{ dragLabel }}</div>
  </div>
</template>

<script setup>
// 时间轴日期滑块（Job000088，Job000145 泛化为三档）。桶适配（正则过滤 / 代表时间 /
// 范围上下界 / 分数↔键换算 / 桶数上限降级）全部外移到 timelineDimensions.js——
// 留在本组件内会因维度分支把行数推过 300 组织红线。
//
// seek 载荷为 { dimension, key } 对象：裸字符串无法自证档位，useTimelineSeek 收到
// '2024-06' 时无法区分它来自月档的合法请求还是日档的误请求（ADR-001 记录的踩坑）。
import { computed, ref } from 'vue'
import { bucketRange, bucketTs, headerLabelOf, keyAtTs, tsAtFraction } from './timelineDimensions'
import { TICK_LIMIT, selectTicks } from './histogramBudget'

const props = defineProps({
  // GET /media/date-histogram?granularity=<dimension> → [{ bucket, count }]，升序（最旧在前）
  buckets: { type: Array, default: () => [] },
  // 当前档位，决定桶键形状（year/month/day）
  dimension: { type: String, default: 'month' },
  // 滚动位置同步进来的当前进度（0=最新，1=最旧）
  fraction: { type: Number, default: 0 },
  // 正在加载/跳转中
  busy: { type: Boolean, default: false }
})
const emit = defineEmits(['seek', 'drag-state'])

const trackRef = ref(null)
const dragging = ref(false)
const dragFraction = ref(0)

const UNIT = { year: '年份', month: '月份', day: '日期' }
const unitName = computed(() => UNIT[props.dimension] || UNIT.month)

// 滑块时间范围从**完整**桶数据推导（这是刚修掉的缺陷点：若上游按渲染预算截断过，
// 范围会整体错位，滚动同步与拖拽跳转都定位到错误位置）
const range = computed(() => bucketRange(props.dimension, props.buckets))

/**
 * 密度刻度：按桶代表时间定位，疏密用宽度 + 透明度表达。
 *
 * 呈现层裁剪在这里做（而非在 pager 存数据时截断）：pager 必须留完整数据，
 * 因为日历要用它给任意一天打圆点，而 bucketRange 也要用它推导滑块时间范围。
 * 桶数超 TICK_LIMIT 时 selectTicks 按**时间分箱聚合**（count 求和），
 * 既守住 DOM 节点上限，又不会像截断那样在时间轴上留下空白、扭曲密度形状。
 */
const ticks = computed(() => {
  if (!range.value.newestTs) return []
  const span = Math.max(1, range.value.newestTs - range.value.oldestTs)
  const shown = selectTicks(props.dimension, props.buckets, TICK_LIMIT)
  const max = Math.max(1, ...shown.map((b) => b.count || 0))
  return shown.map((b, i) => {
    const ratio = (b.count || 0) / max
    // 聚合箱用箱中点定位；未聚合的原始桶用自身代表时间
    const ts = b.ts || bucketTs(props.dimension, b.bucket)
    const top = Math.min(100, Math.max(0, ((range.value.newestTs - ts) / span) * 100))
    return {
      key: `${b.bucket}#${i}`,
      top,
      w: 5 + Math.round(9 * ratio),
      o: 0.4 + 0.6 * ratio
    }
  })
})

const handleTop = computed(() => {
  const f = dragging.value ? dragFraction.value : props.fraction
  return Math.min(100, Math.max(0, f * 100))
})

const dragLabel = computed(() => headerLabelOf(props.dimension, new Date(tsAtFraction(range.value, dragFraction.value))))

function fractionFromEvent(e) {
  const el = trackRef.value
  if (!el) return 0
  const rect = el.getBoundingClientRect()
  return Math.min(1, Math.max(0, (e.clientY - rect.top) / rect.height))
}

function onDown(e) {
  if (props.busy) return
  dragging.value = true
  dragFraction.value = fractionFromEvent(e)
  emit('drag-state', true)
  trackRef.value?.setPointerCapture?.(e.pointerId)
}

function onMove(e) {
  if (!dragging.value) return
  dragFraction.value = fractionFromEvent(e)
}

function onUp() {
  if (!dragging.value) return
  dragging.value = false
  emit('drag-state', false)
  emit('seek', { dimension: props.dimension, key: keyAtTs(props.dimension, tsAtFraction(range.value, dragFraction.value)) })
}
</script>

<style scoped>
.date-slider {
  position: relative;
  width: 26px;
  flex-shrink: 0;
  align-self: stretch;
  cursor: pointer;
  touch-action: none;
  user-select: none;
}

.track {
  position: absolute;
  top: 4px;
  bottom: 4px;
  left: 50%;
  width: 14px;
  transform: translateX(-50%);
  border-radius: 7px;
  background-color: var(--color-surface-hover);
  overflow: hidden;
}

.tick {
  position: absolute;
  right: 0;
  height: 3px;
  border-radius: 2px;
  background-color: var(--color-primary);
}

.handle {
  position: absolute;
  left: 50%;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background-color: var(--color-primary);
  border: 2px solid var(--color-surface);
  box-shadow: var(--shadow-card);
  transform: translate(-50%, -50%);
  pointer-events: none;
}

.dragging .handle {
  width: 15px;
  height: 15px;
}

.bubble {
  position: absolute;
  right: 30px;
  transform: translateY(-50%);
  white-space: nowrap;
  padding: 5px 10px;
  border-radius: var(--radius-sm);
  background-color: var(--color-text-primary);
  color: var(--color-surface);
  font-size: var(--font-size-sm);
  box-shadow: var(--shadow-card);
  pointer-events: none;
}

.busy {
  opacity: 0.55;
  cursor: wait;
}
</style>