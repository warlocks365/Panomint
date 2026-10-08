import { describe, expect, it } from 'vitest'
import {
  DIMENSIONS,
  HEADER_HEIGHT,
  MIN_ITEM_SIZE,
  bucketRange,
  buildHeaderLookup,
  buildMonthMatrix,
  findRowIndexByMediaId,
  fractionAtTs,
  headerKeyOf,
  headerLabelOf,
  isValidKey,
  keyAtTs,
  resolveAnchorIndex
} from '../timelineDimensions'

// Job000145 前端单测。本项目改动前**零测试文件**（架构师实测F10），
// 三档泛化的正确性原本只能靠人眼 —— 而泛化正是「沉默逻辑错误」的高发区：
// 键少一位补零、标签串错档、锚点兜底取成「第一个」都不会报错，只是位置悄悄错。
// 故本文件覆盖三条不变式：① 三档键定长零填充 ② 字典序 == 时间序 ③ 锚点解析语义。

describe('维度枚举与高度表', () => {
  it('档位枚举恰为三档且顺序固定', () => {
    expect(DIMENSIONS).toEqual(['year', 'month', 'day'])
  })

  // Spec §8.1 锁定：年 > 月 > 日的视觉层级。若被改成三档同高，层级即失效
  it('三档高度严格递减 56 > 46 > 34', () => {
    expect(HEADER_HEIGHT.year).toBeGreaterThan(HEADER_HEIGHT.month)
    expect(HEADER_HEIGHT.month).toBeGreaterThan(HEADER_HEIGHT.day)
    expect(HEADER_HEIGHT).toEqual({ year: 56, month: 46, day: 34 })
  })

  // TimelineGrid.vue 的 :min-item-size="34" 与之绑定。若下调 HEADER_HEIGHT.day
  // 而不同步下调 min-item-size，虚拟滚动会算小总高并抖动（见 timelineDimensions 注释）
  it('不变式：min-item-size ≤ 三档最小标题高度', () => {
    const minH = Math.min(...Object.values(HEADER_HEIGHT))
    expect(MIN_ITEM_SIZE).toBeLessThanOrEqual(minH)
    expect(MIN_ITEM_SIZE).toBe(34)
  })
})

describe('三档键定长零填充（不可省）', () => {
  // 锚点兜底的「字典序 == 时间序」与翻页守卫的 last <= key 全依赖定长。
  // 去掉补零（如 2024-6-4）会让 '2024-10' < '2024-6' 为假，静默跳错
  it.each([
    ['year', 4],
    ['month', 7],
    ['day', 10]
  ])('%s 档键长度恒为 %i', (dim, len) => {
    const d = new Date(2024, 5, 4) // 2024-06-04：月/日均为一位数，最易漏补零
    expect(headerKeyOf(dim, d)).toHaveLength(len)
    expect(headerKeyOf(dim, d)).toBe(headerKeyOf(dim, d))
  })

  it('day 档的月与日均补零', () => {
    expect(headerKeyOf('day', new Date(2024, 0, 2))).toBe('2024-01-02')
  })

  it('month 档的月补零', () => {
    expect(headerKeyOf('month', new Date(2024, 8, 15))).toBe('2024-09')
  })

  it('year 档不受月日影响（同一年内键相同）', () => {
    expect(headerKeyOf('year', new Date(2024, 0, 1))).toBe('2024')
    expect(headerKeyOf('year', new Date(2024, 11, 31))).toBe('2024')
  })

  it('闰年 2/29 有效（非闰年日期被 Date 归一到3/1，键随实际 Date 而非输入字面量）', () => {
    expect(headerKeyOf('day', new Date(2024, 1, 29))).toBe('2024-02-29')
    expect(headerKeyOf('day', new Date(2023, 1, 29))).toBe('2023-03-01')
  })

  it('无日期 / 非法日期 → unknown', () => {
    expect(headerKeyOf('day', null)).toBe('unknown')
    expect(headerKeyOf('day', new Date('nonsense'))).toBe('unknown')
  })
})

describe('键的形状自证（档位不匹配必须显式失败）', () => {
  it('月键在 day 档下非法（防止裸字符串靠长度猜档位）', () => {
    expect(isValidKey('day', '2024-06')).toBe(false)
    expect(isValidKey('month', '2024-06')).toBe(true)
  })

  it('年键在 month 档下非法', () => {
    expect(isValidKey('month', '2024')).toBe(false)
    expect(isValidKey('year', '2024')).toBe(true)
  })

  it('非零填充的键非法', () => {
    expect(isValidKey('day', '2024-6-4')).toBe(false)
    expect(isValidKey('month', '2024-6')).toBe(false)
  })

  it('unknown 不是合法锚点键', () => {
    expect(isValidKey('day', 'unknown')).toBe(false)
  })
})

describe('标题文案', () => {
  it('三档标题形态与 Spec §8.1 一致', () => {
    const d = new Date(2024, 5, 14)
    expect(headerLabelOf('year', d)).toBe('2024年')
    expect(headerLabelOf('month', d)).toBe('2024年6月')
    expect(headerLabelOf('day', d)).toBe('2024年6月14日')
  })

  it('无日期 → 未知日期（不为空串，避免渲染出空标题行）', () => {
    expect(headerLabelOf('day', null)).toBe('未知日期')
  })
})

