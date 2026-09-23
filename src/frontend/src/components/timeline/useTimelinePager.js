import { reactive, ref } from 'vue'
import http from '../../api/http'

// TimelineGrid 拆解（Job000088）：分页加载状态机整体自持——
// items 去重累积 / cursor 游标 / loading·finished·error 三态 / loadSeq 代次守卫
//（reset() 递增，旧筛选的在途响应落地即作废）。
// histogram 由本模块请求（reset 联动重载），消费侧（日期滑块）只读。
const PAGE_SIZE = 60

export function useTimelinePager(props) {
  const items = reactive([])
  const seen = new Set()
  const loading = ref(false)
  const finished = ref(false)
  const error = ref('')
  const histogram = ref([])
  let nextCursor = null
  let loadSeq = 0

  async function loadMore() {
    if (loading.value || finished.value) return
    const seq = loadSeq
    loading.value = true
    error.value = ''
    try {
      const params = { view: 'all', limit: PAGE_SIZE }
      if (props.type) params.type = props.type
      if (props.favorites) params.favorites = 'true'
      if (props.place) params.place = props.place
      if (nextCursor) params.cursor = nextCursor
      const { data } = await http.get('/media', { params })
      if (seq !== loadSeq) return // reset() 已递增代次：旧筛选的响应丢弃
      const list = Array.isArray(data.items) ? data.items : []
      for (const m of list) {
        if (!seen.has(m.id)) {
          seen.add(m.id)
          items.push(m)
        }
      }
      nextCursor = data.next_cursor || null
      if (!nextCursor || !list.length) finished.value = true
    } catch (e) {
      if (seq !== loadSeq) return
      error.value = e.response
        ? `HTTP ${e.response.status} ${e.response.data?.error?.message || ''}`
        : '网络不可达'
    } finally {
      if (seq === loadSeq) loading.value = false
    }
  }

  async function loadHistogram() {
    try {
      const { data } = await http.get('/media/date-histogram', { params: { granularity: 'month' } })
      histogram.value = Array.isArray(data) ? data : (Array.isArray(data?.buckets) ? data.buckets : [])
    } catch {
      histogram.value = [] // 接口未就绪或失败：隐藏滑块，不影响主时间流
    }
  }

  function reset() {
    loadSeq++ // 作废在途响应；同时放行 loading 守卫，让新筛选的首页请求立刻可发
    loading.value = false
    items.splice(0, items.length)
    seen.clear()
    nextCursor = null
    finished.value = false
    error.value = ''
    loadMore()
    loadHistogram()
  }

  function removeById(id) {
    const i = items.findIndex((m) => m.id === id)
    if (i >= 0) {
      items.splice(i, 1)
      seen.delete(id)
    }
  }

  return { items, loading, finished, error, histogram, loadMore, loadHistogram, reset, removeById }
}
