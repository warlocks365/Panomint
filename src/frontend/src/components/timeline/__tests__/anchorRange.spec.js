import { describe, expect, it } from 'vitest'
import {
  HEADER_HEIGHT,
  bucketRange,
  buildHeaderLookup,
  buildMonthMatrix,
  findRowIndexByMediaId,
  fractionAtTs,
  isValidKey,
  keyAtTs,
  resolveAnchorIndex
} from '../timelineDimensions'

// Job000145：滑块范围换算 / 切档锚点重定位 / 日历月矩阵。
// 与 timelineDimensions.spec.js 分文件是因为后者已近 300 行组织红线；
// 按「键与标签」/「定位与范围」两个关注点切开，不是为凑行数。

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
  // 2024-03 早于所有分组 → 无<= 它的键 → -1（回顶部），**不能**落到 2024-04：
  // 落到比目标更新的分组会让用户以为跳到了目标，实际停在未来位置。
  it('目标分组无媒体且早于全部 → -1（回顶部，不落到更新的分组）', () => {
    expect(resolveAnchorIndex(lookup, '2024-03')).toBe(-1)
    expect(resolveAnchorIndex(lookup, '2020-01')).toBe(-1)
  })

  it('目标无媒体但落在两个分组之间 → 取最后一个 <= key 的分组', () => {
    expect(resolveAnchorIndex(lookup, '2024-05-15')).toBe(2)
    // '2024-04-20' < '2024-05'，故 <= 它的最大键是 2024-04（索引 4）
    expect(resolveAnchorIndex(lookup, '2024-04-20')).toBe(4)
  })

  it('目标晚于所有媒体 → 索引 0（新→旧流的首个 header，即最新分组）', () => {
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

describe('按媒体 id 找行（切档锚点重定位用）', () => {
  const flat = makeFlat([
    { header: true, key: '2024-06', level: 'month' },
    { id: 'a', cells: [{ id: 'a' }, { id: 'a2' }] },
    { header: true, key: '2024-05', level: 'month' },
    { id: 'b', cells: [{ id: 'b' }] }
  ])

  it('命中首个格与行内非首格', () => {
    expect(findRowIndexByMediaId(flat, 'a')).toBe(1)
    expect(findRowIndexByMediaId(flat, 'a2')).toBe(1)
    expect(findRowIndexByMediaId(flat, 'b')).toBe(3)
  })

  it('不存在 → -1（触发同月首项降级）', () => {
    expect(findRowIndexByMediaId(flat, 'zzz')).toBe(-1)
  })
})

describe('滑块范围与分数换算', () => {
  const buckets = [{ bucket: '2024-04', count: 1 }, { bucket: '2024-06', count: 3 }]

  it('month 范围：最旧月 1 日 → 最新月次月 1 日', () => {
    const r = bucketRange('month', buckets)
    expect(new Date(r.oldestTs).getDate()).toBe(1)
    expect(new Date(r.oldestTs).getMonth()).toBe(3)
    expect(new Date(r.newestTs).getMonth()).toBe(6)
    expect(new Date(r.newestTs).getDate()).toBe(1)
  })

  it('year 范围：最旧年 1/1 → 最新年次年 1/1', () => {
    const r = bucketRange('year', [{ bucket: '2023', count: 1 }, { bucket: '2024', count: 3 }])
    expect(new Date(r.oldestTs).getFullYear()).toBe(2023)
    expect(new Date(r.oldestTs).getMonth()).toBe(0)
    expect(new Date(r.oldestTs).getDate()).toBe(1)
    expect(new Date(r.newestTs).getFullYear()).toBe(2025)
    expect(new Date(r.newestTs).getMonth()).toBe(0)
    expect(new Date(r.newestTs).getDate()).toBe(1)
  })

  // 档位与桶形状不匹配时不产生垃圾范围（否则滑块会画出 NaN 位置）
  it('year档收到 month 桶 → 范围全 0（不静默产出错误范围）', () => {
    expect(bucketRange('year', buckets)).toEqual({ oldestTs: 0, newestTs: 0 })
  })

  it('day 范围：当日 00:00 → 次日 00:00', () => {
    const r = bucketRange('day', [{ bucket: '2024-06-14', count: 2 }])
    expect(new Date(r.newestTs).getDate()).toBe(15)
  })

  it('unknown 与形状不符的桶被剔除，不参与范围计算', () => {
    const r = bucketRange('day', [{ bucket: 'unknown', count: 9 }, { bucket: '2024-06-14', count: 1 }])
    expect(new Date(r.oldestTs).getDate()).toBe(14)
  })

  it('空桶 → 范围全 0（滑块隐藏，不产生 NaN 分数）', () => {
    expect(bucketRange('month', [])).toEqual({ oldestTs: 0, newestTs: 0 })
    expect(fractionAtTs({ oldestTs: 0, newestTs: 0 }, Date.now())).toBe(0)
  })

  it('分数恒被夹在 [0,1]（防止越界导致滑块飞出轨道）', () => {
    const r = bucketRange('month', buckets)
    expect(fractionAtTs(r, r.newestTs)).toBe(0)
    expect(fractionAtTs(r, r.oldestTs)).toBe(1)
    expect(fractionAtTs(r, r.newestTs + 1e12)).toBe(0)
    expect(fractionAtTs(r, r.oldestTs - 1e12)).toBe(1)
  })

  it('keyAtTs 反解出的键落在合法桶区间内', () => {
    const r = bucketRange('month', buckets)
    const k = keyAtTs('month', (r.oldestTs + r.newestTs) / 2)
    expect(isValidKey('month', k)).toBe(true)
    expect(['2024-04', '2024-05', '2024-06']).toContain(k)
  })
})

describe('日历月矩阵', () => {
  it('恒为 6 行 × 7 列 = 42 格', () => {
    expect(buildMonthMatrix(2024, 6)).toHaveLength(42)
    expect(buildMonthMatrix(2024, 2)).toHaveLength(42)
  })

  it('2024-06-01 是周六 → 前面补 5 格（周一为首列）', () => {
    const cells = buildMonthMatrix(2024, 6)
    expect(cells[0].inMonth).toBe(false)
    expect(cells[5].key).toBe('2024-06-01')
    expect(cells[5].inMonth).toBe(true)
  })

  it('本月格数 = 该月天数（闰年 2 月 = 29）', () => {
    expect(buildMonthMatrix(2024, 2).filter((c) => c.inMonth)).toHaveLength(29)
    expect(buildMonthMatrix(2023, 2).filter((c) => c.inMonth)).toHaveLength(28)
    expect(buildMonthMatrix(2024, 4).filter((c) => c.inMonth)).toHaveLength(30)
  })

  // 跨月补位格必须无 key，否则会被当成可跳转日期（AC-10）
  it('跨月补位格 key 为空串（不可定位）', () => {
    const cells = buildMonthMatrix(2024, 6)
    for (const c of cells) {
      if (!c.inMonth) expect(c.key).toBe('')
      else expect(isValidKey('day', c.key)).toBe(true)
    }
  })

  it('格键按时间升序排列（42 格覆盖完整 6 周窗口）', () => {
    const cells = buildMonthMatrix(2024, 6)
    for (let i = 1; i < cells.length; i++) {
      expect(cells[i].ts).toBeGreaterThan(cells[i - 1].ts)
    }
  })
})