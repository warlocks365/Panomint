// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { computed, nextTick, ref } from 'vue'
import { useTimelineGrouping } from '../useTimelineGrouping'
import { DIMENSIONS, HEADER_HEIGHT, headerKeyOf } from '../timelineDimensions'

// Job000145 核心语义回归：**档位即唯一可见的标题粒度**（AC-01/02/03）。
//
// 为什么这条值得单独钉：ADR-001 记录了架构师初稿把「仅切换标题粒度」误读为
// 「三级标题始终全部渲染」，那个解读会导出「年档标题比日档更多」的自相矛盾结果，
// 已被团队裁决驳回。但代码结构上的 `level: dim` 只是**当前实现**的形状，
// 不构成契约保证 —— 若日后有人「好心」把多级渲染加回来（Google Photos 2026-07
// 取消日期分隔线后用户强烈反弹，说明日期标题是用户依赖的定位锚点），
// 只有这条用例会拦住他。
//
// 故本文件直接对 useTimelineGrouping 的输出 flat 做断言：不看实现，只看产出。

/** 造媒体：id 顺序即传入顺序（生产中为时间倒序：新→旧） */
function media(id, iso) {
  return { id, taken_at: iso }
}

function build(dimension, items, cols = 5) {
  const dim = ref(dimension)
  const colsRef = ref(cols)
  const rowHeight = ref(148)
  const g = useTimelineGrouping(items, colsRef, rowHeight, dim)
  return { ...g, dim }
}

const SAMPLE = [
  media(1, '2024-06-14T10:00:00Z'),
  media(2, '2024-06-14T11:00:00Z'),
  media(3, '2024-06-02T09:00:00Z'),
  media(4, '2024-05-20T08:00:00Z'),
  media(5, '2023-11-05T12:00:00Z'),
  media(6, '2023-02-28T12:00:00Z')
]

describe('档位即唯一可见标题粒度（AC-01/02/03）', () => {
  it.each(DIMENSIONS)('%s 档：flat 中不存在任何 level !== 当前档位的 header', (dim) => {
    const { flat } = build(dim, SAMPLE)
    const headers = flat.value.filter((it) => it.header)
    expect(headers.length).toBeGreaterThan(0)
    // 这条断言就是全部意义：年档下不得出现月/日标题，反之亦然
    expect(headers.every((h) => h.level === dim)).toBe(true)
    const foreign = headers.filter((h) => h.level !== dim).map((h) => h.level)
    expect(foreign).toEqual([])
  })

  it('年档：标题集合恰为每个年份一个，且不含月/日形态的键', () => {
    const { flat } = build('year', SAMPLE)
    const keys = flat.value.filter((it) => it.header).map((h) => h.anchorKey)
    expect(keys).toEqual(['2024', '2023'])
    // 年档的键不得含分隔符（月/日键才有），这是「不产出月/日标题」的键级体现
    for (const k of keys) expect(k).not.toContain('-')
  })

  it('月档：标题键为 YYYY-MM，粒度恰好比年档细一级', () => {
    const { flat } = build('month', SAMPLE)
    const keys = flat.value.filter((it) => it.header).map((h) => h.anchorKey)
    expect(keys).toEqual(['2024-06', '2024-05', '2023-11', '2023-02'])
    for (const k of keys) expect(k).toMatch(/^\d{4}-\d{2}$/)
  })

  it('日档：标题键为 YYYY-MM-DD，且数量多于月档（细粒度必然更多分组）', () => {
    const dayKeys = build('day', SAMPLE).flat.value.filter((it) => it.header).map((h) => h.anchorKey)
    const monthKeys = build('month', SAMPLE).flat.value.filter((it) => it.header).map((h) => h.anchorKey)
    expect(dayKeys).toEqual(['2024-06-14', '2024-06-02', '2024-05-20', '2023-11-05', '2023-02-28'])
    expect(dayKeys.length).toBeGreaterThan(monthKeys.length)
    for (const k of dayKeys) expect(k).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  })

  // 这条直接否掉「年档全渲染」的错解读：那样年档标题数会 >= 日档标题数，
  // 与「年档只显示年标题、粒度更粗」矛盾
  it('三档标题数量严格递增：year < month < day（年档不可能比日档标题多）', () => {
    const n = (d) => build(d, SAMPLE).flat.value.filter((it) => it.header).length
    const [y, m, d] = DIMENSIONS.map(n)
    expect(y).toBeLessThan(m)
    expect(m).toBeLessThan(d)
  })

  it('切档后标题标签随之改变（唯一真源 headerLabelOf，非硬编码）', () => {
    const label = (d) => build(d, SAMPLE).flat.value.find((it) => it.header).label
    expect(label('year')).toBe('2024年')
    expect(label('month')).toBe('2024年6月')
    expect(label('day')).toBe('2024年6月14日')
  })

  it('同一档位内 anchorKey 唯一（否则 headerIndex 会互相覆盖）', () => {
    for (const dim of DIMENSIONS) {
      const { flat } = build(dim, SAMPLE)
      const keys = flat.value.filter((it) => it.header).map((h) => h.anchorKey)
      expect(new Set(keys).size).toBe(keys.length)
    }
  })

  it('无拍摄日期媒体归入唯一 unknown 标题，不与真实日期混排', () => {
    const withUnknown = [...SAMPLE, media(7, null), media(8, null)]
    const { flat } = build('day', withUnknown)
    const headers = flat.value.filter((it) => it.header)
    const unknown = headers.filter((h) => h.anchorKey === 'unknown')
    expect(unknown).toHaveLength(1)
    expect(unknown[0].label).toBe('未知日期')
    // unknown 不进 headerIndex（不可定位，AC-11）
    expect(flat.value && build('day', withUnknown).headerIndex.value.index.has('unknown')).toBe(false)
  })
})

