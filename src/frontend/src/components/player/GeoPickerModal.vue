<script setup>
// Job000143：轻量地图选点弹层。
//
// 为什么不用 views/map/useMapEngine.js（173 行）：它耦合了图层渲染、标记模式、
// 时间轴 chrome、hover 卡片等一堆本场景不需要的东西。本组件只做一件事 ——
// 「让用户点一下地图，取那个点的坐标」。
//
// 坐标系（本组件最需要小心的点）：
//   · 底图是**高德栅格瓦片**（GCJ-02）
//   · MediaEditor 的 form.lat/lng 存的也是 GCJ-02（与底图一致，用户所见即所存）
//   · 提交时由 MetadataEditor 带 coordSource='gcj02'，**服务端**转成 WGS-84 落库
//   · 初始位置若来自媒体详情（已是 WGS-84），显示前需 WGS84→GCJ02，
//     否则标记会落在偏西 500 米处，用户一看就知道不对
//
// 破坏性约束：组件卸载时必须 map.remove()，否则 maplibre 的 canvas 与
// 事件监听会泄漏（多次开关弹层会累积多个实例）。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Map as MapLibreMap, Marker, setWorkerUrl } from 'maplibre-gl'
import { getAccessToken } from '../../utils/tokenStore'
import { wgs84ToGcj02 } from '../../utils/coord'

const props = defineProps({
  // { lat, lon, wgs84: true } 或 null（无初始位置，默认全国视野）
  init: { type: Object, default: null }
})
const emit = defineEmits(['pick', 'close'])

const mapEl = ref(null)
const picked = ref(null)
let map = null
let marker = null

// 与 useMapEngine 同一份栅格样式与 worker 路径（vite 插件已把它复制到 public/）
const rasterStyle = {
  version: 8,
  sources: {
    amap: {
      type: 'raster',
      tiles: ['/tiles/amap/{z}/{x}/{y}'],
      tileSize: 256,
      attribution: '© 高德地图'
    }
  },
  layers: [{ id: 'amap-base', type: 'raster', source: 'amap' }]
}

const DEFAULT_CENTER = [116.397, 39.909] // 北京，与 MapView 一致

onMounted(() => {
  setWorkerUrl('/maplibre-gl-worker.mjs')

  // 初始坐标：WGS-84 需转 GCJ-02 才能与高德底图对齐
  let center = DEFAULT_CENTER
  let zoom = 4
  if (props.init && typeof props.init.lat === 'number') {
    const c = props.init.wgs84
      ? wgs84ToGcj02(props.init.lon, props.init.lat)
      : [props.init.lon, props.init.lat]
    center = c
    zoom = 15
  }

  map = new MapLibreMap({
    container: mapEl.value,
    style: rasterStyle,
    center,
    zoom,
    // 瓦片经本站反代，需带 JWT（与 useMapEngine 同一处理）
    transformRequest: (url) => {
      if (url.includes('/tiles/')) {
        const token = getAccessToken()
        if (token) return { url, headers: { Authorization: `Bearer ${token}` } }
      }
      return { url }
    }
  })

  map.on('click', (e) => {
    const pos = { lat: e.lngLat.lat, lon: e.lngLat.lng }
    picked.value = pos
    if (marker) marker.setLngLat([pos.lon, pos.lat])
    else {
      marker = new Marker({ draggable: true }).setLngLat([pos.lon, pos.lat]).addTo(map)
      // 拖动标记等同重新选点
      marker.on('dragend', () => {
        const ll = marker.getLngLat()
        picked.value = { lat: ll.lat, lon: ll.lng }
      })
    }
  })

  // 已有初始位置时先把标记放上（用户点「确定」而不改位置即为保留原值）
  if (props.init && typeof props.init.lat === 'number') {
    picked.value = { lat: props.init.lat, lon: props.init.lon }
    marker = new Marker({ draggable: true }).setLngLat(center).addTo(map)
    marker.on('dragend', () => {
      const ll = marker.getLngLat()
      picked.value = { lat: ll.lat, lon: ll.lng }
    })
  }
})

onBeforeUnmount(() => {
  // 必须显式销毁：maplibre 的 canvas / worker / 事件监听不会随 Vue 卸载自动释放
  if (map) {
    map.remove()
    map = null
  }
  marker = null
})

function confirm() {
  if (picked.value) emit('pick', picked.value)
  else emit('close')
}
</script>

<template>
  <div class="gp-overlay" data-testid="geo-picker" @click.self="emit('close')">
    <div class="gp-panel">
      <header class="gp-head">
        <h4 class="gp-title">地图选点</h4>
        <button class="gp-x" type="button" aria-label="关闭" @click="emit('close')">×</button>
      </header>

      <div ref="mapEl" class="gp-map" data-testid="gp-map"></div>

      <p class="gp-hint">
        点击地图选点，拖动标记可微调。<span v-if="picked" class="gp-coord">{{
          picked.lat.toFixed(6)
        }}, {{ picked.lon.toFixed(6) }}</span>
        <span v-else>尚未选点。</span>
      </p>

      <footer class="gp-actions">
        <button class="gp-btn gp-btn--primary" type="button" :disabled="!picked" @click="confirm">
          确定
        </button>
        <button class="gp-btn" type="button" @click="emit('close')">取消</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.gp-overlay {
  position: fixed;
  inset: 0;
  z-index: 60; /* 玩家信息面板之上 */
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(65, 64, 60, 0.34);
}

.gp-panel {
  width: min(560px, calc(100vw - 32px));
  background: var(--color-surface, #f4f1ed);
  border-radius: 14px;
  box-shadow: var(--shadow-lift);
  overflow: hidden;
}

.gp-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 14px;
}

.gp-title {
  margin: 0;
  font-size: var(--font-size-sm);
  font-weight: 600;
  color: var(--color-text-primary);
}

.gp-x {
  width: 24px;
  height: 24px;
  font-size: 18px;
  line-height: 1;
  color: var(--color-text-secondary);
  background: transparent;
  border: none;
  border-radius: 6px;
  cursor: pointer;
}

.gp-x:hover {
  background: var(--color-bg, #e9e4de);
}

.gp-map {
  height: 340px;
  background: var(--color-bg, #e9e4de);
}

.gp-hint {
  margin: 0;
  padding: 10px 14px 0;
  font-size: var(--font-size-xs, 11px);
  color: var(--color-text-secondary);
}

.gp-coord {
  font-family: var(--font-mono, monospace);
  margin-left: 6px;
  color: var(--color-text-primary);
}

.gp-actions {
  display: flex;
  gap: 8px;
  padding: 12px 14px 14px;
}

.gp-btn {
  padding: 6px 16px;
  font-size: var(--font-size-sm);
  font-family: inherit;
  color: var(--color-text-primary);
  background: var(--color-bg, #e9e4de);
  border: none;
  border-radius: 999px;
  cursor: pointer;
}

.gp-btn--primary {
  color: var(--color-on-primary, #f4f1ed);
  background: var(--color-primary, #4a5a6a);
}

.gp-btn:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}

@media (max-width: 640px) {
  .gp-map {
    height: 260px;
  }
}
</style>
