// 地图内置地名搜索状态机（Job000096 拆自 MapView，语义原样迁移）。
// 回车 → /map/search → 候选下拉 → flyTo 定位（不跳 /search）；失败只在地图内提示。
// searchReqId 代次守卫：连续回车时只认最后一次响应。
// 点面板外关闭：以 .map-search 根类为界（与 MapSearchBox 根节点的类契约，document 级监听由宿主接线）。
import { ref } from 'vue'
import { searchPlaces } from '../../api/map'

export function useMapSearch({ getMap, onPicked }) {
  const q = ref('')
  const candidates = ref([])
  const searchOpen = ref(false)
  const searching = ref(false)
  const searchErr = ref('')
  const searchMsg = ref('') // 「未找到该地点」等空结果提示
  let searchReqId = 0

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
    const map = getMap()
    if (!map || typeof c.lon !== 'number' || typeof c.lat !== 'number') return
    map.flyTo({
      center: [c.lon, c.lat],
      zoom: Math.max(14, map.getZoom()),
      duration: 800
    })
    onPicked()
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

  // 点击搜索面板以外区域关闭下拉（宿主 onMounted 挂 document 监听、onBeforeUnmount 移除）
  function onSearchOutside(e) {
    if (!e.target.closest('.map-search')) closeSearch()
  }

  return {
    q,
    candidates,
    searchOpen,
    searching,
    searchErr,
    searchMsg,
    runSearch,
    pickCandidate,
    closeSearch,
    clearSearch,
    onSearchOutside
  }
}
