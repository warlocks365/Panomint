<template>
  <div class="map-timeline" :class="{ 'tl--collapsed': chrome.detailHidden.value, 'tl--top': position === 'top' }">
    <div class="tl-head">
      <span class="tl-title">时间轴</span>
      <span v-if="!chrome.detailHidden.value" class="tl-range">{{ sel.rangeLabel.value }}</span>
      <span v-if="loading && !chrome.detailHidden.value" class="tl-loading">加载中…</span>
      <span v-if="!chrome.detailHidden.value" class="tl-zoom">
        <button class="tl-zoom-btn" type="button" :disabled="granularity === 'year'" @click="chrome.zoomOut">−</button>
        <span class="tl-zoom-label">{{ chrome.granularityLabel.value }}</span>
        <button class="tl-zoom-btn" type="button" :disabled="granularity === 'day'" @click="chrome.zoomIn">＋</button>
      </span>
      <button v-if="sel.hasRange.value && !chrome.detailHidden.value" class="tl-clear" type="button" @click="sel.clear">清除</button>
      <!-- 移动端折叠开关：折叠后只剩本行（≤40px），把高度让给地图 -->
      <button
        v-if="chrome.isMobile.value"
        class="tl-toggle"
        type="button"
        :aria-expanded="String(!chrome.collapsed.value)"
        :title="chrome.collapsed.value ? '展开时间轴' : '收起时间轴'"
        @click="chrome.collapsed.value = !chrome.collapsed.value"
      >
        {{ chrome.collapsed.value ? '展开' : '收起' }}
        <svg viewBox="0 0 10 6" width="9" height="6" fill="none" :class="{ 'tl-chevron--up': chrome.collapsed.value }">
          <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <div v-if="!chrome.detailHidden.value" class="tl-body">
      <div
        v-if="sel.bars.value.length"
        ref="trackRef"
        class="tl-track"
        @pointerdown="sel.onDown"
        @pointermove="onTrackPointerMove"
        @pointerup="sel.onUp"
        @pointercancel="sel.onUp"
        @pointerleave="hoverIdx = -1"
      >
        <!-- V4 面积图主体（DESIGN.md §8）：平滑曲线 + 雾蓝渐变面积；pointer-events:none，拖拽命中仍由 bar 层承担 -->
        <svg class="tl-area" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
          <path class="tl-area-fill" :d="areaPath" />
          <path class="tl-area-line" :d="linePath" vector-effect="non-scaling-stroke" />
        </svg>
        <!-- 刷选命中层：视觉弱化为雾面，inRange/hover 高亮 -->
        <div
          v-for="(b, i) in sel.bars.value"
          :key="b.key"
          class="tl-bar"
          :class="{ on: sel.inRange(i), hv: hoverIdx === i }"
          :style="{ height: sel.barHeight(b.count) + '%' }"
        ></div>
        <!-- V4 悬停参考线 + V2 分解气泡（数据源：后端直方图恒返四类计数） -->
        <div
          v-if="hoverIdx >= 0 && hoverBar"
          class="tl-cursor"
          :style="{ left: ((hoverIdx + 0.5) / sel.bars.value.length) * 100 + '%' }"
        ></div>
        <div
          v-if="hoverIdx >= 0 && hoverBar"
          class="tl-tip"
          :class="{ 'tl-tip--flip': hoverIdx > sel.bars.value.length / 2 }"
          :style="{ left: ((hoverIdx + 0.5) / sel.bars.value.length) * 100 + '%' }"
        >
          <div class="tl-tip-key"><b>{{ hoverBar.key }}</b> · {{ hoverBar.count }} 项</div>
          <div class="tl-tip-r"><i class="d" style="background: var(--stat-photo)"></i>照片 {{ hoverBar.photos }}</div>
          <div class="tl-tip-r"><i class="d" style="background: var(--stat-video)"></i>视频 {{ hoverBar.videos }}</div>
          <div class="tl-tip-r"><i class="d" style="background: var(--stat-pano-photo)"></i>全景照 {{ hoverBar.panoPhotos }}</div>
          <div class="tl-tip-r"><i class="d" style="background: var(--stat-pano-video)"></i>全景视 {{ hoverBar.panoVideos }}</div>
        </div>
      </div>
      <div v-else class="tl-empty">当前视野内没有带时间的照片</div>

      <div v-if="sel.bars.value.length" class="tl-axis">
        <span>{{ sel.bars.value[0].key }}</span>
        <span>{{ sel.bars.value[sel.bars.value.length - 1].key }}</span>
      </div>

      <TimelineStatsPanel :totals="sel.statTotals.value" :places="places" @place="$emit('place', $event)" />
    </div>
  </div>
</template>

