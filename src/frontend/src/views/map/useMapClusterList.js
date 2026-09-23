// 「此处 N 项」列表面板状态机（Job000096 拆自 MapView，语义原样迁移；Job000080 批量语义）。
// openCluster zoom 门禁：count>1 且 zoom<12 只 flyTo zoom+2 不开列表；列表项上限 60。
// lastListBBox 记忆当前列表范围：批量操作完成后按同一 bbox 重取（列表项已因操作变化）。
import { ref } from 'vue'
import { fetchItems } from '../../api/map'

export function useMapClusterList({ getMap, getRange, getKind, closeHover, onDataChanged, errRef }) {
  const items = ref([])
  const listOpen = ref(false)
  const listLoading = ref(false)

  let lastListBBox = null

  async function openCluster(props, lngLat) {
    const map = getMap()
    if (!map) return
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
    listOpen.value = true
    closeHover()
    await loadList(bbox)
  }

  async function loadList(bbox) {
    lastListBBox = bbox
    listLoading.value = true
    items.value = []
    try {
      items.value = await fetchItems(bbox, getRange()?.from || '', getRange()?.to || '', 60, getKind())
    } catch (e) {
      errRef.value = '条目加载失败'
    } finally {
      listLoading.value = false
    }
  }

  // Job000080 批量操作完成 → 重取列表（同 bbox）+ 刷新簇/计数
  function onListChanged() {
    onDataChanged()
    if (listOpen.value && lastListBBox) loadList(lastListBBox)
  }

  function closeList() {
    listOpen.value = false
    items.value = []
    lastListBBox = null
  }

  return { items, listOpen, listLoading, openCluster, onListChanged, closeList }
}
