// 空间页面数据编排（Job000093：SpacesView 拆页面级 composable，与 090-092 同范式）。
// 职责：空间信息（/spaces）与个人/共享媒体分页加载（space 过滤+游标翻页）、
// 空间切换语义（共享为空不拉媒体）、加载更多累积。
// 视图职责留宿主：openItem 跳转 player（router 属视图）、卡片/空态/网格布局。
import { onMounted, ref } from 'vue'
import http from '../../api/http'

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

  function switchSpace(space) {
    if (activeSpace.value === space) return
    activeSpace.value = space
    if (space === 'personal' || shared.value.length > 0) {
      loadMedia()
    }
  }

  onMounted(async () => {
    try {
      const res = await http.get('/spaces')
      personal.value = res.data.personal
      shared.value = res.data.shared || []
    } catch (e) {
      loadError.value = '空间信息加载失败：' + (e.response?.data?.error?.message || '网络错误')
    }
    await loadMedia()
  })

  return {
    personal, shared, activeSpace, loadError,
    mediaItems, mediaTotal, nextCursor, mediaLoading, loadingMore, sharedSpaceName,
    formatBytes, loadMedia, loadMore, switchSpace
  }
}