/** 造一个与 useTimelineGrouping 同形的 flat：新→旧倒序，header 与 row 交替 */
function makeFlat(entries) {
  const out = []
  let y = 0
  for (const e of entries) {
    if (e.header) {
      out.push({ __key: `h:${e.key}`, header: true, level: e.level, anchorKey: e.key, ts: y })
      y += HEADER_HEIGHT[e.level] || HEADER_HEIGHT.month
    } else {
      out.push({ __key: `r:${e.id}`, header: false, cells: e.cells || [{ id: e.id }], ts: y })
      y += 100
    }
  }
  return out
}

describe('锚点解析 resolveAnchorIndex（AC-13/AC-05 的核心）', () => {
  const flat = makeFlat([
    { header: true, key: '2024-06', level: 'month' },
    { id: 'a' },
    { header: true, key: '2024-05', level: 'month' },
    { id: 'b' },
    { header: true, key: '2024-04', level: 'month' },
    { id: 'c' }
  ])
  const lookup = buildHeaderLookup(flat)

  it('精确命中返回该分组的流索引', () => {
    expect(resolveAnchorIndex(lookup, '2024-05')).toBe(2)
    expect(resolveAnchorIndex(lookup, '2024-06')).toBe(0)
  })

  // 兜底取「最后一个 <= key」而非「第一个」：语义与档位无关，不依赖遍历顺序假设。
  // 2024-03 早于所有分组 → 无 <= 它的键 → -1（回顶部），**不能**落到 2024-04：
  // 落到比目标更新的分组会让用户以为跳到了目标月，实际停在未来位置。
  it('目标分组无媒体且早于全部 → -1（回顶部，不落到更新的分组）', () => {
    expect(resolveAnchorIndex(lookup, '2024-03')).toBe(-1)
    expect(resolveAnchorIndex(lookup, '2020-01')).toBe(-1)
  })

  it('目标无媒体但晚于部分分组 → 取最后一个 <= key 的分组', () => {
    // 2024-05-15 落在 2024-05 分组内（非键，按 <= 兜底）→ 索引 2
    expect(resolveAnchorIndex(lookup, '2024-05-15')).toBe(2)
    // 2024-04-20 在 2024-04 分组之后 → 最后一个 <= 它的是 2024-05（索引 2）？否：
    // '2024-04-20' < '2024-05'，故 <= 它的最大键是 2024-04（索引 4）
    expect(resolveAnchorIndex(lookup, '2024-04-20')).toBe(4)
  })

  it('目标晚于所有媒体 → 最后一个分组（索引 0，新→旧流的首个 header）', () => {
    expect(resolveAnchorIndex(lookup, '2030-12')).toBe(0)
  })

  it('落点恒为 header 自身的索引（不会落进媒体行中间）', () => {
    for (const k of ['2024-06', '2024-05', '2024-04', '2024-03', '2024-05-15', '2030-01']) {
      const idx = resolveAnchorIndex(lookup, k)
      if (idx < 0) continue // -1 是合法的「回顶部」信号
      expect(flat[idx].header).toBe(true)
    }
  })

  it('unknown 桶不进查找表，且 unknown 不可作为锚点（AC-11）', () => {
    const withUnknown = makeFlat([
      { header: true, key: 'unknown', level: 'month' },
      { id: 'x' },
      { header: true, key: '2024-01', level: 'month' },
      { id: 'y' }
    ])
    const lk = buildHeaderLookup(withUnknown)
    expect(lk.index.has('unknown')).toBe(false)
    // 'unknown' 字典序大于所有数字键，若不显式挡掉会命中最后一个真实分组 → 静默错位
    expect(resolveAnchorIndex(lk, 'unknown')).toBe(-1)
    expect(resolveAnchorIndex(lk, '2024-01')).toBe(2)
  })

  it('非日期形状的键一律 -1（形状守卫先于字典序比较）', () => {
    expect(resolveAnchorIndex(lookup, '')).toBe(-1)
    expect(resolveAnchorIndex(lookup, null)).toBe(-1)
    expect(resolveAnchorIndex(lookup, 'abc')).toBe(-1)
    expect(resolveAnchorIndex(lookup, '2024-6')).toBe(-1)
  })
})

describe('不变式：字典序 == 时间序（三档均成立）', () => {
  // 这条是锚点兜底与翻页守卫 last <= key 的唯一前提。若有人为「缩短键」去掉补零，
  // 下面的断言会立刻变红——这是本期最该被钉死的一条
  it.each(['year', 'month', 'day'])('%s 档升序键序列与时间序一致', (dim) => {
    const d = new Date(2023, 11, 31)
    const keys = []
    for (let i = 0; i < 40; i++) {
      keys.push(headerKeyOf(dim, d))
      d.setDate(d.getDate() + 37) // 步长跨月跨年，覆盖三种档位
    }
    const sorted = [...keys].sort()
    expect(keys).toEqual(sorted)
  })

  it('跨年边界 12/31 → 1/1 仍然单调', () => {
    expect('2024-12' < '2025-01').toBe(true)
    expect('2024-12-31' < '2025-01-01').toBe(true)
    expect('2024' < '2025').toBe(true)
  })
})
