// @vitest-environment jsdom
// 定位算法纯逻辑验证：resolveAnchorIndex + offsets 是否指向目标分组。
// 不依赖浏览器/登录态—— 这些都是纯计算，出错就能定位到算法而非环境。
import { describe, it, expect } from 'vitest'
import { reactive, ref } from 'vue'
import { useTimelineGrouping } from '../useTimelineGrouping'
import { resolveAnchorIndex } from '../timelineDimensions'

/** 复刻真实数据分布：2024-11 ~ 2026-09，98 条、跨 14 个月 */
function makeItems() {
  const counts = { '2024-11': 6, '2025-01': 5, '2025-03': 6, '2025-04': 5, '2025-06': 6,
    '2025-07': 5, '2025-08': 6, '2025-09': 5, '2025-10': 7, '2025-12': 5,
    '2026-02': 6, '2026-06': 6, '2026-08': 1, '2026-09': 26 }
  const out = []
  let id = 0
  for (const ym of Object.keys(counts).sort()) {
    const [y, m] = ym.split('-').map(Number)
    for (let d = 1; d <= counts[ym]; d++) {
      out.push({ id: ++id, taken_at: new Date(Date.UTC(y, m - 1, (d % 28) + 1, 12)).toISOString() })
    }
  }
  // 时间倒序（后端 /media 即如此）
  return out.sort((a, b) => (a.taken_at < b.taken_at ? 1 : -1))
}

function setup(dim = 'day') {
  const items = reactive(makeItems())
  const g = useTimelineGrouping(items, ref(5), ref(34), ref(dim))
  return { items, g }
}

describe('定位算法 · 跨年目标', () => {
  it('resolveAnchorIndex 命中 2024-11 的分组', () => {
    const { g } = setup('day')
    const idx = resolveAnchorIndex(g.headerIndex.value, '2024-11-03')
    expect(idx).toBeGreaterThanOrEqual(0)
    const el = g.flat.value[idx]
    expect(el.header).toBe(true)
    expect(el.anchorKey).toBe('2024-11-03')
  })

  it('offsets[idx] 正是该分组头的纵向偏移', () => {
    const { g } = setup('day')
    const idx = resolveAnchorIndex(g.headerIndex.value, '2024-11-03')
    // 偏移应等于该元素之前所有元素高度之和
    let expectY = 0
    for (let i = 0; i < idx; i++) {
      const it = g.flat.value[i]
      expectY += it.header ? (it.level === 'day' ? 34 : it.level === 'month' ? 46 : 56) : 34
    }
    expect(g.offsets.value[idx]).toBe(expectY)
  })

  it('跨年目标（2024-11）比 2026-09 的偏移更大——方向正确', () => {
    const { g } = setup('day')
    const a = resolveAnchorIndex(g.headerIndex.value, '2026-09-27')
    const b = resolveAnchorIndex(g.headerIndex.value, '2024-11-03')
    expect(b).toBeGreaterThan(a)
    expect(g.offsets.value[b]).toBeGreaterThan(g.offsets.value[a])
  })

  it('未命中的日期退到「最后一个 <= key 的分组」，不越界', () => {
    const { g } = setup('day')
    const idx = resolveAnchorIndex(g.headerIndex.value, '2024-11-30') // 该月只有 03/20
    expect(idx).toBeGreaterThanOrEqual(0)
    expect(g.flat.value[idx].header).toBe(true)
    expect(g.flat.value[idx].anchorKey <= '2024-11-30').toBe(true)
  })

  it('非法键返回 -1（调用方据此不滚）', () => {
    const { g } = setup('day')
    expect(resolveAnchorIndex(g.headerIndex.value, 'unknown')).toBe(-1)
    expect(resolveAnchorIndex(g.headerIndex.value, '')).toBe(-1)
  })

  it('年/月档同理可定位', () => {
    const gYear = setup('year')
    const iy = resolveAnchorIndex(gYear.g.headerIndex.value, '2024')
    expect(gYear.g.flat.value[iy].anchorKey).toBe('2024')
    const gMonth = setup('month')
    const im = resolveAnchorIndex(gMonth.g.headerIndex.value, '2024-11')
    expect(gMonth.g.flat.value[im].anchorKey).toBe('2024-11')
  })
})