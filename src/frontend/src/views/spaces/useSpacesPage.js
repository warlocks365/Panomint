// 空间页面数据编排（Job000093：SpacesView 拆页面级 composable，与 090-092 同范式；
// Job000101 增个人空间按相册分组视图）。
// 职责：空间信息（/spaces）、个人/共享媒体分页加载（space 过滤+游标翻页）、
// 空间切换语义（共享为空不拉媒体）、加载更多累积、按相册分组视图（/albums/groups +
// 账户级偏好 spaces_group_by_album 持久化，与地图侧同契约同端点）。
// 视图职责留宿主：openItem 跳转 player（router 属视图）、卡片/空态/网格布局、视图切换钮。
import { computed, onMounted, ref } from 'vue'
import http from '../../api/http'
import { getUiPrefs, putUiPrefs } from '../../api/map'

export function useSpacesPage() {
  const personal = ref(null)
  const shared = ref([])
  const activeSpace = ref('personal')
  const loadError = ref('')

  const mediaItems = ref([])
  const mediaTotal = ref(0)
  const nextCursor = ref('')
  const mediaLoading = ref(false)
  const loadingMore = ref(false)
  const sharedSpaceName = ref('共享空间媒体')

  // ---- Job000101 按相册分组视图（仅个人空间）----
  const groups = ref([]) // GET /albums/groups → GroupSummary[]
  const ungrouped = ref(null) // GET /albums/groups → UngroupedBucket
  const groupsLoading = ref(false)

  // 账户级 UI 偏好全量副本（PUT /user/ui-prefs 是整行替换语义：缺键会把其他模块的偏好清回默认，
  // 故与地图侧同范式——进场 GET 全量、改动先落本地、防抖 PUT 全量；本页只消费/改写 spaces_group_by_album）。
  const uiPrefs = ref({
    map_slider_pos: 'bottom',
    map_filter_side: 'left',
    map_filter_collapsed: false,
    map_marker_mode: 'icon',
    map_default_provider: 'auto',
    map_default_zoom: null,
    spaces_group_by_album: false // false=时间轴平铺（默认）/true=按相册分组
  })
  const groupByAlbum = computed(() => uiPrefs.value.spaces_group_by_album === true)

  function formatBytes(n) {
    if (!n) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    let v = n
    let i = 0
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024
      i++
    }
    return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i]
  }

  async function fetchMedia(cursor) {
    const params = { space: activeSpace.value, limit: 60 }
    if (cursor) params.cursor = cursor
    const res = await http.get('/media', { params })
    return res.data
  }

  async function loadMedia() {
    mediaLoading.value = true
    mediaItems.value = []
    nextCursor.value = ''
    try {
      const data = await fetchMedia('')
      mediaItems.value = data.items || []
      mediaTotal.value = data.total ?? mediaItems.value.length
      nextCursor.value = data.next_cursor || ''
    } catch (e) {
      loadError.value = '媒体列表加载失败：' + (e.response?.data?.error?.message || '网络错误')
    } finally {
      mediaLoading.value = false
    }
  }

  async function loadMore() {
    if (!nextCursor.value) return
    loadingMore.value = true
    try {
      const data = await fetchMedia(nextCursor.value)
      mediaItems.value = mediaItems.value.concat(data.items || [])
      nextCursor.value = data.next_cursor || ''
    } finally {
      loadingMore.value = false
    }
  }

  // ---- Job000101 分组视图数据 ----

  async function loadGroups() {
    groupsLoading.value = true
    try {
      const res = await http.get('/albums/groups')
      groups.value = res.data.groups || []
      ungrouped.value = res.data.ungrouped || null
    } catch (e) {
      loadError.value = '相册分组加载失败：' + (e.response?.data?.error?.message || '网络错误')
    } finally {
      groupsLoading.value = false
    }
  }

  // 当前空间+视图模式下应加载的数据源（切空间/切视图统一走这里）
  function loadActiveFeed() {
    if (activeSpace.value === 'personal' && groupByAlbum.value) {
      loadGroups()
    } else if (activeSpace.value === 'personal' || shared.value.length > 0) {
      loadMedia()
    }
  }

  function switchSpace(space) {
    if (activeSpace.value === space) return
    activeSpace.value = space
    loadActiveFeed()
  }

  // ---- 视图模式切换 + 偏好持久化（先落本地立刻响应，500ms 防抖写服务端；失败保留本地值）----

  let prefsSaveTimer = null
  let prefDirty = false

  async function savePrefs() {
    try {
      await putUiPrefs({ ...uiPrefs.value })
      prefDirty = false
    } catch {
      // 保留本地值；prefDirty 不清，卸载时补写重试
      loadError.value = '视图偏好已本地生效，服务端同步失败'
    }
  }

  function patchPrefs(partial) {
    uiPrefs.value = { ...uiPrefs.value, ...partial }
    prefDirty = true
    clearTimeout(prefsSaveTimer)
    prefsSaveTimer = setTimeout(savePrefs, 500)
  }

  function setGroupByAlbum(v) {
    if (groupByAlbum.value === !!v) return
    patchPrefs({ spaces_group_by_album: !!v })
    // 立刻按新视图加载数据源（切回时间轴补拉媒体，避免空屏）
    loadActiveFeed()
  }

  async function loadPrefs() {
    try {
      const d = (await getUiPrefs()).data || {}
      uiPrefs.value = {
        map_slider_pos: d.map_slider_pos || 'bottom',
        map_filter_side: d.map_filter_side || 'left',
        map_filter_collapsed: d.map_filter_collapsed === true,
        map_marker_mode: d.map_marker_mode === 'thumb' ? 'thumb' : 'icon',
        map_default_provider: d.map_default_provider || 'auto',
        map_default_zoom: typeof d.map_default_zoom === 'number' ? d.map_default_zoom : null,
        spaces_group_by_album: d.spaces_group_by_album === true
      }
    } catch {
      // 保持默认（时间轴视图）；偏好失败绝不拦住页面渲染
    }
  }

  function flushOnUnmount() {
    clearTimeout(prefsSaveTimer)
    if (prefDirty) savePrefs()
  }

  onMounted(async () => {
    try {
      const res = await http.get('/spaces')
      personal.value = res.data.personal
      shared.value = res.data.shared || []
    } catch (e) {
      loadError.value = '空间信息加载失败：' + (e.response?.data?.error?.message || '网络错误')
    }
    await loadPrefs()
    loadActiveFeed()
  })

  return {
    personal, shared, activeSpace, loadError,
    mediaItems, mediaTotal, nextCursor, mediaLoading, loadingMore, sharedSpaceName,
    groupByAlbum, groups, ungrouped, groupsLoading,
    formatBytes, loadMedia, loadMore, switchSpace,
    setGroupByAlbum, loadGroups, flushOnUnmount
  }
}
