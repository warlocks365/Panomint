<template>
  <div class="map-timeline">
    <div class="tl-head">
      <span class="tl-title">时间轴</span>
      <span class="tl-range">{{ rangeLabel }}</span>
      <span v-if="loading" class="tl-loading">加载中…</span>
      <span class="tl-zoom">
        <button class="tl-zoom-btn" type="button" :disabled="granularity === 'year'" @click="zoomOut">−</button>
        <span class="tl-zoom-label">{{ granularityLabel }}</span>
        <button class="tl-zoom-btn" type="button" :disabled="granularity === 'day'" @click="zoomIn">＋</button>
      </span>
      <button v-if="hasRange" class="tl-clear" type="button" @click="clear">清除</button>
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
        :title="barTitle(b)"
      ></div>
    </div>
    <div v-else class="tl-empty">当前视野内没有带时间的照片</div>

    <div v-if="bars.length" class="tl-axis">
      <span>{{ bars[0].key }}</span>
      <span>{{ bars[bars.length - 1].key }}</span>
    </div>

    <div class="tl-stats">
      <div class="stat-cell">
        <span class="stat-dot" style="background:#3b82f6"></span>
        <span class="stat-name">照片</span>
        <span class="stat-num">{{ statTotals.photos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:#8b5cf6"></span>
        <span class="stat-name">视频</span>
        <span class="stat-num">{{ statTotals.videos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:#f59e0b"></span>
        <span class="stat-name">全景照片</span>
        <span class="stat-num">{{ statTotals.panoPhotos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:#ef4444"></span>
        <span class="stat-name">全景视频</span>
        <span class="stat-num">{{ statTotals.panoVideos }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'

// 时间轴（Job000009 优化）：年/月/日粒度缩放 + 拖拽框选 + 四类媒体实时统计
const props = defineProps({
  buckets: { type: Array, default: () => [] }, // GET /geo/histogram → [{ bucket, count, photos, videos, pano_photos, pano_videos }]
  range: { type: Object, default: null }, // { from: ISO, to: ISO } 或 null
  loading: { type: Boolean, default: false },
  granularity: { type: String, default: 'month' } // year|month|day（由父组件控制）
})
const emit = defineEmits(['change', 'zoom'])

const trackRef = ref(null)
const dragging = ref(false)
const sel = ref([-1, -1]) // 框选中的索引区间（未提交）

const granularityLabel = computed(() => ({ year: '年', month: '月', day: '日' })[props.granularity] || '月')

function zoomIn() {
  const order = ['year', 'month', 'day']
  const i = order.indexOf(props.granularity)
  if (i < order.length - 1) emit('zoom', order[i + 1])
}
function zoomOut() {
  const order = ['year', 'month', 'day']
  const i = order.indexOf(props.granularity)
  if (i > 0) emit('zoom', order[i - 1])
}

// bucket key 正则随粒度变化：year=YYYY，month=YYYY-MM，day=YYYY-MM-DD
const keyRe = computed(() => {
  if (props.granularity === 'year') return /^\d{4}$/
  if (props.granularity === 'day') return /^\d{4}-\d{2}-\d{2}$/
  return /^\d{4}-\d{2}$/
})

// 'unknown' 桶（taken_at 为空）无法定位时间轴，剔除
const bars = computed(() =>
  props.buckets
    .filter((b) => b && keyRe.value.test(b.bucket))
    .sort((a, b) => (a.bucket < b.bucket ? -1 : 1))
    .map((b) => ({
      key: b.bucket,
      count: b.count || 0,
      photos: b.photos || 0,
      videos: b.videos || 0,
      panoPhotos: b.pano_photos || 0,
      panoVideos: b.pano_videos || 0
    }))
)

const maxCount = computed(() => Math.max(1, ...bars.value.map((b) => b.count)))

function barHeight(count) {
  return Math.max(6, Math.round((count / maxCount.value) * 100))
}

function barTitle(b) {
  return `${b.key} · 照片${b.photos} 视频${b.videos} 全景照片${b.panoPhotos} 全景视频${b.panoVideos}`
}

// 由已提交的 range（ISO）反推高亮索引（按当前粒度截断 bucket 前缀）
const committedIdx = computed(() => {
  if (!props.range) return [-1, -1]
  const keys = bars.value.map((b) => b.key)
  const prefixLen = { year: 4, month: 7, day: 10 }[props.granularity] || 7
  const prefixOf = (iso) => (iso || '').slice(0, prefixLen)
  const a = keys.indexOf(prefixOf(props.range.from))
  const b = keys.indexOf(prefixOf(props.range.to))
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

// 四类统计：默认统计全视野；有框选时统计选中区间
const statTotals = computed(() => {
  let list = bars.value
  const [a, b] = active.value
  if (a >= 0 && b >= a) list = bars.value.slice(a, b + 1)
  return list.reduce(
    (acc, b) => {
      acc.photos += b.photos
      acc.videos += b.videos
      acc.panoPhotos += b.panoPhotos
      acc.panoVideos += b.panoVideos
      return acc
    },
    { photos: 0, videos: 0, panoPhotos: 0, panoVideos: 0 }
  )
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

// bucket → ISO 区间（闭区间：起始 00:00 ~ 末尾 23:59:59.999）
function toRange(fromKey, toKey) {
  const from = bucketStart(fromKey)
  const to = bucketEnd(toKey)
  return { from: from.toISOString(), to: to.toISOString() }
}

function bucketStart(key) {
  if (/^\d{4}$/.test(key)) return new Date(+key, 0, 1, 0, 0, 0, 0)
  if (/^\d{4}-\d{2}$/.test(key)) {
    const [y, m] = key.split('-').map(Number)
    return new Date(y, m - 1, 1, 0, 0, 0, 0)
  }
  const [y, m, d] = key.split('-').map(Number)
  return new Date(y, m - 1, d, 0, 0, 0, 0)
}

function bucketEnd(key) {
  if (/^\d{4}$/.test(key)) return new Date(+key + 1, 0, 1, 0, 0, 0, -1)
  if (/^\d{4}-\d{2}$/.test(key)) {
    const [y, m] = key.split('-').map(Number)
    return new Date(y, m, 1, 0, 0, 0, -1) // 次月 1 日 -1ms
  }
  const [y, m, d] = key.split('-').map(Number)
  return new Date(y, m - 1, d, 23, 59, 59, 999)
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

.tl-loading {
  color: #94a3b8;
}

.tl-zoom {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 6px;
}

.tl-zoom-btn {
  width: 22px;
  height: 22px;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  font-size: 14px;
  line-height: 1;
  color: #475569;
  cursor: pointer;
}

.tl-zoom-btn:disabled {
  opacity: 0.4;
  cursor: default;
}

.tl-zoom-label {
  min-width: 20px;
  text-align: center;
  font-weight: 600;
  color: #0f172a;
}

.tl-clear {
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
  transition: background 0.15s, height 0.2s ease;
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

.tl-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
  margin-top: 10px;
}

.stat-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  background: #f8fafc;
  border-radius: 6px;
  padding: 6px 10px;
}

.stat-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex-shrink: 0;
}

.stat-name {
  font-size: 11px;
  color: #64748b;
  white-space: nowrap;
}

.stat-num {
  margin-left: auto;
  font-size: 14px;
  font-weight: 600;
  color: #0f172a;
  font-variant-numeric: tabular-nums;
}
</style>
