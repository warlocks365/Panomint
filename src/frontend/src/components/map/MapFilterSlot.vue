<template>
  <!-- 桌面端筛选悬浮层（Job000059）：展开=浮在地图角部的卡片，收起=角部小药丸。
       位置跟随 side 偏好，收起状态由父级记忆在 uiPrefs.map_filter_collapsed。 -->
  <div
    v-if="!collapsed"
    class="map-filter-float"
    :class="{ 'map-filter-float--right': side === 'right' }"
    data-testid="map-filter-float"
  >
    <MapFilterBar
      :kind="kind"
      :side="side"
      :slider-pos="sliderPos"
      :provider="provider"
      :default-zoom="defaultZoom"
      :marker-mode="markerMode"
      @update:kind="$emit('update:kind', $event)"
      @update:side="$emit('update:side', $event)"
      @update:slider-pos="$emit('update:sliderPos', $event)"
      @update:provider="$emit('update:provider', $event)"
      @update:default-zoom="$emit('update:defaultZoom', $event)"
      @update:marker-mode="$emit('update:markerMode', $event)"
      @close="$emit('update:collapsed', true)"
    />
  </div>
  <button
    v-else
    class="map-filter-pill"
    :class="{ 'map-filter-pill--right': side === 'right' }"
    type="button"
    data-testid="map-filter-pill"
    title="展开筛选"
    @click="$emit('update:collapsed', false)"
  >
    <svg viewBox="0 0 16 16" width="14" height="14" fill="none" aria-hidden="true">
      <path d="M2 3.5h12l-4.6 5.2v4.1l-2.8 1.4V8.7L2 3.5z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round" />
    </svg>
    <span class="mfp-label">筛选</span>
    <span class="mfp-summary">{{ kindLabel }}</span>
  </button>
</template>

<script setup>
// 桌面筛选悬浮层——从 MapView 抽出（Job000059，顺带缓解其超大行数）。
import { computed } from 'vue'
import MapFilterBar from './MapFilterBar.vue'

const props = defineProps({
  kind: { type: String, default: 'all' },
  side: { type: String, default: 'left' },
  sliderPos: { type: String, default: 'bottom' },
  provider: { type: String, default: 'auto' },
  defaultZoom: { type: Number, default: null },
  markerMode: { type: String, default: 'icon' },
  collapsed: { type: Boolean, default: false }
})
defineEmits([
  'update:kind',
  'update:side',
  'update:sliderPos',
  'update:provider',
  'update:defaultZoom',
  'update:markerMode',
  'update:collapsed'
])

const KIND_LABELS = { all: '全部', photo: '照片', video: '视频', pano: '全景' }
const kindLabel = computed(() => KIND_LABELS[props.kind] || '全部')
</script>

<style scoped>
/* 悬浮卡片：叠加在地图之上，不压缩地图宽度 */
.map-filter-float {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 9;
  max-height: calc(100% - 120px);
  overflow-y: auto;
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-card);
}

.map-filter-float--right {
  left: auto;
  right: 12px;
}

.map-filter-float .map-filter-bar {
  border-radius: var(--radius-md);
}

/* 收起态：角部小药丸，带当前筛选摘要 */
.map-filter-pill {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 9;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: 999px;
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  box-shadow: var(--shadow-card);
}

.map-filter-pill--right {
  left: auto;
  right: 12px;
}

.map-filter-pill:hover {
  background-color: var(--color-surface-hover);
}

.map-filter-pill .mfp-summary {
  color: var(--color-text-secondary);
}
</style>
