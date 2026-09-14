<template>
  <div class="map-view" :class="{ 'map-view--mobile': isMobile }">
    <div ref="mapRef" class="map-canvas"></div>

    <!-- 浮层容器：顶栏 + 地名搜索框纵向排列；容器自身不吃鼠标事件，地图交互不受遮挡 -->
    <div class="map-float">
      <div class="map-topbar">
        <span class="mt-title">地图</span>
        <span class="mt-stat">{{ clusterCount }} 个位置 · {{ pointCount }} 项</span>
        <span v-if="err" class="mt-err">{{ err }}</span>
        <button class="mt-icon-btn" type="button" @click="iconPickerOpen = !iconPickerOpen">图标</button>
      </div>

      <!-- 地图内置地名搜索：回车 → /map/search → 候选下拉 → flyTo 定位（不再跳 /search） -->
      <div ref="searchRef" class="map-search" @keydown.esc="closeSearch">
        <div class="ms-box">
          <svg class="ms-icon" viewBox="0 0 16 16" width="14" height="14" fill="none">
            <circle cx="7" cy="7" r="5" stroke="currentColor" stroke-width="1.5" />
            <path d="M11 11l3.5 3.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
          </svg>
          <input
            v-model="q"
            class="ms-input"
            type="text"
            placeholder="搜索地名，回车定位"
            @keyup.enter="runSearch"
          />
          <button v-if="q" class="ms-clear" type="button" title="清空" @click="clearSearch">
            <svg viewBox="0 0 12 12" width="10" height="10" fill="none">
              <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </button>
        </div>

        <div v-if="searchOpen" class="ms-panel">
          <div v-if="searching" class="ms-hint">搜索中…</div>
          <div v-else-if="searchErr" class="ms-hint ms-hint--err">{{ searchErr }}</div>
          <div v-else-if="searchMsg" class="ms-hint">{{ searchMsg }}</div>
          <ul v-else class="ms-list">
            <li v-for="(c, i) in candidates" :key="`${c.name}-${i}`">
              <button class="ms-item" type="button" @click="pickCandidate(c)">
                <span class="ms-name">{{ c.name }}</span>
                <span v-if="c.provider" class="ms-provider">{{ providerLabel(c.provider) }}</span>
              </button>
            </li>
          </ul>
        </div>
      </div>
    </div>

    <MapIconPicker
      v-if="iconPickerOpen"
      :pref="iconPref"
      @update="onIconPrefUpdate"
      @close="iconPickerOpen = false"
    />

    <MapItemList
      v-if="listOpen"
      :items="items"
      :loading="listLoading"
      @close="closeList"
      @open="openItem"
    />

    <MapHoverCard
      v-if="hoverOpen"
      :items="hoverItems"
      :loading="hoverLoading"
      :place="hoverPlace"
      :pos="hoverPos"
      @open="openItem"
      @enter="onHoverCardEnter"
      @leave="onHoverCardLeave"
    />

    <MapTimeline
      :buckets="buckets"
      :range="range"
      :loading="timelineLoading"
      :granularity="granularity"
      :places="places"
      @change="onRangeChange"
      @zoom="onZoomChange"
      @place="onPlaceClick"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Map as MapLibreMap, NavigationControl, setWorkerUrl } from 'maplibre-gl' // v6 纯 ESM
import 'maplibre-gl/dist/maplibre-gl.css'
import { getAccessToken } from '../utils/tokenStore'
import {
  fetchClusters,
  fetchHistogram,
  fetchItems,
  fetchPlaces,
  getMapIconPref,
  putMapIconPref,
  searchPlaces
} from '../api/map'
import { useResponsive } from '../composables/useResponsive'
import MapTimeline from '../components/map/MapTimeline.vue'
import MapItemList from '../components/map/MapItemList.vue'
import MapIconPicker from '../components/map/MapIconPicker.vue'
import MapHoverCard from '../components/map/MapHoverCard.vue'

// 地图模式（Job000009 优化）：全屏地图 + 时间轴缩放滑块 + 图标可配置 + 悬停预览
const router = useRouter()
const mapRef = ref(null)
const { isMobile } = useResponsive()

const clusters = ref([])
const buckets = ref([])
const places = ref([])
const range = ref(null) // { from: ISO, to: ISO } | null
const granularity = ref('month') // year|month|day
const err = ref('')
const timelineLoading = ref(false)

const items = ref([])
const listOpen = ref(false)
const listLoading = ref(false)

const iconPickerOpen = ref(false)
const iconPref = ref({ shape: 'circle', color: '#ef4444' })

const hoverOpen = ref(false)
const hoverItems = ref([])
const hoverLoading = ref(false)
const hoverPlace = ref('')
const hoverPos = ref(null)
let hoverCloseTimer = null

