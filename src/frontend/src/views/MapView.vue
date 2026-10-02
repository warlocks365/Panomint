<template>
  <div class="map-view" :class="{ 'map-view--mobile': isMobile }">
    <!-- 地图主区：浮层与时间轴都挂在这里（.map-main 为 relative，绝对定位于其上的浮层仍能正确锚定） -->
    <div class="map-main">
      <div ref="mapRef" class="map-canvas"></div>

      <!-- 浮层容器：顶栏 + 地名搜索框纵向排列；容器自身不吃鼠标事件，地图交互不受遮挡 -->
      <div class="map-float">
        <MapTopbar
          :cluster-count="data.clusterCount.value"
          :point-count="data.pointCount.value"
          :err="err"
          :is-mobile="isMobile"
          @toggle-icons="iconPickerOpen = !iconPickerOpen"
          @toggle-filter="prefs.filterOpen.value = !prefs.filterOpen.value"
        />
        <MapSearchBox
          v-model:q="search.q.value"
          :class="{ 'map-search--mobile': isMobile }"
          :candidates="search.candidates.value"
          :open="search.searchOpen.value"
          :searching="search.searching.value"
          :search-err="search.searchErr.value"
          :search-msg="search.searchMsg.value"
          @enter="search.runSearch"
          @pick="search.pickCandidate"
          @clear="search.clearSearch"
          @close="search.closeSearch"
        />
      </div>

      <!-- 贴顶时用 order 上移而非挪动 DOM：MapLibre 需要画布容器保持既有尺寸行为 -->
      <MapTimeline
        :class="{ 'map-timeline--top': prefs.uiPrefs.value.map_slider_pos === 'top' }"
        :buckets="data.buckets.value"
        :range="data.range.value"
        :loading="data.timelineLoading.value"
        :granularity="data.granularity.value"
        :places="data.places.value"
        :position="prefs.uiPrefs.value.map_slider_pos"
        @change="onRangeChange"
        @zoom="data.onZoomChange"
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
        v-if="list.listOpen.value"
        :items="list.items.value"
        :loading="list.listLoading.value"
        selectable
        @close="list.closeList"
        @open="openItem"
        @changed="list.onListChanged"
      />

      <MapHoverCard
        v-if="hover.hoverOpen.value"
        :items="hover.hoverItems.value"
        :loading="hover.hoverLoading.value"
        :place="hover.hoverPlace.value"
        :pos="hover.hoverPos.value"
        @open="openItem"
        @enter="hover.cancelHoverClose"
        @leave="hover.scheduleHoverClose"
      />
    </div>

    <MapFilterDock
      :kind="prefs.kind.value"
      :ui-prefs="prefs.uiPrefs.value"
      :marker-mode="prefs.markerMode.value"
      :is-mobile="isMobile"
      :filter-open="prefs.filterOpen.value"
      @update:kind="prefs.kind.value = $event"
      @patch="prefs.patchPrefs"
      @update:collapsed="prefs.setFilterCollapsed"
      @update:marker-mode="prefs.setMarkerMode"
      @close="prefs.filterOpen.value = false"
    />
  </div>
</template>

<script setup>
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { useResponsive } from '../composables/useResponsive'
import { useMapIcon } from '../composables/useMapIcon'
import { useMapEngine } from './map/useMapEngine'
import { useMapData } from './map/useMapData'
import { useMapHover } from './map/useMapHover'
import { useMapSearch } from './map/useMapSearch'
import { useMapUiPrefs } from './map/useMapUiPrefs'
import { useMapClusterList } from './map/useMapClusterList'
import MapTimeline from '../components/map/MapTimeline.vue'
import MapItemList from '../components/map/MapItemList.vue'
import MapIconPicker from '../components/map/MapIconPicker.vue'
import MapHoverCard from '../components/map/MapHoverCard.vue'
import MapFilterDock from '../components/map/MapFilterDock.vue'
import MapTopbar from '../components/map/MapTopbar.vue'
import MapSearchBox from '../components/map/MapSearchBox.vue'

// 地图模式（Job000009 优化）：全屏地图 + 时间轴缩放滑块 + 图标可配置 + 悬停预览。
// 引擎型视图拆解（Job000096）：MapLibre 引擎生命周期自持于 useMapEngine，业务状态机五域分离
// （搜索/偏好/主数据/悬停/列表面板），宿主只做装配与编排——模板经 .value 解包直连各 composable。
const router = useRouter()
const mapRef = ref(null)
const { isMobile } = useResponsive()
const err = ref('') // 页级错误通道：顶栏展示，各状态机注入写入

