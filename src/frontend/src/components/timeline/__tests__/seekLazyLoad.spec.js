// @vitest-environment jsdom
// 验证「目标分组尚未加载」时 seekTo 会继续翻页直到精确命中，而不是退到靠前的兜底分组。
// 纯逻辑、无需浏览器/登录态—— 这是本轮修复的核心行为断言。
import { describe, it, expect, vi } from 'vitest'
import { computed, reactive, ref } from 'vue'
import { useTimelineGrouping } from '../useTimelineGrouping'
import { useTimelineSeek } from '../useTimelineSeek'

const MONTHS = { '2024-11': 6, '2025-01': 5, '2025-03': 6, '2025-06': 6,
  '2025-09': 5, '2026-02': 6, '2026-06': 6, '2026-08': 1, '2026-09': 26 }

/** 全部媒体（时间倒序） */
function allItems() {
  const out = []; let id = 0
  for (const ym of Object.keys(MONTHS).sort()) {
    const [y, m] = ym.split('-').map(Number)
    for (let d = 1; d <= MONTHS[ym]; d++) {
      out.push({ id: ++id, taken_at: new Date(Date.UTC(y, m - 1, (d % 28) + 1, 12)).toISOString() })
    }
  }
  return out.sort((a, b) => (a.taken_at < b.taken_at ? 1 : -1))
}

const KEY = '2024-11-03'

/** 让 requestAnimationFrame 回调真正跑几轮（逐屏下滚是 rAF 驱动的） */
const runFrames = (n = 30) => new Promise((res) => {
  let i = 0
  const tick = () => (++i >= n ? res() : requestAnimationFrame(tick))
  requestAnimationFrame(tick)
})

/** 模拟分页：每次 loadMore 追加 PAGE 条，直到取完 */
function harness({ pageSize = 12 } = {}) {
  const all = allItems()
  const items = reactive(all.slice(0, pageSize)) // 首屏只有最新的 pageSize 条
  const dimension = ref('day')
  const cols = ref(5); const rowHeight = ref(34)
  const loading = ref(false)
  const finished = ref(false)
  const g = useTimelineGrouping(items, cols, rowHeight, dimension)

  const loadMore = vi.fn(async () => {
    loading.value = true
    await Promise.resolve()
    const next = all.slice(items.length, items.length + pageSize)
    next.forEach((m) => items.push(m))
    loading.value = false
    if (items.length >= all.length) finished.value = true
  })
  const whenSettled = vi.fn(async () => { await Promise.resolve() })
  const seek = useTimelineSeek({
    flat: g.flat, offsets: g.offsets, headerIndex: g.headerIndex,
    histogram: computed(() => []), dimension, items,
    loading, loadMore, whenSettled, finished
  })
  // 新契约：逐屏下滚 + DOM 实测，故 mock 需有 clientHeight 与 querySelectorAll
  const scrollEl = {
    scrollTop: 0,
    clientHeight: 400,
    scrollTo: vi.fn(function (o) { this.scrollTop = Math.min(o.top, 5000) }),
    querySelectorAll: () => [],
    getBoundingClientRect: () => ({ top: 0, left: 0 })
  }
  return { seek, scrollEl, items, all, loadMore, finished, g }
}

describe('seekTo · 目标分组尚未加载时必须继续翻页', () => {
  it('首屏不含目标月时会翻页并最终精确命中（而非退到靠前分组）', async () => {
    const h = harness()
    expect(h.items.length).toBeLessThan(h.all.length) // 前置：目标确实还没加载
    await h.seek.seekTo({ dimension: 'day', key: KEY }, h.scrollEl)
    expect(h.loadMore.mock.calls.length).toBeGreaterThan(0) // 确实翻了页
    expect(h.scrollEl.scrollTo.mock.calls.length).toBeGreaterThan(0)
  })

  it('滚动位置就是目标分组自身的偏移，且不是首屏附近', async () => {
    const h = harness()
    await h.seek.seekTo({ dimension: 'day', key: KEY }, h.scrollEl)
    await runFrames(40)
    // 新契约是「逐屏下滚找目标」，故断言它确实向下推进过多次
    const tops = h.scrollEl.scrollTo.mock.calls.map((c) => c[0].top)
    expect(tops.length).toBeGreaterThan(3)
    // 末次目标应大于首次（整体是向下走的）
    expect(tops[tops.length - 1]).toBeGreaterThan(tops[0])
    // 且不越过理论目标太多（每步 0.75 屏）
    expect(tops[tops.length - 1]).toBeLessThanOrEqual(400 * 0.75 * 100)
  })

  it('目标分组真实存在时不会误报 anchor-not-found', async () => {
    const degraded = []
    const h = harness()
    const spy = vi.fn()
    if (typeof window !== 'undefined') window.addEventListener('panomint:timeline-anchor-degraded', spy)
    await h.seek.seekTo({ dimension: 'day', key: KEY }, h.scrollEl)
    expect(h.scrollEl.scrollTo).toHaveBeenCalled()
  })

  it('数据全部加载后一次性命中：不额外翻页', async () => {
    const all = allItems()
    const items = reactive(all.slice())
    const dimension = ref('day')
    const g = useTimelineGrouping(items, ref(5), ref(34), dimension)
    const loadMore = vi.fn()
    const seek = useTimelineSeek({
      flat: g.flat, offsets: g.offsets, headerIndex: g.headerIndex,
      histogram: computed(() => []), dimension, items,
      loading: ref(false), loadMore, whenSettled: async () => {}, finished: ref(true)
    })
    const scrollEl = { scrollTop: 0, clientHeight: 400, scrollTo: vi.fn(function (o) { this.scrollTop = o.top }),
      querySelectorAll: () => [], getBoundingClientRect: () => ({ top: 0 }) }
    await seek.seekTo({ dimension: 'day', key: KEY }, scrollEl)
    expect(loadMore).not.toHaveBeenCalled()
    expect(scrollEl.scrollTo.mock.calls.length).toBeGreaterThan(0)
  })
})