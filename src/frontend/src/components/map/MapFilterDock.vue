<template>
  <!-- 桌面端：筛选悬浮层（展开=浮层卡片 / 收起=角部药丸），不压缩地图宽度 -->
  <MapFilterSlot
    v-if="!isMobile"
    :kind="kind"
    :side="uiPrefs.map_filter_side"
    :slider-pos="uiPrefs.map_slider_pos"
    :provider="uiPrefs.map_default_provider"
    :default-zoom="uiPrefs.map_default_zoom"
    :collapsed="uiPrefs.map_filter_collapsed"
    :marker-mode="markerMode"
    @update:kind="$emit('update:kind', $event)"
    @update:side="$emit('patch', { map_filter_side: $event })"
    @update:slider-pos="$emit('patch', { map_slider_pos: $event })"
    @update:provider="$emit('patch', { map_default_provider: $event })"
    @update:default-zoom="$emit('patch', { map_default_zoom: $event })"
    @update:collapsed="$emit('update:collapsed', $event)"
    @update:marker-mode="$emit('update:markerMode', $event)"
  />

  <!-- 移动端：筛选栏以浮层形式展开（顶栏「筛选」按钮开关） -->
  <div
    v-else-if="filterOpen"
    class="map-filter-overlay"
    data-testid="map-filter-overlay"
    :class="{ 'map-filter-overlay--right': uiPrefs.map_filter_side === 'right' }"
  >
    <MapFilterBar
      :kind="kind"
      :side="uiPrefs.map_filter_side"
      :slider-pos="uiPrefs.map_slider_pos"
      :provider="uiPrefs.map_default_provider"
      :default-zoom="uiPrefs.map_default_zoom"
      :marker-mode="markerMode"
      mobile
      @update:kind="$emit('update:kind', $event)"
      @update:side="$emit('patch', { map_filter_side: $event })"
      @update:slider-pos="$emit('patch', { map_slider_pos: $event })"
      @update:provider="$emit('patch', { map_default_provider: $event })"
      @update:default-zoom="$emit('patch', { map_default_zoom: $event })"
      @update:marker-mode="$emit('update:markerMode', $event)"
      @close="$emit('close')"
    />
  </div>
</template>

<script setup>
import MapFilterSlot from './MapFilterSlot.vue'
import MapFilterBar from './MapFilterBar.vue'

defineProps({
  kind: { type: String, default: 'all' },
  uiPrefs: { type: Object, required: true },
  markerMode: { type: String, default: 'icon' },
  isMobile: { type: Boolean, default: false },
  filterOpen: { type: Boolean, default: false }
})
defineEmits(['update:kind', 'patch', 'update:collapsed', 'update:markerMode', 'close'])
</script>

<style scoped>
/* 桌面端筛选栏侧栏：左侧 order:0，右侧 order:2 */
.map-filter-overlay {
  position: absolute;
  top: 100px;
  left: 12px;
  right: 12px;
  /* 矮屏（横屏 390px 高）放不下 434px 的面板 → 限定高度并允许浮层自身滚动，
     否则底部控件够不到（此时靠 align-items 保持面板自身高度，不被拉满） */
  bottom: 12px;
  z-index: 8;
  display: flex;
  align-items: flex-start;
  justify-content: flex-start;
  overflow-y: auto;
  pointer-events: none;
}

.map-filter-overlay--right {
  justify-content: flex-end;
}

.map-filter-overlay > * {
  pointer-events: auto;
}
</style>
