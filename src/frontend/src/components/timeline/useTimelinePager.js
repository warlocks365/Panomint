import { computed, reactive, ref } from 'vue'
import http from '../../api/http'
import { DEFAULT_DIMENSION, DIMENSIONS, TICK_LIMIT, isDimension } from './timelineDimensions'

// TimelineGrid 拆解（Job000088）：分页加载状态机整体自持——
// items 去重累积 / cursor 游标 / loading·finished·error 三态 / loadSeq 代次守卫
//（reset() 已递增，旧筛选的在途响应落地即作废）。
// histogram 由本模块请求，消费侧（日期滑块 / 日历）只读。
const PAGE_SIZE = 60

export function useTimelinePager(props) {
  const items = reactive([])
  const seen = new Set()
  const loading = ref(false)
  const finished = ref(false)
  const error = ref('')
  // 三档直方图**一次性全取**。这是 AC-04 的硬约束：切档 must 不发起任何网络请求。
  // 若改成「切到哪档才取哪档」，每次切档都会多一个请求，直接违反该验收项。
  const histograms = reactive({ year: [], month: [], day: [] })
  // day 档被截断时置位：消费侧降级为等宽刻度，不按 count 定宽
  const truncated = reactive({ year: false, month: false, day: false })
  // taken_at IS NULL 的媒体数（后端归入 'unknown' 桶）。如实报出而非静默丢弃（AC-11）
  const unknownCount = ref(0)
  const dimension = ref(DEFAULT_DIMENSION)

  let nextCursor = null
  let loadSeq = 0
  let inflight = null

  /** 当前档位的直方图（切档只换引用，不发请求） */
  const histogram = computed(() => histograms[dimension.value] || [])
  const histogramTruncated = computed(() => !!truncated[dimension.value])

  async function loadMore() {
    if (loading.value || finished.value) return
    const seq = loadSeq
    loading.value = true
    error.value = ''
    inflight = (async () => {
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
    })()
    await inflight
  }

  /** 等待在途分页请求落地。seekTo 用它替代轮询，避免 abort 在途请求（AC-14）。 */
  function whenSettled() {
    return inflight || Promise.resolve()
  }

  /**
   * 取某一档直方图。桶数超 TICK_LIMIT 时截断并置 truncated，
   * 避免 day 档跨十年可达上万个 DOM 节点拖垮滚动（ADR-001 记录的负面代价）。
   *
   * `space` 必须显式携带：漏传会回落到缺省作用域分支，是已登记的泄漏模式（K2）。
   */
  async function loadHistogram(dim) {
    if (!isDimension(dim)) return
    try {
      const { data } = await http.get('/media/date-histogram', {
        params: { granularity: dim, space: 'personal' }
      })
      const raw = Array.isArray(data) ? data : Array.isArray(data?.buckets) ? data.buckets : []
      // 'unknown'（taken_at IS NULL）不是日期字符串：剔除后才可参与键比较
      const list = raw.filter((b) => b && typeof b.bucket === 'string' && b.bucket !== 'unknown')
      if (dim === 'day') {
        unknownCount.value = raw
          .filter((b) => b && b.bucket === 'unknown')
          .reduce((n, b) => n + (b.count || 0), 0)
      }
      truncated[dim] = list.length > TICK_LIMIT
      histograms[dim] = truncated[dim] ? list.slice(0, TICK_LIMIT) : list
    } catch {
      histograms[dim] = [] // 接口未就绪或失败：隐藏滑块，不影响主时间流
      truncated[dim] = false
    }
  }

  /**
   * 一次性取齐三档直方图。挂载时调一次即可，之后切档零请求（AC-04）。
   * 三档并发而非串行：彼此无依赖，串行会让首屏多等两个 RTT。
   */
  function loadHistograms() {
    return Promise.all(DIMENSIONS.map((d) => loadHistogram(d)))
  }

  /** 切档：只改computed 的取数来源，**不发任何请求**（AC-04 的实现点） */
  function setDimension(next) {
    if (isDimension(next)) dimension.value = next
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
  }

  function removeById(id) {
    const i = items.findIndex((m) => m.id === id)
    if (i >= 0) {
      items.splice(i, 1)
      seen.delete(id)
    }
  }

  return {
    items,
    loading,
    finished,
    error,
    dimension,
    histogram,
    histogramTruncated,
    histograms,
    unknownCount,
    loadMore,
    loadHistogram,
    loadHistograms,
    setDimension,
    whenSettled,
    reset,
    removeById
  }
}