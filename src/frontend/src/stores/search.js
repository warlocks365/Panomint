import { defineStore } from 'pinia'
import { searchMedia } from '../api/search'

const PAGE_SIZE = 60

function emptyFilters() {
  return { type: '', favorites: false, tag: '', date_after: '', date_before: '', place: '', person: '' }
}

export const useSearchStore = defineStore('search', {
  state: () => ({
    query: '',
    filters: emptyFilters(),
    results: [],
    cursor: null,
    total: 0,
    loading: false,
    error: '',
    placeFallback: null, // { mode:'geo_radius', center:{lon,lat}, radius_m }
    searched: false, // 是否已发起过至少一次搜索
    reqSeq: 0 // 代次守卫：run()/clearAll() 递增，在途旧查询响应落地时作废
  }),
  getters: {
    // 已选条件数量（用于移动端「筛选」按钮角标）
    activeFilterCount(state) {
      const f = state.filters
      let n = 0
      if (f.type) n++
      if (f.favorites) n++
      if (f.tag) n++
      if (f.place) n++
      if (f.date_after || f.date_before) n++
      if (f.person) n++
      return n
    },
    hasMore: (state) => !!state.cursor,
    // 已选条件 chips 列表：{ key, label }
    chips(state) {
      const f = state.filters
      const out = []
      if (f.type) {
        const map = { photo: '照片', video: '视频', '360': '360' }
        out.push({ key: 'type', label: `类型：${map[f.type] || f.type}` })
      }
      if (f.favorites) out.push({ key: 'favorites', label: '仅收藏' })
      if (f.tag) out.push({ key: 'tag', label: `标签：${f.tag}` })
      if (f.date_after || f.date_before) {
        out.push({
          key: 'date',
          label: `日期：${f.date_after || '…'} ~ ${f.date_before || '…'}`
        })
      }
      if (f.place) out.push({ key: 'place', label: `拍摄地：${f.place}` })
      if (f.person) out.push({ key: 'person', label: '人物' })
      return out
    }
  },
  actions: {
    buildParams() {
      const params = { limit: PAGE_SIZE }
      if (this.query) params.q = this.query
      const f = this.filters
      if (f.type) params.type = f.type
      if (f.favorites) params.favorites = 'true'
      if (f.tag) params.tag = f.tag
      if (f.date_after) params.date_after = f.date_after
      if (f.date_before) params.date_before = f.date_before
      if (f.place) params.place = f.place
      if (f.person) params.person = f.person // 契约 §8 GET /search?person=（人物页跳转入口）
      return params
    },
    // 全新搜索：清空结果后拉第一页
    async run() {
      this.reqSeq++ // 作废在途旧查询；同时放行 loading 守卫，新首页请求不被旧在途请求挡死
      this.loading = false
      this.searched = true
      this.results = []
      this.cursor = null
      this.total = 0
      this.error = ''
      this.placeFallback = null
      await this.loadMore()
    },
    async loadMore() {
      if (this.loading || (this.searched && !this.cursor && this.results.length)) return
      const seq = this.reqSeq
      this.loading = true
      this.error = ''
      try {
        const params = this.buildParams()
        if (this.cursor) params.cursor = this.cursor
        const { data } = await searchMedia(params)
        if (seq !== this.reqSeq) return // run()/clearAll() 已递增代次：旧查询的响应丢弃
        // items 可能带 score（OR+score 契约），前端不解析；next_cursor 为不透明三元组编码，原样回传
        const list = Array.isArray(data.items) ? data.items : []
        const seen = new Set(this.results.map((m) => m.id))
        for (const m of list) {
          if (!seen.has(m.id)) this.results.push(m)
        }
        this.cursor = data.next_cursor || null
        this.total = typeof data.total === 'number' ? data.total : this.results.length
        // place_fallback 仅在首页（无 cursor）时由后端返回
        if (!params.cursor) this.placeFallback = data.place_fallback || null
      } catch (e) {
        if (seq !== this.reqSeq) return
        this.error = e.response
          ? `搜索失败：HTTP ${e.response.status}`
          : '搜索失败：网络不可达'
      } finally {
        if (seq === this.reqSeq) this.loading = false
      }
    },
    setQuery(q) {
      this.query = (q || '').trim()
    },
    removeChip(key) {
      if (key === 'type') this.filters.type = ''
      else if (key === 'favorites') this.filters.favorites = false
      else if (key === 'tag') this.filters.tag = ''
      else if (key === 'place') this.filters.place = ''
      else if (key === 'person') this.filters.person = ''
      else if (key === 'date') {
        this.filters.date_after = ''
        this.filters.date_before = ''
      }
      this.run()
    },
    clearAll() {
      this.reqSeq++ // 作废在途响应，避免清空后被旧查询回填
      this.query = ''
      this.filters = emptyFilters()
      this.results = []
      this.cursor = null
      this.total = 0
      this.error = ''
      this.placeFallback = null
      this.searched = false
    }
  }
})
