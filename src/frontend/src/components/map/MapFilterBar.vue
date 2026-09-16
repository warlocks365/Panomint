<template>
  <div class="map-filter-bar" data-testid="map-filter-bar">
    <div class="mf-head">
      <span class="mf-title">筛选</span>
      <button
        v-show="mobile"
        class="mf-close"
        type="button"
        title="收起筛选"
        data-testid="map-filter-close"
        @click="$emit('close')"
      >×</button>
    </div>

    <section class="mf-section">
      <div class="mf-label">媒体类型</div>
      <div class="fc-options">
        <button
          v-for="opt in kindOptions"
          :key="opt.value"
          class="fc-option"
          :class="{ active: kind === opt.value }"
          :data-testid="opt.testid"
          type="button"
          @click="$emit('update:kind', opt.value)"
        >{{ opt.label }}</button>
      </div>
    </section>

    <section class="mf-section">
      <div class="mf-label">筛选栏位置</div>
      <div class="fc-options">
        <button
          class="fc-option"
          :class="{ active: side === 'left' }"
          type="button"
          data-testid="map-side-left"
          @click="$emit('update:side', 'left')"
        >左侧</button>
        <button
          class="fc-option"
          :class="{ active: side === 'right' }"
          type="button"
          data-testid="map-side-right"
          @click="$emit('update:side', 'right')"
        >右侧</button>
      </div>
    </section>

    <section class="mf-section">
      <div class="mf-label">时间轴位置</div>
      <div class="fc-options">
        <button
          class="fc-option"
          :class="{ active: sliderPos === 'bottom' }"
          type="button"
          data-testid="map-slider-bottom"
          @click="$emit('update:sliderPos', 'bottom')"
        >底部</button>
        <button
          class="fc-option"
          :class="{ active: sliderPos === 'top' }"
          type="button"
          data-testid="map-slider-top"
          @click="$emit('update:sliderPos', 'top')"
        >顶部</button>
      </div>
    </section>

    <section class="mf-section">
      <div class="mf-label">底图</div>
      <select
        class="mf-select"
        :value="provider"
        data-testid="map-provider"
        @change="$emit('update:provider', $event.target.value)"
      >
        <option v-for="opt in providerOptions" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
      </select>
      <!-- OSM 瓦片代理未接入：如实告知当前仍出高德底图，避免用户以为切换已生效 -->
      <div v-if="provider === 'osm'" class="mf-warn">OSM 瓦片代理尚未接入，当前仍使用高德底图</div>
    </section>

    <section class="mf-section">
      <div class="mf-label">默认缩放（1–20，留空为默认）</div>
      <input
        class="mf-number"
        type="number"
        min="1"
        max="20"
        :value="defaultZoom == null ? '' : String(defaultZoom)"
        data-testid="map-default-zoom"
        @change="onZoomChange"
      />
    </section>
  </div>
</template>

<script setup>
const props = defineProps({
  kind: { type: String, default: 'all' },
  side: { type: String, default: 'left' },
  sliderPos: { type: String, default: 'bottom' },
  provider: { type: String, default: 'auto' },
  defaultZoom: { type: Number, default: null },
  mobile: { type: Boolean, default: false }
})
const emit = defineEmits([
  'update:kind',
  'update:side',
  'update:sliderPos',
  'update:provider',
  'update:defaultZoom',
  'close'
])

const kindOptions = [
  { value: 'all', label: '全部', testid: 'map-kind-all' },
  { value: 'photo', label: '照片', testid: 'map-kind-photo' },
  { value: 'video', label: '视频', testid: 'map-kind-video' },
  { value: 'pano', label: '全景', testid: 'map-kind-pano' }
]

const providerOptions = [
  { value: 'auto', label: '自动' },
  { value: 'amap', label: '高德' },
  { value: 'osm', label: 'OSM' }
]

// 空串 → null（表示用后端默认缩放）；越界/非整数一律忽略并回显旧值，
// 不把脏数据写进偏好（后端对非法 map_default_zoom 会返回 400）。
function onZoomChange(e) {
  const raw = e.target.value.trim()
  if (!raw) {
    emit('update:defaultZoom', null)
    return
  }
  const n = Number(raw)
  if (!Number.isInteger(n) || n < 1 || n > 20) {
    e.target.value = props.defaultZoom == null ? '' : String(props.defaultZoom)
    return
  }
  emit('update:defaultZoom', n)
}
</script>

<style scoped>
/* 面板浮在地图上，沿用 .map-topbar 的半透明浮层观感 */
.map-filter-bar {
  width: 200px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 10px 12px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: var(--radius-md);
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
}

.mf-head {
  display: flex;
  align-items: center;
}

.mf-title {
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--color-text-primary);
}

.mf-close {
  margin-left: auto;
  width: 22px;
  height: 22px;
  line-height: 1;
  border: 1px solid var(--color-border);
  background: var(--color-surface);
  border-radius: var(--radius-sm);
  font-size: 14px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.mf-close:hover {
  color: var(--color-text-primary);
}

.mf-label {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  margin-bottom: 6px;
}

/* 与 search/FilterControls.vue 的分段控件保持一致 */
.fc-options {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.fc-option {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: 14px;
  padding: 4px 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  cursor: pointer;
}

.fc-option:hover {
  background-color: var(--color-surface-hover);
}

.fc-option.active {
  background-color: var(--color-primary-active-bg);
  border-color: var(--color-primary);
  color: var(--color-primary);
  font-weight: 600;
}

.mf-select,
.mf-number {
  width: 100%;
  height: 32px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 8px;
  outline: none;
}

.mf-select:focus,
.mf-number:focus {
  border-color: var(--color-primary);
}

.mf-warn {
  margin-top: 6px;
  font-size: var(--font-size-sm);
  line-height: 1.4;
  color: var(--color-warning-text);
  background: var(--color-warning-bg);
  border-radius: var(--radius-sm);
  padding: 6px 8px;
}
</style>