describe('offsets 与标题高度一致（切档后滚动定位的物理基础）', () => {
  it.each(DIMENSIONS)('%s 档：每个 header 占 HEADER_HEIGHT[档位]', (dim) => {
    const { flat, offsets } = build(dim, SAMPLE)
    const list = flat.value
    for (let i = 0; i < list.length; i++) {
      if (!list[i].header) continue
      const next = i + 1 < list.length ? offsets.value[i + 1] : offsets.value[i] + HEADER_HEIGHT[dim]
      expect(next - offsets.value[i]).toBe(HEADER_HEIGHT[dim])
    }
  })

  it('切档后总高度按新档位重算（不是沿用旧档位高度）', () => {
    const colsRef = ref(5)
    const rowHeight = ref(148)
    const dim = ref('month')
    const g = useTimelineGrouping(SAMPLE, colsRef, rowHeight, dim)
    // 总高= 末元素 offset + 末元素自身高度
    const total = () => {
      const list = g.flat.value
      const offs = g.offsets.value
      const last = list[list.length - 1]
      return offs[offs.length - 1] + (last.header ? HEADER_HEIGHT[dim.value] : rowHeight.value)
    }
    const monthTotal = total()
    dim.value = 'day'
    const dayTotal = total()
    // 日档 header 更多（5 个 vs 4 个）且行数可能变化，故总高不必单调——
    // 要断言的是「确实重算了」，而非某个方向。用年档对比才有确定方向：
    dim.value = 'year'
    expect(total()).toBeLessThan(monthTotal) // 年档只剩 2 个 header，总高必然更小
    expect(dayTotal).not.toBe(monthTotal)
  })

  it('标题高度不随列数变化（否则 offsets 与实际渲染高度会漂移）', () => {
    // 列数会改变行数，故不能比总高；只断言 header 段增量恒为该档位高度
    const headerDelta = (cols) => {
      const g = build('month', SAMPLE, cols)
      const idx = g.flat.value.findIndex((it) => it.header)
      return g.offsets.value[idx + 1] - g.offsets.value[idx]
    }
    expect(headerDelta(5)).toBe(HEADER_HEIGHT.month)
    expect(headerDelta(3)).toBe(HEADER_HEIGHT.month)
    expect(headerDelta(2)).toBe(HEADER_HEIGHT.month)
  })
})

describe('切档即时生效（响应式）', () => {
  it('dimension 改变后 flat 立即重算，无需重新加载 items', async () => {
    const g = build('month', SAMPLE)
    expect(g.flat.value.filter((it) => it.header).map((h) => h.anchorKey)).toContain('2024-06')
    g.dim.value = 'year'
    await nextTick()
    const keys = g.flat.value.filter((it) => it.header).map((h) => h.anchorKey)
    expect(keys).toEqual(['2024', '2023'])
    // 媒体集合不变（AC-04：切档不动数据流）
    expect(g.flat.value.filter((it) => !it.header).flatMap((r) => r.cells.map((c) => c.id))).toEqual([1, 2, 3, 4, 5, 6])
  })

  it('切档后 headerIndex 的键空间随之收窄', async () => {
    const g = build('day', SAMPLE)
    expect(g.headerIndex.value.index.has('2024-06-14')).toBe(true)
    g.dim.value = 'year'
    await nextTick()
    expect(g.headerIndex.value.index.has('2024-06-14')).toBe(false)
    expect(g.headerIndex.value.index.has('2024')).toBe(true)
  })
})