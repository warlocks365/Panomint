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
  const scrollEl = { scrollTo: vi.fn() }
  return { seek, scrollEl, items, all, loadMore, finished, g }
}

describe('seekTo · 目标分组尚未加载时必须继续翻页', () => {
  it('首屏不含目标月时会翻页并最终精确命中（而非退到靠前分组）', async () => {
    const h = harness()
    expect(h.items.length).toBeLessThan(h.all.length) // 前置：目标确实还没加载
    await h.seek.seekTo({ dimension: 'day', key: KEY }, h.scrollEl)
    expect(h.loadMore.mock.calls.length).toBeGreaterThan(0) // 确实翻了页
    expect(h.scrollEl.scrollTo).toHaveBeenCalledTimes(1)
  })

  it('滚动位置就是目标分组自身的偏移，且不是首屏附近', async () => {
    const h = harness()
    await h.seek.seekTo({ dimension: 'day', key: KEY }, h.scrollEl)
    const call = h.scrollEl.scrollTo.mock.calls[0][0]
    // 该偏移应等于「目标分组之前的元素高度之和」
    const idx = h.g.headerIndex.value.index.get(KEY)
    expect(idx).toBeGreaterThan(0)
    let expectY = 0
    for (let i = 0; i < idx; i++) {
      const it = h.g.flat.value[i]
      expectY += it.header ? 34 : 34
    }
    expect(call.top).toBe(expectY)
    // 首屏（只有最新 12 条）的最大偏移远小于目标偏移
    expect(call.top).toBeGreaterThan(34 * 10)
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
    const scrollEl = { scrollTo: vi.fn() }
    await seek.seekTo({ dimension: 'day', key: KEY }, scrollEl)
    expect(loadMore).not.toHaveBeenCalled()
    expect(scrollEl.scrollTo).toHaveBeenCalledTimes(1)
  })
})