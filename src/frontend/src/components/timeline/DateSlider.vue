<template>
  <div
    v-if="buckets.length"
    ref="trackRef"
    class="date-slider"
    :class="{ busy, dragging }"
    title="拖动跳转到指定月份"
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
import { computed, ref } from 'vue'

const props = defineProps({
  // GET /media/date-histogram?granularity=month → [{ bucket:'YYYY-MM', count }]
  buckets: { type: Array, default: () => [] },
  // 滚动位置同步进来的当前进度（0=最新，1=最旧）
  fraction: { type: Number, default: 0 },
  // 正在加载/跳转中
  busy: { type: Boolean, default: false }
})
const emit = defineEmits(['seek'])

const trackRef = ref(null)
const dragging = ref(false)
const dragFraction = ref(0)

/* 时间范围：最新月次月 1 日 ~ 最旧月 1 日 */
// taken_at 为空的媒体后端归入 'unknown' 桶，无法定位时间轴，刻度中剔除
const sorted = computed(() =>
  [...props.buckets]
    .filter((b) => b && /^\d{4}-\d{2}$/.test(b.bucket))
    .sort((a, b) => (a.bucket < b.bucket ? 1 : -1))
)

const newestTs = computed(() => {
  if (!sorted.value.length) return 0
  const [y, m] = sorted.value[0].bucket.split('-').map(Number)
  return new Date(y, m, 1).getTime() // 次月 1 日
})

const oldestTs = computed(() => {
  if (!sorted.value.length) return 0
  const [y, m] = sorted.value[sorted.value.length - 1].bucket.split('-').map(Number)
  return new Date(y, m - 1, 1).getTime()
})

const range = computed(() => Math.max(1, newestTs.value - oldestTs.value))

/* 密度刻度：按月份分布定位，疏密用宽度+透明度表达 */
const ticks = computed(() => {
  const max = Math.max(1, ...sorted.value.map((b) => b.count || 0))
  return sorted.value.map((b) => {
    const [y, m] = b.bucket.split('-').map(Number)
    const ts = new Date(y, m - 1, 15).getTime() // 月中定位
    const ratio = (b.count || 0) / max
    const top = Math.min(100, Math.max(0, ((newestTs.value - ts) / range.value) * 100))
    return {
      key: b.bucket,
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

function monthKeyAt(f) {
  const ts = newestTs.value - f * range.value
  const d = new Date(ts)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`
}

const dragLabel = computed(() => {
  const key = monthKeyAt(dragFraction.value)
  const [y, m] = key.split('-')
  return `${y} 年 ${Number(m)} 月`
})

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
  trackRef.value?.setPointerCapture?.(e.pointerId)
}

function onMove(e) {
  if (!dragging.value) return
  dragFraction.value = fractionFromEvent(e)
}

function onUp() {
  if (!dragging.value) return
  dragging.value = false
  emit('seek', monthKeyAt(dragFraction.value))
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