const clusterCount = computed(() => clusters.value.length)
const pointCount = computed(() => clusters.value.reduce((s, c) => s + (c.count || 0), 0))

// ---- 地图内置地名搜索（问题⑥：地图内搜地名应定位地图，而不是被全局搜索框跳到 /search）----
const q = ref('')
const candidates = ref([])
const searchOpen = ref(false)
const searching = ref(false)
const searchErr = ref('')
const searchMsg = ref('') // 「未找到该地点」等空结果提示
const searchRef = ref(null)
let searchReqId = 0 // 过期请求丢弃（连续回车时只认最后一次）

function providerLabel(p) {
  return { amap: '高德', nominatim: 'OSM' }[p] || p
}

// 回车检索：q 为空不发请求（后端对空 q 返回 400 INVALID_PARAMS，前端先拦）
async function runSearch() {
  const text = q.value.trim()
  if (!text) {
    closeSearch()
    return
  }
  const reqId = ++searchReqId
  searching.value = true
  searchOpen.value = true
  searchErr.value = ''
  searchMsg.value = ''
  candidates.value = []
  try {
    const list = await searchPlaces(text)
    if (reqId !== searchReqId) return
    candidates.value = list
    if (!list.length) searchMsg.value = '未找到该地点'
  } catch (e) {
    if (reqId !== searchReqId) return
    // 失败只在地图内提示，绝不跳路由
    searchErr.value = e?.response?.data?.error?.message || '地点搜索失败，请稍后重试'
  } finally {
    if (reqId === searchReqId) searching.value = false
  }
}

// 选中候选 → 地图定位。后端返回 GCJ-02（默认 display provider=amap），
// 与高德栅格底图同坐标系，无需再转换。
function pickCandidate(c) {
  if (!map || typeof c.lon !== 'number' || typeof c.lat !== 'number') return
  map.flyTo({
    center: [c.lon, c.lat],
    zoom: Math.max(14, map.getZoom()),
    duration: 800
  })
  scheduleReload()
  closeSearch()
}

function closeSearch() {
  searchOpen.value = false
  candidates.value = []
  searchErr.value = ''
  searchMsg.value = ''
}

function clearSearch() {
  q.value = ''
  closeSearch()
}

function onSearchOutside(e) {
  if (searchRef.value && !searchRef.value.contains(e.target)) closeSearch()
}

let map = null
let reloadTimer = null
let hoverTimer = null
let hoverRequestId = 0
let touchStartInfo = null

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

// ---- 图标：矢量形状 SVG path（填充色由 iconPref.color 控制）----
const SHAPE_PATHS = {
  circle: '<circle cx="12" cy="12" r="10" />',
  triangle: '<path d="M12 2l10 20H2z" />',
  diamond: '<path d="M12 2l10 10-10 10L2 12z" />',
  star: '<path d="M12 2l3 6.5 7 .8-5.2 4.7 1.4 7-6.2-3.6L5.8 21l1.4-7L2 9.3l7-.8z" />'
}

function shapeSvgDataUrl(shape, color) {
  const path = SHAPE_PATHS[shape]
  if (!path) return null
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24"><g fill="${color}">${path}</g></svg>`
  return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg)
}

// 加载图标到 map（矢量 SVG → dataURL；内置 PNG → 静态路径；自定义 → dataURL）
function applyIcon() {
  if (!map || !map.hasImage) return
  const pref = iconPref.value
  let url = null
  if (pref.shape === 'pin') url = '/map-icons/pin.png'
  else if (pref.shape === 'inverted') url = '/map-icons/inverted.png'
  else if (pref.shape === 'custom' && pref.data_url) url = pref.data_url
  else url = shapeSvgDataUrl(pref.shape, pref.color || '#ef4444')

  if (!url) return
  const img = new Image()
  img.onload = () => {
    if (!map) return
    // 尺寸统一 24px；删除旧图标避免累积
    if (map.hasImage('cluster-icon')) map.removeImage('cluster-icon')
    map.addImage('cluster-icon', img, { sdf: false })
  }
  img.src = url
}

// ---- 图标偏好：账户级持久化（服务端失败降级 localStorage）----
async function loadIconPref() {
  try {
    const p = await getMapIconPref()
    if (p && p.shape) {
      iconPref.value = { shape: p.shape, color: p.color || '#ef4444', data_url: p.data_url || '' }
    }
  } catch {
    // 服务端不可达 → 读本地兜底
    try {
      const local = localStorage.getItem('map_icon_pref')
      if (local) iconPref.value = JSON.parse(local)
    } catch {}
  }
}