<script setup>
// Job000089 拆解（交互密集：拖拽框选状态机 + 范围反推 + 折叠/缩放，展示段拆面板）：
// - useMapTimelineChrome：移动端折叠 + 年/月/日粒度缩放
// - useMapTimelineSel：bars 过滤排序 / 拖拽框选状态机 / range 反推高亮 / 四类统计 / bucket↔ISO
// - TimelineStatsPanel：统计四格 + 位置 chips 纯展示
// 宿主留：头/轨道/轴/空态模板与轨道样式；trackRef 宿主自持（DOM 归属宿主），传入 sel 消费。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import TimelineStatsPanel from './TimelineStatsPanel.vue'
import { useMapTimelineChrome } from './useMapTimelineChrome'
import { useMapTimelineSel } from './useMapTimelineSel'

// 时间轴（Job000009 优化）：年/月/日粒度缩放 + 拖拽框选 + 四类媒体实时统计
const props = defineProps({
  buckets: { type: Array, default: () => [] }, // GET /geo/histogram → [{ bucket, count, photos, videos, pano_photos, pano_videos }]
  range: { type: Object, default: null }, // { from: ISO, to: ISO } 或 null
  loading: { type: Boolean, default: false },
  granularity: { type: String, default: 'month' }, // year|month|day（由父组件控制）
  places: { type: Array, default: () => [] }, // GET /geo/places → [{ name, count }]（底部位置罗列）
  position: { type: String, default: 'bottom' } // bottom|top（时间轴贴底/贴顶，由用户偏好控制）
})
const emit = defineEmits(['change', 'zoom', 'place'])

const chrome = useMapTimelineChrome(props, emit)
const trackRef = ref(null) // 轨道元素宿主自持（v-if 切换时 Vue 自动置 null，拖拽前守卫 bars 非空）
const sel = useMapTimelineSel(props, emit, trackRef)

/* ---- V4 面积图 + V2 分解气泡（DESIGN.md §8；数据源：后端直方图恒返四类计数） ---- */
import gsap from 'gsap'
const hoverIdx = ref(-1)
const hoverBar = computed(() => (hoverIdx.value >= 0 ? sel.bars.value[hoverIdx.value] : null))

function onTrackMove(e) {
  const bars = sel.bars.value
  if (!bars.length || !trackRef.value) return
  const r = trackRef.value.getBoundingClientRect()
  const i = Math.floor(((e.clientX - r.left) / r.width) * bars.length)
  hoverIdx.value = Math.min(bars.length - 1, Math.max(0, i))
}

// pointermove 同时承担两件事（需求：拖拽选范围 + 悬停看计数）：
//   1. onTrackMove —— 悬停参考线与四类计数气泡
//   2. sel.onMove  —— 拖拽框选状态机推进（把选区从 [按下桶] 扩成 [按下桶, 当前桶]）
// 🔴 为什么必须合到两个函数而不是挂两个 @pointermove：同一元素上重复绑定同类型
// 事件，后者会覆盖前者；而模板表达式又不支持分号多语句。故统一走这个合并入口。
// 历史缺陷（2026-10-10 实测定位）：模板原先只绑了 onTrackMove，sel.onMove 从未
// 被调用 → sel 永远停在按下时的 [i,i] → 无论怎么拖都只选中单个时刻，
// 「选择一段时间轴范围（起止区间）」这个原需求在实机上完全不可用，
// 而逻辑层单测全部通过（缺陷藏在线路而非逻辑，故补了源码级回归断言）。
function onTrackPointerMove(e) {
  onTrackMove(e)
  sel.onMove(e)
}

// 归一化点列（x=列中心 0-100，y=100-高度%）→ Catmull-Rom 平滑贝塞尔
const pts = computed(() =>
  sel.bars.value.map((b, i) => [
    ((i + 0.5) / sel.bars.value.length) * 100,
    100 - sel.barHeight(b.count)
  ])
)
const linePath = computed(() => {
  const p = pts.value
  if (p.length < 2) return ''
  let d = `M ${p[0][0]},${p[0][1]}`
  for (let i = 0; i < p.length - 1; i++) {
    const p0 = p[Math.max(0, i - 1)]
    const p3 = p[Math.min(p.length - 1, i + 2)]
    const c1x = p[i][0] + (p[i + 1][0] - p0[0]) / 6
    const c1y = p[i][1] + (p[i + 1][1] - p0[1]) / 6
    const c2x = p[i + 1][0] - (p3[0] - p[i][0]) / 6
    const c2y = p[i + 1][1] - (p3[1] - p[i][1]) / 6
    d += ` C ${c1x.toFixed(2)},${c1y.toFixed(2)} ${c2x.toFixed(2)},${c2y.toFixed(2)} ${p[i + 1][0].toFixed(2)},${p[i + 1][1].toFixed(2)}`
  }
  return d
})
const areaPath = computed(() => {
  const p = pts.value
  if (p.length < 2) return ''
  return `${linePath.value} L ${p[p.length - 1][0].toFixed(2)},100 L ${p[0][0].toFixed(2)},100 Z`
})

