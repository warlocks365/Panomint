<template>
  <div class="map-view" :class="{ 'map-view--mobile': isMobile }">
    <!-- 地图主区：浮层与时间轴都挂在这里（.map-main 为 relative，绝对定位于其上的浮层仍能正确锚定） -->
    <div class="map-main">
      <div ref="mapRef" class="map-canvas"></div>

      <!-- 浮层容器：顶栏 + 地名搜索框纵向排列；容器自身不吃鼠标事件，地图交互不受遮挡 -->
      <div class="map-float">
        <div class="map-topbar">
          <span class="mt-title">地图</span>
          <span class="mt-stat">{{ clusterCount }} 个位置 · {{ pointCount }} 项</span>
          <span v-if="err" class="mt-err">{{ err }}</span>
          <button class="mt-icon-btn" type="button" @click="iconPickerOpen = !iconPickerOpen">图标</button>
          <!-- 移动端筛选栏平时收起，点此按钮以浮层展开（桌面端常驻侧栏，无需开关） -->
          <button
            v-if="isMobile"
            class="mt-icon-btn"
            type="button"
            data-testid="map-filter-toggle"
            @click="filterOpen = !filterOpen"
          >筛选</button>
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

      <!-- 贴顶时用 order 上移而非挪动 DOM：MapLibre 需要画布容器保持既有尺寸行为 -->
      <MapTimeline
        :class="{ 'map-timeline--top': uiPrefs.map_slider_pos === 'top' }"
        :buckets="buckets"
        :range="range"
        :loading="timelineLoading"
        :granularity="granularity"
        :places="places"
        :position="uiPrefs.map_slider_pos"
        @change="onRangeChange"
        @zoom="onZoomChange"
        @place="onPlaceClick"
      />

      <!-- 三个浮层按 left/right 锚定，放进主区才不会压到侧栏（.map-main 同为 relative） -->
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
    </div>

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
      @update:kind="kind = $event"
      @update:side="patchPrefs({ map_filter_side: $event })"
      @update:slider-pos="patchPrefs({ map_slider_pos: $event })"
      @update:provider="patchPrefs({ map_default_provider: $event })"
      @update:default-zoom="patchPrefs({ map_default_zoom: $event })"
      @update:collapsed="setFilterCollapsed($event)"
      @update:marker-mode="setMarkerMode($event)"
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
        @update:kind="kind = $event"
        @update:side="patchPrefs({ map_filter_side: $event })"
        @update:slider-pos="patchPrefs({ map_slider_pos: $event })"
        @update:provider="patchPrefs({ map_default_provider: $event })"
        @update:default-zoom="patchPrefs({ map_default_zoom: $event })"
        @update:marker-mode="setMarkerMode($event)"
        @close="filterOpen = false"
      />
    </div>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
  getUiPrefs,
  putMapIconPref,
  putUiPrefs,
  searchPlaces
} from '../api/map'
import { useResponsive } from '../composables/useResponsive'
import { loadThumbUrl } from '../components/timeline/mediaLoader'
import { useMapIcon } from '../composables/useMapIcon'
import MapTimeline from '../components/map/MapTimeline.vue'
import MapItemList from '../components/map/MapItemList.vue'
import MapIconPicker from '../components/map/MapIconPicker.vue'
import MapHoverCard from '../components/map/MapHoverCard.vue'
import MapFilterSlot from '../components/map/MapFilterSlot.vue'
import MapFilterBar from '../components/map/MapFilterBar.vue' // 移动端浮层仍直接用

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

// ---- 界面偏好（Job：筛选栏 + 布局记忆）----
// 先以默认值渲染，拉到服务端偏好后再覆盖；服务端不可达时保持默认，地图照常可用
const uiPrefs = ref({
  map_slider_pos: 'bottom',
  map_filter_side: 'left',
  map_filter_collapsed: false, // Job000059：桌面筛选悬浮化收起状态
  map_marker_mode: 'icon', // Job000060：标记样式 icon|thumb
  map_default_provider: 'auto',
  map_default_zoom: null
})
const kind = ref('all') // 媒体类型过滤 all|photo|video|pano
const filterOpen = ref(false) // 移动端筛选浮层开关