async function onIconPrefUpdate(pref) {
  iconPref.value = pref
  applyIcon()
  // 本地兜底 + 服务端持久化
  try { localStorage.setItem('map_icon_pref', JSON.stringify(pref)) } catch {}
  try {
    await putMapIconPref(pref)
  } catch {
    err.value = '图标偏好已本地保存，服务端同步失败'
  }
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
    const [cs, hs, ps] = await Promise.all([
      fetchClusters(bbox, zoom, from, to),
      fetchHistogram(bbox, granularity.value).catch(() => []),
      fetchPlaces(bbox, from, to).catch(() => [])
    ])
    clusters.value = cs
    buckets.value = hs
    places.value = ps
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
  reloadTimer = setTimeout(reload, 250)
}

function onRangeChange(next) {
  range.value = next
  closeList()
  reload()
}

function onZoomChange(next) {
  granularity.value = next
  reload() // 重取直方图（新粒度 + 四类计数）
}

// 点击位置标签 → 地图飞过去定位（放大到街道级，突出该地点）
function onPlaceClick(place) {
  if (!map || place.lng === undefined || place.lat === undefined) return
  closeHover()
  map.flyTo({
    center: [place.lng, place.lat],
    zoom: Math.max(14, map.getZoom() + 6),
    duration: 800
  })
}

async function openCluster(props, lngLat) {
  const zoom = map.getZoom()
  if (props.count > 1 && zoom < 12) {
    map.flyTo({ center: lngLat, zoom: Math.min(18, zoom + 2), duration: 500 })
    return
  }
  const half = 0.02
  const bbox = {
    minLng: lngLat[0] - half,
    minLat: lngLat[1] - half,
    maxLng: lngLat[0] + half,
    maxLat: lngLat[1] + half
  }
  listLoading.value = true
  listOpen.value = true
  items.value = []
  closeHover()
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

// ---- 悬停预览（mouseenter）+ 长按预览（touch）----
function closeHover() {
  hoverOpen.value = false
  hoverItems.value = []
  hoverPos.value = null
}

// 与后端 GridSize 一致的网格边长（度）：z=zoom → 180/2^zoom
function gridSizeAt(zoom) {
  let g = 180 / Math.pow(2, zoom)
  if (g < 0.0005) g = 0.0005
  return g
}

function showHover(feature, clientX, clientY) {
  const coord = feature.geometry.coordinates.slice()
  // 用当前 zoom 的聚合网格尺寸做 bbox，命中该簇聚合的所有媒体。
  // 后端 Clusters 用 ST_SnapToGrid(gps, GridSize(zoom)) 聚合，质心为网格中心，
  // 故 hover bbox 应以质心为中心、半边长 = GridSize(zoom)（覆盖整个网格），
  // 而非 GridSize/2（只覆盖 1/4，缩小状态下只能命中 1 张 → 统计不准）。
  const half = gridSizeAt(map.getZoom())
  const bbox = {
    minLng: coord[0] - half,
    minLat: coord[1] - half,
    maxLng: coord[0] + half,
    maxLat: coord[1] + half
  }
  hoverPos.value = { x: clientX, y: clientY }
  hoverLoading.value = true
  hoverOpen.value = true
  hoverItems.value = []

  const reqId = ++hoverRequestId
  // limit 提高，支持翻书与缩略图条
  fetchItems(bbox, range.value?.from || '', range.value?.to || '', 60)
    .then((list) => {
      if (reqId !== hoverRequestId) return // 过期请求丢弃
      hoverItems.value = list
      hoverPlace.value = list[0]?.place || ''
      hoverLoading.value = false
    })
    .catch(() => {
      if (reqId === hoverRequestId) {
        hoverItems.value = []
        hoverLoading.value = false
      }
    })
}

// 鼠标移开地图簇点后延迟关闭（给用户移入卡片的时间）；移入卡片则取消关闭
function scheduleHoverClose() {
  clearTimeout(hoverCloseTimer)
  hoverCloseTimer = setTimeout(() => closeHover(), 250)
}

function onHoverCardEnter() {
  clearTimeout(hoverCloseTimer) // 移入卡片，取消关闭
}

function onHoverCardLeave() {
  scheduleHoverClose()
}

onMounted(async () => {
  setWorkerUrl('/maplibre-gl-worker.mjs')
  // 点击地图（或页面其他区域）关闭候选下拉
  document.addEventListener('click', onSearchOutside)
  await loadIconPref()

  map = new MapLibreMap({
    container: mapRef.value,
    style: rasterStyle,
    center: [116.397, 39.909],
    zoom: 4,
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
      type: 'symbol',
      source: 'clusters',
      layout: {
        'icon-image': 'cluster-icon',
        'icon-size': 1,
        'icon-allow-overlap': true
      }
    })

    // 加载初始图标（默认红点）
    applyIcon()

    // 点击下钻 / 展开
    map.on('click', 'cluster-circles', (e) => {
      const f = e.features?.[0]
      if (!f) return
      openCluster(f.properties, f.geometry.coordinates.slice())
    })

    // 悬停预览（桌面）
    map.on('mouseenter', 'cluster-circles', (e) => {
      const f = e.features?.[0]
      if (!f) return
      clearTimeout(hoverTimer)
      clearTimeout(hoverCloseTimer)
      hoverTimer = setTimeout(() => {
        showHover(f, e.originalEvent.clientX, e.originalEvent.clientY)
      }, 120)
    })
    map.on('mousemove', 'cluster-circles', () => {
      map.getCanvas().style.cursor = 'pointer'
    })
    map.on('mouseleave', 'cluster-circles', () => {
      clearTimeout(hoverTimer)
      map.getCanvas().style.cursor = ''
      // 不立即关闭：给用户移入卡片点选的时间
      scheduleHoverClose()
    })

    window.__map = map
    reload()
  })

  // 长按预览（触摸）：500ms 未移动触发，移动超阈值取消（与平移互斥）
  const canvas = map.getCanvas()
  canvas.addEventListener('touchstart', (e) => {
    if (e.touches.length !== 1) return
    const t = e.touches[0]
    touchStartInfo = { x: t.clientX, y: t.clientY, t: Date.now() }
    clearTimeout(hoverTimer)
    hoverTimer = setTimeout(() => {
      const fs = map.queryRenderedFeatures([t.clientX, t.clientY], { layers: ['cluster-circles'] })
      if (fs.length) showHover(fs[0], t.clientX, t.clientY)
      touchStartInfo = null
    }, 500)
  }, { passive: true })
  canvas.addEventListener('touchmove', (e) => {
    if (!touchStartInfo || !e.touches.length) return
    const t = e.touches[0]
    const dx = t.clientX - touchStartInfo.x
    const dy = t.clientY - touchStartInfo.y
    if (dx * dx + dy * dy > 100) { // 移动超过 10px → 判定为平移，取消长按
      clearTimeout(hoverTimer)
      closeHover()
      touchStartInfo = null
    }
  }, { passive: true })
  canvas.addEventListener('touchend', () => {
    clearTimeout(hoverTimer)
    // 延迟关闭预览（长按松手后短暂停留，给点选时间）
    scheduleHoverClose()
    touchStartInfo = null
  }, { passive: true })

  map.on('moveend', scheduleReload)
  map.on('zoomend', scheduleReload)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onSearchOutside)
  clearTimeout(reloadTimer)
  clearTimeout(hoverTimer)
  clearTimeout(hoverCloseTimer)
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

