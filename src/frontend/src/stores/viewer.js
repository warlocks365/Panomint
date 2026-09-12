import { defineStore } from 'pinia'

// 查看器全局状态（幻灯片 + 播放集）。
//
// 设计要点：
// - 播放集（items/index/source）集中在此，供「模态查看器 MediaViewer」与「路由播放页 PlayerView」共用；
//   MediaViewer 仍保留 items/index props 以兼容既有调用方（TimelineView / AlbumDetailView），
//   但会把 props 镜像进本 store，从而 PlayerView 也能拿到同一播放集实现连续播放。
// - 幻灯片播控状态（playing/interval）与编辑中的临时参数（editing）也在此集中。
// - 默认跳过 video/360（skipVideo360=true），仅对本 store 的自动播放生效（手动 ←/→ 不受限）。

// 幻灯片可选间隔（毫秒）
export const SLIDESHOW_INTERVALS = [3000, 5000, 10000]

export const useViewerStore = defineStore('viewer', {
  state: () => ({
    items: [],
    index: 0,
    source: 'timeline', // timeline | search | album | map
    open: false,
    playing: false,
    interval: 5000,
    skipVideo360: true
  }),
  getters: {
    current: (s) => s.items[s.index] || null,
    count: (s) => s.items.length,
    hasPrev: (s) => s.index > 0,
    hasNext: (s) => s.index >= 0 && s.index < s.items.length - 1
  },
  actions: {
    // 设定播放集（不改变 open）。items 直接引用调用方数组，保证与页面数据同步。
    setPlayset({ items, index = 0, source } = {}) {
      if (Array.isArray(items)) this.items = items
      if (source) this.source = source
      const max = Math.max(this.items.length - 1, 0)
      this.index = Math.min(Math.max(Number(index) || 0, 0), max)
    },
    openAt(payload = {}) {
      this.setPlayset(payload)
      this.open = true
    },
    close() {
      this.open = false
      this.playing = false
    },
    setOpen(v) {
      this.open = !!v
      if (!v) this.playing = false
    },
    setIndex(i) {
      if (!this.items.length) {
        this.index = 0
        return
      }
      this.index = Math.min(Math.max(Number(i) || 0, 0), this.items.length - 1)
    },
    // 返回是否成功移动
    go(delta) {
      const next = this.index + delta
      if (next < 0 || next >= this.items.length) return false
      this.index = next
      return true
    },
    next() {
      return this.go(1)
    },
    prev() {
      return this.go(-1)
    },
    setIntervalMs(ms) {
      const n = Number(ms)
      if (n >= 1000) this.interval = n
    },
    play() {
      if (this.items.length > 1) this.playing = true
    },
    pause() {
      this.playing = false
    },
    togglePlay() {
      if (this.playing) this.pause()
      else this.play()
    },
    // 幻灯片"下一张"：按 skipVideo360 跳过视频/360，返回是否成功
    nextForSlideshow() {
      let i = this.index + 1
      while (i < this.items.length) {
        const m = this.items[i]
        const skip = this.skipVideo360 && (m?.type === 'video' || m?.is_360 || m?.type === '360')
        if (!skip) {
          this.index = i
          return true
        }
        i++
      }
      return false
    },
    removeById(id) {
      const i = this.items.findIndex((m) => String(m.id) === String(id))
      if (i < 0) return
      this.items.splice(i, 1)
      if (this.index >= this.items.length) this.index = Math.max(this.items.length - 1, 0)
    }
  }
})