// Job000060：标记样式 icon|thumb（记忆 uiPrefs.map_marker_mode，默认图标）
const markerMode = computed(() => (uiPrefs.value.map_marker_mode === 'thumb' ? 'thumb' : 'icon'))

// ---- 标记图标偏好（Job000060 抽为 composable useMapIcon）----
const { iconPref, clusterIconEl, applyIcon, loadIconPref, onIconPrefUpdate } = useMapIcon(() => map, err)

function applyMarkerMode() {
  if (!map?.getLayer) return
  map.setLayoutProperty('cluster-circles', 'visibility', markerMode.value === 'icon' ? 'visible' : 'none')
  map.setLayoutProperty('cluster-thumbs', 'visibility', markerMode.value === 'thumb' ? 'visible' : 'none')
}
watch(markerMode, () => {
  applyMarkerMode()
  // 模式切换后重下数据（icon_img 属性只在缩略图模式下注入）
  if (map) map.getSource('clusters')?.setData(toGeoJSON(clusters.value))
})

function setFilterCollapsed(v) {
  patchPrefs({ map_filter_collapsed: !!v })
}

function setMarkerMode(m) {
  patchPrefs({ map_marker_mode: m === 'thumb' ? 'thumb' : 'icon' })
}

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
let prefsSaveTimer = null // 界面偏好防抖写入（与 reloadTimer 同为手写定时器，项目内无防抖工具）
let prefDirty = false // 有尚未落到服务端的偏好改动

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

// ---- 界面偏好：账户级持久化 ----
// 偏好接口失败绝不能拦住地图渲染，故整体吞掉异常、退回默认值
async function loadUiPrefs() {
  try {
    const d = (await getUiPrefs()).data || {}
    uiPrefs.value = {
      map_slider_pos: d.map_slider_pos || 'bottom',
      map_filter_side: d.map_filter_side || 'left',
      map_filter_collapsed: d.map_filter_collapsed === true,
      map_marker_mode: d.map_marker_mode === 'thumb' ? 'thumb' : 'icon',
      map_default_provider: d.map_default_provider || 'auto',
      map_default_zoom: typeof d.map_default_zoom === 'number' ? d.map_default_zoom : null
    }
  } catch {
    // 保持默认值即可，不提示（用户没做任何操作，弹错误只会困惑）
  }
}

// 先落本地（界面立刻响应），再防抖写服务端
function patchPrefs(partial) {
  uiPrefs.value = { ...uiPrefs.value, ...partial }
  prefDirty = true
  clearTimeout(prefsSaveTimer)
  prefsSaveTimer = setTimeout(saveUiPrefs, 500)
}

async function saveUiPrefs() {
  try {
    await putUiPrefs({ ...uiPrefs.value })
    prefDirty = false
  } catch {
    // 保存失败保留本地值：界面与用户的选择保持一致，仅非阻塞提示
    // 不清 prefDirty：留给卸载时的补写重试
    err.value = '界面偏好已本地生效，服务端同步失败'
  }
}

// 媒体类型变化 → 重新查询（直方图不受 kind 影响，后端恒返四类计数）
watch(kind, () => reload())

// 侧栏出现/消失或换边会改变画布宽度，必须让 MapLibre 重算画布尺寸，否则会拉伸或留白
watch([isMobile, () => uiPrefs.value.map_filter_side], () => {
  nextTick(() => map?.resize())
})

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
  const thumb = markerMode.value === 'thumb'
  return {
    type: 'FeatureCollection',
    features: list.map((c, i) => ({
      type: 'Feature',
      id: i,
      geometry: { type: 'Point', coordinates: [c.lng, c.lat] },
      // 缩略图模式下把代表图 id 带给 symbol 层；无代表图/图标模式的簇走 coalesce 回退 cluster-icon
      properties: { count: c.count, ...(thumb && c.cover_id ? { icon_img: c.cover_id } : {}) }
    }))
  }
}