const prefs = useMapUiPrefs({ errRef: err })
const engine = useMapEngine({
  getDefaultZoom: () => prefs.uiPrefs.value.map_default_zoom,
  getMarkerMode: () => prefs.markerMode.value,
  onLoad: () => applyIcon(),
  onReady: () => data.reload(),
  onClusterClick: (props, lngLat) => list.openCluster(props, lngLat),
  onHoverEnter: (f, x, y) => hover.delayedShow(f, x, y),
  onHoverLeave: () => {
    hover.clearDelayedShow()
    hover.scheduleHoverClose()
  },
  onTouchShow: (f, x, y) => hover.showHover(f, x, y),
  onTouchClear: () => {
    hover.clearDelayedShow()
    hover.closeHover()
  },
  onTouchClose: () => hover.scheduleHoverClose(),
  onMoveEnd: () => data.scheduleReload(),
  probe: () => ({
    has: (id) => !!(engine.getMap() && engine.getMap().hasImage(id)),
    vis: (l) => {
      const m = engine.getMap()
      return m?.getLayer(l) ? m.getLayoutProperty(l, 'visibility') : null
    },
    openList: () => {
      const c = data.clusters.value[0]
      if (!c) return false
      list.openCluster({ count: 1 }, [c.lng, c.lat])
      return true
    }
  })
})
const data = useMapData({
  getMap: () => engine.getMap(),
  getKind: () => prefs.kind.value,
  getMarkerMode: () => prefs.markerMode.value,
  errRef: err
})
const hover = useMapHover({
  getMap: () => engine.getMap(),
  getRange: () => data.range.value,
  getKind: () => prefs.kind.value
})
const list = useMapClusterList({
  getMap: () => engine.getMap(),
  getRange: () => data.range.value,
  getKind: () => prefs.kind.value,
  closeHover: hover.closeHover,
  onDataChanged: data.reload,
  errRef: err
})
const search = useMapSearch({ getMap: () => engine.getMap(), onPicked: () => data.scheduleReload() })
const { iconPref, applyIcon, loadIconPref, onIconPrefUpdate } = useMapIcon(() => engine.getMap(), err)

const iconPickerOpen = ref(false)

// 媒体类型变化 → 重新查询（直方图不受 kind 影响，后端恒返四类计数）
watch(prefs.kind, () => data.reload())

// 标记样式切换：图层显隐 + 补注册代表图（reload 才走 ensureThumbImage；仅 setData 不会触发图片请求）+ 重下数据
watch(prefs.markerMode, () => {
  data.applyMarkerMode()
  if (prefs.markerMode.value === 'thumb') for (const c of data.clusters.value) data.ensureThumbImage(c.cover_id)
  engine.getMap()?.getSource('clusters')?.setData(data.toGeoJSON(data.clusters.value))
})

// 侧栏出现/消失或换边会改变画布宽度，必须让 MapLibre 重算画布尺寸，否则会拉伸或留白
watch([isMobile, () => prefs.uiPrefs.value.map_filter_side], () => {
  nextTick(() => engine.getMap()?.resize())
})

// 时间轴框选变化：关列表面板（旧范围结果失效）并重查主数据
function onRangeChange(next) {
  data.setRange(next)
  list.closeList()
  data.reload()
}

// 点击位置标签 → 地图飞过去定位（放大到街道级，突出该地点）
function onPlaceClick(place) {
  const map = engine.getMap()
  if (!map || place.lng === undefined || place.lat === undefined) return
  hover.closeHover()
  map.flyTo({
    center: [place.lng, place.lat],
    zoom: Math.max(14, map.getZoom() + 6),
    duration: 800
  })
}

function openItem(it) {
  router.push(`/player/${it.id}`)
}

onMounted(async () => {
  // 点击地图（或页面其他区域）关闭候选下拉
  document.addEventListener('click', search.onSearchOutside)
  // 偏好必须在建图前拿到：默认缩放是建图参数。两者各自兜底，任一失败都不影响建图
  await Promise.all([loadIconPref(), prefs.loadUiPrefs()])
  engine.init(mapRef.value)
})

onBeforeUnmount(() => {
  document.removeEventListener('click', search.onSearchOutside)
  // 顺序与语义承原实现：偏好补写 → 销毁引擎（map=null）→ 作废在途响应
  prefs.flushOnUnmount()
  engine.destroy()
  data.destroy()
  hover.destroy()
  prefs.destroy()
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

/* 时间轴贴顶：order 上移（不改 DOM，MapLibre 画布容器的尺寸行为不受影响） */
.map-timeline--top {
  order: -1;
}

.map-canvas {
  flex: 1 1 auto;
  min-height: 0;
  background: var(--color-surface-hover);
  /* 瓦片 Morandi 调和（DESIGN.md MapView 深化）：压饱和入晨雾色系，不触碰地图引擎 */
  filter: saturate(0.42) brightness(1.04);
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
</style>
