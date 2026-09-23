// 地图主数据通道（Job000096 拆自 MapView，语义原样迁移）：视野内簇/直方图/地点三查 + 代表图注册 + 图层显隐。
// reloadSeq 代次守卫（同 searchReqId/hoverRequestId 范式）：慢响应后到时丢弃；
// 直方图刻意不下发 kind（后端恒返四类计数，切类型时统计条不应跟着跳）；
// 缩略图模式先注册代表图再下数据——晚到的图由 addImage 重绘带上；
// 加载失败（429/网络/缩略图缺失）注册同步默认红点，视觉上仍是标记、绝不「消失」。
import { computed, ref } from 'vue'
import { fetchClusters, fetchHistogram, fetchPlaces } from '../../api/map'
import { loadThumbUrl } from '../../components/timeline/mediaLoader'
import { makeDefaultIconImageData } from '../../composables/useMapIcon'

export function useMapData({ getMap, getKind, getMarkerMode, errRef }) {
  const clusters = ref([])
  const buckets = ref([])
  const places = ref([])
  const range = ref(null) // { from: ISO, to: ISO } | null
  const granularity = ref('month') // year|month|day
  const timelineLoading = ref(false)

  const clusterCount = computed(() => clusters.value.length)
  const pointCount = computed(() => clusters.value.reduce((s, c) => s + (c.count || 0), 0))

  function bboxOf() {
    const b = getMap().getBounds()
    return {
      minLng: b.getWest(),
      minLat: b.getSouth(),
      maxLng: b.getEast(),
      maxLat: b.getNorth()
    }
  }

  function toGeoJSON(list) {
    const thumb = getMarkerMode() === 'thumb'
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
    const map = getMap()
    if (!map || !id || map.hasImage(id) || thumbImgState.has(id)) return
    thumbImgState.set(id, 1)
    // loadThumbUrl 返回 Promise（fetch blob→ObjectURL）——resolve 出真实 URL 再挂到 Image
    loadThumbUrl({ id }, 'sm')
      .then((url) => {
        const img = new Image()
        img.onload = () => {
          if (map) map.addImage(id, img) // addImage 触发重绘，晚到的图自动显形
        }
        img.src = url
      })
      .catch(() => {
        // 加载失败（429/网络/缩略图缺失）→ 注册同步默认红点，视觉上仍是标记、绝不「消失」
        if (map && !map.hasImage(id)) map.addImage(id, makeDefaultIconImageData())
      })
  }

  let reloadSeq = 0 // 主数据通道代次守卫：kind 切换/框选/粒度切换等直调入口均经此守卫
  let reloadTimer = null

  async function reload() {
    const map = getMap()
    if (!map) return
    const seq = ++reloadSeq
    const bbox = bboxOf()
    const zoom = map.getZoom()
    const from = range.value?.from || ''
    const to = range.value?.to || ''

    timelineLoading.value = true
    try {
      const [cs, hs, ps] = await Promise.all([
        fetchClusters(bbox, zoom, from, to, getKind()),
        // 直方图刻意不下发 kind：后端恒返四类计数，切类型时统计条不应跟着跳
        fetchHistogram(bbox, granularity.value).catch(() => []),
        fetchPlaces(bbox, from, to, 50, getKind()).catch(() => [])
      ])
      if (seq !== reloadSeq || getMap() !== map) return // 已有更新的请求发起，或地图已销毁
      clusters.value = cs
      buckets.value = hs
      places.value = ps
      // 缩略图模式：先为每个簇注册代表图（去重缓存），再下数据——晚到的图由 addImage 重绘带上
      if (getMarkerMode() === 'thumb') for (const c of cs) ensureThumbImage(c.cover_id)
      map.getSource('clusters')?.setData(toGeoJSON(cs))
      errRef.value = ''
    } catch (e) {
      if (seq !== reloadSeq) return
      errRef.value = e?.response?.data?.error?.message || '地图数据加载失败'
    } finally {
      if (seq === reloadSeq) timelineLoading.value = false
    }
  }

  function scheduleReload() {
    clearTimeout(reloadTimer)
    reloadTimer = setTimeout(reload, 250)
  }

  function setRange(next) {
    range.value = next
  }

  function onZoomChange(next) {
    granularity.value = next
    reload() // 重取直方图（新粒度 + 四类计数）
  }

  function applyMarkerMode() {
    const map = getMap()
    if (!map?.getLayer) return
    map.setLayoutProperty('cluster-circles', 'visibility', getMarkerMode() === 'icon' ? 'visible' : 'none')
    map.setLayoutProperty('cluster-thumbs', 'visibility', getMarkerMode() === 'thumb' ? 'visible' : 'none')
  }

  function destroy() {
    clearTimeout(reloadTimer)
    reloadSeq++ // 作废卸载后在途的 reload 响应
  }

  return {
    clusters,
    buckets,
    places,
    range,
    granularity,
    timelineLoading,
    clusterCount,
    pointCount,
    reload,
    scheduleReload,
    setRange,
    onZoomChange,
    toGeoJSON,
    ensureThumbImage,
    applyMarkerMode,
    destroy
  }
}