// ---- 缩略图模式（Job000060）：簇代表图按需注册为 MapLibre 图片 ----
const thumbImgState = new Map() // cover_id -> 已发起加载（去重，防重入）

function ensureThumbImage(id) {
  if (!map || !id || map.hasImage(id) || thumbImgState.has(id)) return
  thumbImgState.set(id, 1)
  // loadThumbUrl 返回 Promise（fetch blob→ObjectURL）——resolve 出真实 URL 再挂到 Image；
  // 失败（含 429/无缩略图之外的错误与 404 回退也失败的情形）注册回退为当前标记图标
  loadThumbUrl({ id }, 'sm')
    .then((url) => {
      const img = new Image()
      img.onload = () => {
        if (map) map.addImage(id, img) // addImage 触发重绘，晚到的图自动显形
      }
      img.src = url
    })
    .catch(() => {
      if (map && !map.hasImage(id) && clusterIconEl.value) map.addImage(id, clusterIconEl.value)
    })
}

let reloadSeq = 0 // 主数据通道代次守卫（同 searchReqId/hoverRequestId 范式）：慢响应后到时丢弃

async function reload() {
  if (!map) return
  const seq = ++reloadSeq
  const bbox = bboxOf()
  const zoom = map.getZoom()
  const from = range.value?.from || ''
  const to = range.value?.to || ''

  timelineLoading.value = true
  try {
    const [cs, hs, ps] = await Promise.all([
      fetchClusters(bbox, zoom, from, to, kind.value),
      // 直方图刻意不下发 kind：后端恒返四类计数，切类型时统计条不应跟着跳
      fetchHistogram(bbox, granularity.value).catch(() => []),
      fetchPlaces(bbox, from, to, 50, kind.value).catch(() => [])
    ])
    if (seq !== reloadSeq || !map) return // 已有更新的请求发起（kind 切换/框选/粒度切换等直调入口均经此守卫）
    clusters.value = cs
    buckets.value = hs
    places.value = ps
    // 缩略图模式：先为每个簇注册代表图（去重缓存），再下数据——晚到的图由 addImage 重绘带上
    if (markerMode.value === 'thumb') for (const c of cs) ensureThumbImage(c.cover_id)
    map.getSource('clusters')?.setData(toGeoJSON(cs))
    err.value = ''
  } catch (e) {
    if (seq !== reloadSeq) return
    err.value = e?.response?.data?.error?.message || '地图数据加载失败'
  } finally {
    if (seq === reloadSeq) timelineLoading.value = false
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
    items.value = await fetchItems(bbox, range.value?.from || '', range.value?.to || '', 60, kind.value)
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
  fetchItems(bbox, range.value?.from || '', range.value?.to || '', 60, kind.value)
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
  // 偏好必须在建图前拿到：默认缩放是建图参数。两者各自兜底，任一失败都不影响建图
  await Promise.all([loadIconPref(), loadUiPrefs()])

  map = new MapLibreMap({
    container: mapRef.value,
    style: rasterStyle,
    center: [116.397, 39.909],
    zoom: uiPrefs.value.map_default_zoom ?? 4,
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
        'icon-allow-overlap': true,
        visibility: markerMode.value === 'icon' ? 'visible' : 'none'
      }
    })
    // Job000060 缩略图模式：同一份簇数据按 icon_img 属性渲染代表图；
    // 加载失败的簇在 ensureThumbImages 里被注册为回退图标，属性无需变更
    map.addLayer({
      id: 'cluster-thumbs',
      type: 'symbol',
      source: 'clusters',
      layout: {
        'icon-image': ['coalesce', ['get', 'icon_img'], 'cluster-icon'],
        'icon-size': 0.2,
        'icon-anchor': 'center',
        'icon-allow-overlap': true,
        visibility: markerMode.value === 'thumb' ? 'visible' : 'none'
      }
    })

    // 加载初始图标（默认红点）
    applyIcon()

    // 点击下钻 / 展开（两种标记图层同逻辑）
    for (const layerId of ['cluster-circles', 'cluster-thumbs']) {
      map.on('click', layerId, (e) => {
        const f = e.features?.[0]
        if (!f) return
        openCluster(f.properties, f.geometry.coordinates.slice())
      })

      // 悬停预览（桌面）
      map.on('mouseenter', layerId, (e) => {
        const f = e.features?.[0]
        if (!f) return
        clearTimeout(hoverTimer)
        clearTimeout(hoverCloseTimer)
        hoverTimer = setTimeout(() => {
          showHover(f, e.originalEvent.clientX, e.originalEvent.clientY)
        }, 120)
      })
      map.on('mousemove', layerId, () => {
        map.getCanvas().style.cursor = 'pointer'
      })
      map.on('mouseleave', layerId, () => {
        clearTimeout(hoverTimer)
        map.getCanvas().style.cursor = ''
        // 不立即关闭：给用户移入卡片点选的时间
        scheduleHoverClose()
      })
    }

    if (import.meta.env.DEV) window.__map = map
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
      const fs = map.queryRenderedFeatures([t.clientX, t.clientY], { layers: ['cluster-circles', 'cluster-thumbs'] })
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
  // 500ms 防抖窗口内离开页面会静默丢掉用户最后一次选择，故卸载时补写一次；
  // 不 await：组件已在卸载，没有界面可更新，请求发出即可
  clearTimeout(prefsSaveTimer)
  if (prefDirty) saveUiPrefs()
  map?.remove()
  map = null
  reloadSeq++ // 作废卸载后在途的 reload 响应
  if (import.meta.env.DEV) window.__map = null
})
</script>

