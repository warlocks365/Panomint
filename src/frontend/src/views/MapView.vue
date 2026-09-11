<template>
  <div class="map-view">
    <div ref="mapRef" class="map-canvas"></div>

    <div class="map-topbar">
      <span class="mt-title">地图</span>
      <span class="mt-stat">{{ clusterCount }} 个位置 · {{ pointCount }} 项</span>
      <span v-if="err" class="mt-err">{{ err }}</span>
    </div>

    <MapItemList
      v-if="listOpen"
      :items="items"
      :loading="listLoading"
      @close="closeList"
      @open="openItem"
    />

    <MapTimeline
      :buckets="buckets"
      :range="range"
      :loading="timelineLoading"
      @change="onRangeChange"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Map as MapLibreMap, NavigationControl, setWorkerUrl } from 'maplibre-gl' // v6 为纯 ESM，无 default 导出
import 'maplibre-gl/dist/maplibre-gl.css'
import { getAccessToken } from '../utils/tokenStore'
import { fetchClusters, fetchHistogram, fetchItems } from '../api/map'
import MapTimeline from '../components/map/MapTimeline.vue'
import MapItemList from '../components/map/MapItemList.vue'

// 地图模式（Job000009）：全屏地图 + 时间轴双向联动
// 坐标系：底图为高德栅格瓦片（GCJ-02），后端 /geo/* 在 provider=amap 下输出的坐标也是 GCJ-02，
// 两侧一致，可直接上图层；请求 bbox 直接取 map.getBounds()（后端负责还原为库内 WGS-84）。

const router = useRouter()
const mapRef = ref(null)

const clusters = ref([])
const buckets = ref([])
const range = ref(null) // { from: ISO, to: ISO } | null
const err = ref('')
const timelineLoading = ref(false)

const items = ref([])
const listOpen = ref(false)
const listLoading = ref(false)

const clusterCount = computed(() => clusters.value.length)
const pointCount = computed(() => clusters.value.reduce((s, c) => s + (c.count || 0), 0))

let map = null
let reloadTimer = null

// 高德栅格瓦片（经本站反代，Key 不下发浏览器）
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

function bboxOf() {
  const b = map.getBounds()
  return {
    minLng: b.getWest(),
    minLat: b.getSouth(),
    maxLng: b.getEast(),
    maxLat: b.getNorth()
  }
}

function toGeoJSON(list) {
  return {
    type: 'FeatureCollection',
    features: list.map((c, i) => ({
      type: 'Feature',
      id: i,
      geometry: { type: 'Point', coordinates: [c.lng, c.lat] },
      properties: { count: c.count }
    }))
  }
}

async function reload() {
  if (!map) return
  const bbox = bboxOf()
  const zoom = map.getZoom()
  const from = range.value?.from || ''
  const to = range.value?.to || ''

  timelineLoading.value = true
  try {
    const [cs, hs] = await Promise.all([
      fetchClusters(bbox, zoom, from, to),
      fetchHistogram(bbox).catch(() => [])
    ])
    clusters.value = cs
    buckets.value = hs
    map.getSource('clusters')?.setData(toGeoJSON(cs))
    err.value = ''
  } catch (e) {
    err.value = e?.response?.data?.error?.message || '地图数据加载失败'
  } finally {
    timelineLoading.value = false
  }
}

function scheduleReload() {
  clearTimeout(reloadTimer)
  reloadTimer = setTimeout(reload, 250) // 拖动过程不打断，停手后再查
}

function onRangeChange(next) {
  range.value = next
  closeList()
  reload()
}

async function openCluster(props, lngLat) {
  // 单条目或已放大到街道级 → 直接列出该处媒体；否则继续下钻
  const zoom = map.getZoom()
  if (props.count > 1 && zoom < 12) {
    map.flyTo({ center: lngLat, zoom: Math.min(18, zoom + 2), duration: 500 })
    return
  }
  const half = 0.02 // 以簇为中心的经纬度半窗（低 zoom 下的网格尺寸量级）
  const bbox = {
    minLng: lngLat[0] - half,
    minLat: lngLat[1] - half,
    maxLng: lngLat[0] + half,
    maxLat: lngLat[1] + half
  }
  listLoading.value = true
  listOpen.value = true
  items.value = []
  try {
    items.value = await fetchItems(bbox, range.value?.from || '', range.value?.to || '')
  } catch (e) {
    err.value = '条目加载失败'
  } finally {
    listLoading.value = false
  }
}

function closeList() {
  listOpen.value = false
  items.value = []
}

function openItem(it) {
  router.push(`/player/${it.id}`)
}

onMounted(() => {
  // v6 GeoJSON worker：vite 打包后 worker 相对路径失效，构建时由 vite.config 复制到 public/，
  // 此处显式指定（缺失时 GeoJSON 图层静默不渲染，无任何报错）
  setWorkerUrl('/maplibre-gl-worker.mjs')
  map = new MapLibreMap({
    container: mapRef.value,
    style: rasterStyle,
    center: [116.397, 39.909], // 北京（GCJ-02，与底图一致）
    zoom: 4,
    // 瓦片走本站鉴权端点：MapLibre 默认不带 Authorization，需在此注入
    transformRequest: (url) => {
      if (url.includes('/tiles/')) {
        const token = getAccessToken()
        if (token) return { url, headers: { Authorization: `Bearer ${token}` } }
      }
      return { url }
    }
  })
  map.addControl(new NavigationControl({ showCompass: false }), 'top-right')

  map.on('load', () => {
    map.addSource('clusters', { type: 'geojson', data: toGeoJSON([]) })
    map.addLayer({
      id: 'cluster-circles',
      type: 'circle',
      source: 'clusters',
      paint: {
        'circle-color': '#2563eb',
        'circle-opacity': 0.85,
        'circle-stroke-width': 2,
        'circle-stroke-color': '#ffffff',
        // 半径随聚合数量增长（1→7，大量→20）
        'circle-radius': [
          'interpolate',
          ['linear'],
          ['get', 'count'],
          1, 7,
          10, 12,
          100, 20
        ]
      }
    })

    map.on('click', 'cluster-circles', (e) => {
      const f = e.features?.[0]
      if (!f) return
      openCluster(f.properties, f.geometry.coordinates.slice())
    })
    map.on('mouseenter', 'cluster-circles', () => {
      map.getCanvas().style.cursor = 'pointer'
    })
    map.on('mouseleave', 'cluster-circles', () => {
      map.getCanvas().style.cursor = ''
    })

    // 调试/自动化验证用：暴露只读 map 引用（无安全影响，便于投影坐标计算）
    window.__map = map
    reload()
  })

  // 视口变化 → 重算聚合 + 时间轴（地图 → 时间轴）
  map.on('moveend', scheduleReload)
  map.on('zoomend', scheduleReload)
})

onBeforeUnmount(() => {
  clearTimeout(reloadTimer)
  map?.remove()
  map = null
})
</script>

<style scoped>
.map-view {
  position: relative;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

.map-canvas {
  flex: 1 1 auto;
  min-height: 0;
  background: #e8edf2;
}

.map-topbar {
  position: absolute;
  top: 12px;
  left: 12px;
  z-index: 5;
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  padding: 6px 12px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
}

.mt-title {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
}

.mt-stat {
  font-size: 12px;
  color: #64748b;
  font-variant-numeric: tabular-nums;
}

.mt-err {
  font-size: 12px;
  color: #dc2626;
}
</style>
