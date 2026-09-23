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
        @pointermove="sel.onMove"
        @pointerup="sel.onUp"
        @pointercancel="sel.onUp"
      >
        <div
          v-for="(b, i) in sel.bars.value"
          :key="b.key"
          class="tl-bar"
          :class="{ on: sel.inRange(i) }"
          :style="{ height: sel.barHeight(b.count) + '%' }"
          :title="sel.barTitle(b)"
        ></div>
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
import { ref } from 'vue'
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
</script>

<style scoped>
.map-timeline {
  background: rgba(255, 255, 255, 0.96);
  border-top: 1px solid rgba(15, 23, 42, 0.08);
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
  background: #fff;
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
  background: #fff;
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
  background: #fff;
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
  background: linear-gradient(to top, rgba(148, 163, 184, 0.08), transparent);
  border-radius: 4px;
}

.tl-bar {
  flex: 1 1 0;
  min-width: 2px;
  background: var(--color-text-disabled);
  border-radius: 2px 2px 0 0;
  transition: background 0.15s, height 0.2s ease;
}

.tl-bar.on {
  background: var(--color-primary);
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