// GSAP dock-in（原生 Vue 模式：onMounted + gsap；@gsap/react 为 React 专用绑定不可用于 Vue）
let tlCtx = null
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    tlCtx = gsap.context(() => {
      gsap.from('.map-timeline', { y: 16, opacity: 0, duration: 0.55, ease: 'power2.out' })
    })
  })
})
onBeforeUnmount(() => {
  if (tlCtx) tlCtx.revert()
})
</script>

<style scoped>
.map-timeline {
  background: var(--color-surface, #f4f1ed);
  border-top: 1px solid var(--color-border, #ddd8d0);
  padding: 8px 14px 10px;
  user-select: none;
}

/* 贴顶时描边翻到下方，与画布的分界线始终朝向地图 */
.tl--top {
  border-top: none;
  border-bottom: 1px solid rgba(15, 23, 42, 0.08);
}

.tl-head {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: var(--color-text-secondary);
}

/* 移动端折叠开关 */
.tl-toggle {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: var(--color-surface);
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.tl-chevron--up {
  transform: rotate(180deg);
}

/* 折叠态：容器只剩标题行，总高 ≈ 5+20+5 = 30px（验收要求 ≤40px） */
.tl--collapsed {
  padding: 5px 10px;
}

.tl-body {
  min-width: 0;
}

.tl-title {
  font-weight: 600;
  color: var(--color-text-primary);
}

.tl-range {
  font-variant-numeric: tabular-nums;
}

.tl-loading {
  color: var(--color-text-disabled);
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
  background: var(--color-surface);
  border-radius: 6px;
  font-size: 14px;
  line-height: 1;
  color: var(--color-text-secondary);
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
  color: var(--color-text-primary);
}

.tl-clear {
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: var(--color-surface);
  border-radius: 6px;
  padding: 2px 8px;
  font-size: 12px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.tl-clear:hover {
  border-color: rgba(15, 23, 42, 0.28);
  color: var(--color-text-primary);
}

.tl-track {
  display: flex;
  align-items: flex-end;
  gap: 1px;
  height: 54px;
  margin-top: 6px;
  cursor: crosshair;
  background: linear-gradient(to top, rgba(143, 163, 173, 0.08), transparent);
  border-radius: 4px;
  position: relative;
}

/* V4 面积图层：覆盖在 bar 之上，不拦截拖拽/hover 指针 */
.tl-area {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none;
}

.tl-area-fill {
  fill: rgba(74, 90, 106, 0.14);
}

.tl-area-line {
  fill: none;
  stroke: var(--color-primary, #4a5a6a);
  stroke-width: 1.5;
}

/* 刷选命中层：视觉弱化为雾面残影（面积图承担主体视觉） */
.tl-bar {
  flex: 1 1 0;
  min-width: 2px;
  background: transparent;
  border-radius: 2px 2px 0 0;
  transition: background 0.15s, height 0.2s ease;
}

.tl-bar.on {
  background: rgba(74, 90, 106, 0.22);
}

.tl-bar.hv {
  background: rgba(143, 163, 173, 0.16);
}

/* 悬停参考线 */
.tl-cursor {
  position: absolute;
  top: 0;
  bottom: 0;
  width: 1px;
  background: rgba(74, 90, 106, 0.45);
  border-left: 1px dashed rgba(74, 90, 106, 0.35);
  pointer-events: none;
}

/* V2 分解气泡 */
.tl-tip {
  position: absolute;
  bottom: calc(100% + 8px);
  transform: translateX(-50%);
  background: var(--color-surface, #f4f1ed);
  border: 1px solid var(--color-border, #ddd8d0);
  border-radius: 8px;
  box-shadow: 0 2px 6px rgba(65, 64, 60, 0.06), 0 14px 40px rgba(65, 64, 60, 0.12);
  padding: 9px 13px;
  font-size: 11.5px;
  line-height: 1.8;
  color: var(--color-text-primary);
  pointer-events: none;
  z-index: 5;
  white-space: nowrap;
}

.tl-tip--flip {
  transform: translateX(-100%);
}

.tl-tip-key {
  font-family: 'IBM Plex Mono', monospace;
  margin-bottom: 3px;
}

.tl-tip-key b {
  font-weight: 500;
}

.tl-tip-r {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--color-text-secondary);
}

.tl-tip-r .d {
  width: 8px;
  height: 8px;
  border-radius: 2.5px;
  flex: none;
}

.tl-empty,
.tl-axis {
  margin-top: 4px;
  font-size: 11px;
  color: var(--color-text-disabled);
}

.tl-axis {
  display: flex;
  justify-content: space-between;
}
</style>
