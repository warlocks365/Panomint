<template>
  <div class="map-timeline">
    <div class="tl-head">
      <span class="tl-title">时间轴</span>
      <span class="tl-range">{{ rangeLabel }}</span>
      <button v-if="hasRange" class="tl-clear" type="button" @click="clear">清除</button>
      <span v-if="loading" class="tl-loading">加载中…</span>
    </div>

    <div
      v-if="bars.length"
      ref="trackRef"
      class="tl-track"
      @pointerdown="onDown"
      @pointermove="onMove"
      @pointerup="onUp"
      @pointercancel="onUp"
    >
      <div
        v-for="(b, i) in bars"
        :key="b.key"
        class="tl-bar"
        :class="{ on: inRange(i) }"
        :style="{ height: barHeight(b.count) + '%' }"
        :title="`${b.key} · ${b.count} 项`"
      ></div>
    </div>
    <div v-else class="tl-empty">当前视野内没有带时间的照片</div>

    <div v-if="bars.length" class="tl-axis">
      <span>{{ bars[0].key }}</span>
      <span>{{ bars[bars.length - 1].key }}</span>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'

// 时间轴：直方图 + 拖拽框选时间范围（地图 ↔ 时间轴双向联动的"时间轴"侧）
const props = defineProps({
  buckets: { type: Array, default: () => [] }, // GET /geo/histogram → [{ bucket:'YYYY-MM', count }]
  range: { type: Object, default: null }, // { from: ISO, to: ISO } 或 null
  loading: { type: Boolean, default: false }
})
const emit = defineEmits(['change'])

const trackRef = ref(null)
const dragging = ref(false)
const sel = ref([-1, -1]) // 框选中的索引区间（未提交）

// 'unknown' 桶（taken_at 为空）无法定位时间轴，剔除
const bars = computed(() =>
  props.buckets
    .filter((b) => b && /^\d{4}-\d{2}$/.test(b.bucket))
    .sort((a, b) => (a.bucket < b.bucket ? -1 : 1))
    .map((b) => ({ key: b.bucket, count: b.count || 0 }))
)

const maxCount = computed(() => Math.max(1, ...bars.value.map((b) => b.count)))

function barHeight(count) {
  return Math.max(6, Math.round((count / maxCount.value) * 100))
}

// 由已提交的 range（ISO）反推高亮索引
const committedIdx = computed(() => {
  if (!props.range) return [-1, -1]
  const keys = bars.value.map((b) => b.key)
  const monthOf = (iso) => (iso || '').slice(0, 7)
  const a = keys.indexOf(monthOf(props.range.from))
  const b = keys.indexOf(monthOf(props.range.to))
  if (a < 0 || b < 0) return [-1, -1]
  return [Math.min(a, b), Math.max(a, b)]
})

const active = computed(() => (dragging.value ? sel.value : committedIdx.value))
const hasRange = computed(() => active.value[0] >= 0)

function inRange(i) {
  const [a, b] = active.value
  return a >= 0 && i >= a && i <= b
}

const rangeLabel = computed(() => {
  const [a, b] = active.value
  if (a < 0) return '全部时间'
  return a === b ? bars.value[a].key : `${bars.value[a].key} ~ ${bars.value[b].key}`
})

function indexAt(clientX) {
  const track = trackRef.value
  if (!track) return 0
  const rect = track.getBoundingClientRect()
  const ratio = (clientX - rect.left) / Math.max(1, rect.width)
  const i = Math.floor(ratio * bars.value.length)
  return Math.min(bars.value.length - 1, Math.max(0, i))
}

function onDown(e) {
  if (!bars.value.length) return
  dragging.value = true
  const i = indexAt(e.clientX)
  sel.value = [i, i]
  trackRef.value.setPointerCapture?.(e.pointerId)
}

function onMove(e) {
  if (!dragging.value) return
  const i = indexAt(e.clientX)
  sel.value = [sel.value[0], i]
}

function onUp() {
  if (!dragging.value) return
  dragging.value = false
  const [a0, b0] = sel.value
  const a = Math.min(a0, b0)
  const b = Math.max(a0, b0)
  if (!bars.value[a] || !bars.value[b]) return
  emit('change', toRange(bars.value[a].key, bars.value[b].key))
}

function clear() {
  sel.value = [-1, -1]
  emit('change', null)
}

// 月份 → ISO 区间（闭月：月初 00:00 ~ 月末 23:59:59.999）
function toRange(fromKey, toKey) {
  const [fy, fm] = fromKey.split('-').map(Number)
  const [ty, tm] = toKey.split('-').map(Number)
  const from = new Date(fy, fm - 1, 1, 0, 0, 0, 0)
  const to = new Date(ty, tm, 1, 0, 0, 0, 0) // 次月 1 日 0 点
  to.setMilliseconds(-1)
  return { from: from.toISOString(), to: to.toISOString() }
}
</script>

<style scoped>
.map-timeline {
  background: rgba(255, 255, 255, 0.96);
  border-top: 1px solid rgba(15, 23, 42, 0.08);
  padding: 8px 14px 10px;
  user-select: none;
}

.tl-head {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: #475569;
}

.tl-title {
  font-weight: 600;
  color: #0f172a;
}

.tl-range {
  font-variant-numeric: tabular-nums;
}

.tl-clear {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: #475569;
  cursor: pointer;
}

.tl-clear:hover {
  border-color: rgba(15, 23, 42, 0.28);
  color: #0f172a;
}

.tl-loading {
  margin-left: auto;
  color: #94a3b8;
}

.tl-track {
  display: flex;
  align-items: flex-end;
  gap: 1px;
  height: 54px;
  margin-top: 6px;
  cursor: crosshair;
  background: linear-gradient(to top, rgba(148, 163, 184, 0.08), transparent);
  border-radius: 4px;
}

.tl-bar {
  flex: 1 1 0;
  min-width: 2px;
  background: #94a3b8;
  border-radius: 2px 2px 0 0;
  transition: background 0.15s;
}

.tl-bar.on {
  background: #2563eb;
}

.tl-empty,
.tl-axis {
  margin-top: 4px;
  font-size: 11px;
  color: #94a3b8;
}

.tl-axis {
  display: flex;
  justify-content: space-between;
}
</style>
