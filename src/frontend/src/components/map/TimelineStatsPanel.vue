<template>
  <div class="stats-panel">
    <div class="tl-stats">
      <div class="stat-cell">
        <span class="stat-dot" style="background:var(--stat-photo)"></span>
        <span class="stat-name">照片</span>
        <span class="stat-num">{{ totals.photos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:var(--stat-video)"></span>
        <span class="stat-name">视频</span>
        <span class="stat-num">{{ totals.videos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:var(--stat-pano-photo)"></span>
        <span class="stat-name">全景照片</span>
        <span class="stat-num">{{ totals.panoPhotos }}</span>
      </div>
      <div class="stat-cell">
        <span class="stat-dot" style="background:var(--stat-pano-video)"></span>
        <span class="stat-name">全景视频</span>
        <span class="stat-num">{{ totals.panoVideos }}</span>
      </div>
    </div>

    <div v-if="places.length" class="tl-places">
      <span class="pl-label">位置</span>
      <div class="pl-scroll">
        <button
          v-for="p in places"
          :key="p.name"
          class="pl-chip"
          type="button"
          :title="`${p.name} · ${p.count} 项（点击定位）`"
          @click="$emit('place', p)"
        >{{ p.name }}</button>
      </div>
    </div>
  </div>
</template>

<script setup>
// MapTimeline 拆解（Job000089）：统计四格 + 位置 chips 展示面板——
// 纯展示组件：totals（useMapTimelineSel.statTotals 已聚合四类计数）与 places 经 props 注入，
// 点击位置 chip 原样上抛 place 事件（宿主再抛给 MapView 定位）。
defineProps({
  totals: { type: Object, required: true }, // { photos, videos, panoPhotos, panoVideos }
  places: { type: Array, default: () => [] } // [{ name, count }]
})
defineEmits(['place'])
</script>

<style scoped>
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
  background: var(--color-bg);
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
  color: var(--color-text-secondary);
  white-space: nowrap;
}

.stat-num {
  margin-left: auto;
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text-primary);
  font-variant-numeric: tabular-nums;
}

.tl-places {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 8px;
}

.pl-label {
  font-size: 11px;
  color: var(--color-text-disabled);
  flex-shrink: 0;
}

.pl-scroll {
  flex: 1;
  display: flex;
  gap: 6px;
  overflow-x: auto;
  overflow-y: hidden;
  white-space: nowrap;
  scrollbar-width: thin;
  -webkit-overflow-scrolling: touch;
}

.pl-scroll::-webkit-scrollbar {
  height: 4px;
}

.pl-scroll::-webkit-scrollbar-thumb {
  background: rgba(148, 163, 184, 0.5);
  border-radius: 2px;
}

.pl-chip {
  flex-shrink: 0;
  font-size: 11px;
  color: var(--color-text-secondary);
  background: var(--color-bg);
  border: none;
  border-radius: 10px;
  padding: 2px 10px;
  cursor: pointer;
}

.pl-chip:hover {
  background: var(--color-surface-hover);
  color: var(--color-text-primary);
}
</style>