<style scoped>
/* 横向排布：左/右筛选栏 + 地图主区；靠 order 换边，DOM 保持不变 */
.map-view {
  position: relative;
  display: flex;
  flex-direction: row;
  height: 100%;
  min-height: 0;
}

/* 主区自成一列（浮层/时间轴），order 恒为 1，介于左右侧栏之间 */
.map-main {
  position: relative;
  order: 1;
  flex: 1 1 auto;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

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

/* 时间轴贴顶：order 上移（不改 DOM，MapLibre 画布容器的尺寸行为不受影响） */
.map-timeline--top {
  order: -1;
}

.map-canvas {
  flex: 1 1 auto;
  min-height: 0;
  background: var(--color-surface-hover);
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
  color: var(--color-text-disabled);
}

.ms-box:focus-within {
  border-color: var(--color-primary);
}

.ms-input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  font-size: 13px;
  color: var(--color-text-primary);
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
  background: var(--color-border);
  color: var(--color-text-secondary);
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
  color: var(--color-text-primary);
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
  color: var(--color-text-disabled);
}

.ms-hint {
  padding: 8px;
  font-size: 12px;
  color: var(--color-text-secondary);
}

.ms-hint--err {
  color: var(--color-danger);
}

.mt-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text-primary);
}

.mt-stat {
  font-size: 12px;
  color: var(--color-text-secondary);
  font-variant-numeric: tabular-nums;
}

.mt-err {
  font-size: 12px;
  color: var(--color-danger);
}

.mt-icon-btn {
  margin-left: auto;
  border: 1px solid rgba(15, 23, 42, 0.14);
  background: #fff;
  border-radius: 6px;
  padding: 3px 10px;
  font-size: 12px;
  color: var(--color-text-secondary);
  cursor: pointer;
}

.mt-icon-btn:hover {
  border-color: rgba(15, 23, 42, 0.28);
  color: var(--color-text-primary);
}

/* 顶栏第二个按钮（移动端「筛选」）不再抢 auto 外边距，与「图标」并排靠右 */
.mt-icon-btn + .mt-icon-btn {
  margin-left: 0;
}
</style>
