// 悬停/长按预览状态机（Job000096 拆自 MapView，语义原样迁移）。
// hover bbox 半边长 = 当前 zoom 的聚合网格尺寸 GridSize(zoom)=180/2^zoom（与后端 Clusters 的
// ST_SnapToGrid 同网格，质心为网格中心）——用半边长=整个网格而非 GridSize/2（只覆盖 1/4，统计不准）。
// hoverRequestId 代次守卫：过期响应丢弃；mouseenter 120ms 延迟触发，mouseleave 250ms 延迟关闭
// （给用户移入卡片点选的时间，移入卡片则取消关闭）。
import { ref } from 'vue'
import { fetchItems } from '../../api/map'

export function useMapHover({ getMap, getRange, getKind }) {
  const hoverOpen = ref(false)
  const hoverItems = ref([])
  const hoverLoading = ref(false)
  const hoverPlace = ref('')
  const hoverPos = ref(null)

  let hoverTimer = null // mouseenter 120ms 触发计时
  let hoverCloseTimer = null // mouseleave 250ms 关闭计时
  let hoverRequestId = 0

  // 与后端 GridSize 一致的网格边长（度）：z=zoom → 180/2^zoom
  function gridSizeAt(zoom) {
    let g = 180 / Math.pow(2, zoom)
    if (g < 0.0005) g = 0.0005
    return g
  }

  function showHover(feature, clientX, clientY) {
    const map = getMap()
    if (!map) return
    const coord = feature.geometry.coordinates.slice()
    // 用当前 zoom 的聚合网格尺寸做 bbox，命中该簇聚合的所有媒体
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
    fetchItems(bbox, getRange()?.from || '', getRange()?.to || '', 60, getKind())
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

  function closeHover() {
    hoverOpen.value = false
    hoverItems.value = []
    hoverPos.value = null
  }

  // 鼠标移开地图簇点后延迟关闭（给用户移入卡片的时间）；移入卡片则取消关闭
  function scheduleHoverClose() {
    clearTimeout(hoverCloseTimer)
    hoverCloseTimer = setTimeout(() => closeHover(), 250)
  }

  function cancelHoverClose() {
    clearTimeout(hoverCloseTimer) // 移入卡片，取消关闭
  }

  // mouseenter：120ms 延迟触发（快速划过不闪开卡片），同时取消未执行的关闭
  function delayedShow(feature, x, y) {
    clearTimeout(hoverTimer)
    cancelHoverClose()
    hoverTimer = setTimeout(() => showHover(feature, x, y), 120)
  }

  function clearDelayedShow() {
    clearTimeout(hoverTimer)
  }

  function destroy() {
    clearTimeout(hoverTimer)
    clearTimeout(hoverCloseTimer)
    hoverRequestId++ // 作废卸载后在途的 hover 响应
  }

  return {
    hoverOpen,
    hoverItems,
    hoverLoading,
    hoverPlace,
    hoverPos,
    showHover,
    closeHover,
    scheduleHoverClose,
    cancelHoverClose,
    delayedShow,
    clearDelayedShow,
    destroy
  }
}