/* 浮层容器：绝对定位在画布左上；pointer-events:none 让地图交互完全不受容器遮挡 */
.map-float {
  position: absolute;
  top: 12px;
  left: 12px;
  right: 12px;
  z-index: 5;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  pointer-events: none;
}

.map-float > * {
  pointer-events: auto;
}

.map-topbar {
  display: flex;
  align-items: center;
  gap: 10px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  padding: 6px 12px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
}

/* 移动端：数值统计让位，避免与搜索框一起挤爆 390px 宽 */
.map-view--mobile .mt-stat {
  display: none;
}

/* ---- 地名搜索（浮层风格与 .map-topbar 一致）---- */
.map-search {
  width: 320px;
  max-width: 100%;
}

.map-view--mobile .map-search {
  align-self: stretch;
  width: auto;
}

.ms-box {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 12px;
  background: rgba(255, 255, 255, 0.95);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
  color: #94a3b8;
}

.ms-box:focus-within {
  border-color: #2563eb;
}

.ms-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 13px;
  color: #0f172a;
}

.ms-clear {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  border: none;
  border-radius: 50%;
  background: #e2e4e9;
  color: #646a73;
  padding: 0;
}

.ms-panel {
  margin-top: 6px;
  max-height: 240px;
  overflow-y: auto;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(15, 23, 42, 0.08);
  border-radius: 8px;
  box-shadow: 0 4px 14px rgba(15, 23, 42, 0.08);
  padding: 4px;
}

.ms-list {
  list-style: none;
  margin: 0;
  padding: 0;
}

.ms-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
  padding: 7px 8px;
  border-radius: 6px;
  font-size: 13px;
  color: #0f172a;
}

.ms-item:hover {
  background: #f1f5f9;
}

.ms-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ms-provider {
  flex-shrink: 0;
  font-size: 11px;
  color: #94a3b8;
}

.ms-hint {
  padding: 8px;
  font-size: 12px;
  color: #64748b;
}

.ms-hint--err {
  color: #dc2626;
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

.mt-icon-btn {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 3px 10px;
  font-size: 12px;
  color: #475569;
  cursor: pointer;
}

.mt-icon-btn:hover {
  border-color: rgba(15, 23, 42, 0.28);
  color: #0f172a;
}
</style>
