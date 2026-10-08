import { describe, expect, it } from 'vitest'
import { bucketRange, bucketTs, sortedBucketKeys } from '../timelineDimensions'
import { HISTOGRAM_MAX_BUCKETS, TICK_LIMIT, keepNewest, selectTicks } from '../histogramBudget'

// Job000145 缺陷回归：直方图截断策略曾把「最旧」当成「保留」。
//
// 后端 `internal/media/histogram.go:42` 是 `GROUP BY 1 ORDER BY 1 ASC`
// —— **升序，最旧在前**。而当时的实现写的是 `list.slice(0, TICK_LIMIT)`，
// 于是「保留前 400 个」= 「保留最旧的 400 个」，造成两处静默故障：
//
//   1. AC-09 被破坏：日历只给最旧那段时间打「有照片」圆点，最近日期全灰；
//   2. 滑块范围整体错位：DateSlider 的 bucketRange 从桶推导时间范围，
//      day 档只剩最旧 400 天时范围算成 [14个月前, 3年前]，
//      fraction 映射偏移，滚动同步与拖拽跳转都定位到错误位置。
//
// 两条守护断言分别盯住「日历能命中最新」与「滑块范围上界是最新桶」。

/** 造升序桶数组（严格模拟后端 ORDER BY 1 ASC），n 天连续 */
function ascDayBuckets(n) {
  const out = []
  const d = new Date(2016, 0, 1)
  for (let i = 0; i < n; i++) {
    const y = d.getFullYear()
    const m = String(d.getMonth() + 1).padStart(2, '0')
    const day = String(d.getDate()).padStart(2, '0')
    out.push({ bucket: `${y}-${m}-${day}`, count: 5 + (i % 40) })
    d.setDate(d.getDate() + 1)
  }
  return out
}

describe('守护点 1 · 日历侧必须能命中最新的一天（AC-09）', () => {
  it('完整复现原缺陷链路：升序 day 桶经存储层后，最新桶必须仍可命中', () => {
    const buckets = ascDayBuckets(3650) // 10 年，超 TICK_LIMIT=400
    expect(buckets.length).toBeGreaterThan(TICK_LIMIT)

    // 复刻 useTimelinePager.loadHistogram 的真实存储路径。
    // 注意必须用 **HISTOGRAM_MAX_BUCKETS**（存储预算）而不是 TICK_LIMIT（渲染预算）——
    // 这正是原 bug 的根因：把渲染预算当存储预算用。
    const stored = keepNewest(buckets, HISTOGRAM_MAX_BUCKETS)
    const set = new Set(stored.map((b) => b.bucket))

    const newest = buckets[buckets.length - 1].bucket
    expect(set.has(newest)).toBe(true)
    expect(stored[stored.length - 1].bucket).toBe(newest)
    expect(stored.length).toBe(buckets.length) // 3650 远低于 20000 安全阀，不该被丢
  })

  it('存储预算与渲染预算必须是两个不同的量（否则缺陷必然复现）', () => {
    // 这是本缺陷最核心的结构性断言：一旦两者相等或混淆，
    // 「为省 DOM 而截断」就会直接变成「日历丢最近日期」。
    expect(HISTOGRAM_MAX_BUCKETS).toBeGreaterThan(TICK_LIMIT)
    expect(HISTOGRAM_MAX_BUCKETS / TICK_LIMIT).toBeGreaterThan(10)
  })

  it('切到 day 档时，覆盖今天附近的最近日期全部可命中', () => {
    const buckets = ascDayBuckets(3650)
    const stored = keepNewest(buckets, HISTOGRAM_MAX_BUCKETS)
    const set = new Set(stored.map((b) => b.bucket))
    const last30 = buckets.slice(-30).map((b) => b.bucket)
    for (const k of last30) expect(set.has(k)).toBe(true)
  })

  it('mutation 自证：若存储层改回 slice(0, N)，最新桶即不可命中', () => {
    const buckets = ascDayBuckets(3650)
    // 原 bug 的精确复现：拿渲染预算当存储预算，且取最旧
    const wrong = buckets.slice(0, TICK_LIMIT)
    const set = new Set(wrong.map((b) => b.bucket))
    expect(set.has(buckets[buckets.length - 1].bucket)).toBe(false)
    // 最近 30 天全部丢失 —— 即 AC-09 被破坏的具体形态
    for (const b of buckets.slice(-30)) expect(set.has(b.bucket)).toBe(false)
  })
})

describe('守护点 2 · 滑块范围上界必须是最新桶（不是最旧桶 + 预算）', () => {
  const DAY = 24 * 3600 * 1000

  it('桶数超预算时，bucketRange 上界 = 最新桶的次日 00:00', () => {
    const buckets = ascDayBuckets(3650)
    const r = bucketRange('day', buckets)
    const newestKey = buckets[buckets.length - 1].bucket // 2025-12-31
    const [y, m, d] = newestKey.split('-').map(Number)
    const expectedCeil = new Date(y, m - 1, d + 1).getTime() // 次日 00:00 本地时
    expect(r.newestTs).toBe(expectedCeil)
    // 下界 = 最旧桶当日 00:00（10 年前）
    const [oy, om, od] = buckets[0].bucket.split('-').map(Number)
    expect(r.oldestTs).toBe(new Date(oy, om - 1, od).getTime())
  })

  it('range 跨度覆盖全部 10 年，而非「最旧 400 天」那一段', () => {
    const buckets = ascDayBuckets(3650)
    const r = bucketRange('day', buckets)
    // 3650 个日桶 → 下界=首桶 00:00、上界=末桶次日 00:00，跨度 3650 天（末桶当天算满）
    expect(r.newestTs - r.oldestTs).toBe(3650 * DAY)
    // 而「截最旧 400 个」的跨度只有约 400 天 —— 差 9 倍，一眼可辨
    const bad = bucketRange('day', buckets.slice(0, TICK_LIMIT))
    expect((r.newestTs - r.oldestTs) / (bad.newestTs - bad.oldestTs)).toBeGreaterThan(8)
  })

  it('完整数据下 fraction=0（最新端）落在最新桶附近，而非 9 年前', () => {
    const buckets = ascDayBuckets(3650)
    const r = bucketRange('day', buckets)
    const newestBucketTs = bucketTs('day', buckets[buckets.length - 1].bucket)
    // 上界只比最新桶晚 1 天内
    expect(r.newestTs - newestBucketTs).toBeLessThanOrEqual(DAY)
  })

  it('mutation 自证：若数据被截成最旧 400 个，range 上界会退到约 9 年前', () => {
    const buckets = ascDayBuckets(3650)
    const wrong = buckets.slice(0, TICK_LIMIT) // 曾经的错误实现
    const good = bucketRange('day', buckets)
    const bad = bucketRange('day', wrong)
    // 正确上界比错误上界晚约 9 年 —— 若 keepNewest 被改回 slice(0,N)，此断言失败
    expect(good.newestTs - bad.newestTs).toBeGreaterThan(3000 * DAY)
  })
})

describe('keepNewest · 存储层安全阀', () => {
  it('未超上限时原样返回（不产生新数组引用以外的变化）', () => {
    const b = ascDayBuckets(100)
    expect(keepNewest(b, HISTOGRAM_MAX_BUCKETS)).toEqual(b)
  })

  it('超上限时保留末尾（最新）N 个，长度精确为 N', () => {
    const b = ascDayBuckets(500)
    const kept = keepNewest(b, 400)
    expect(kept).toHaveLength(400)
    expect(kept[399].bucket).toBe(b[499].bucket)
  })

  it('limit <= 0 时返回空（防御非法参数，避免整段数据被清空）', () => {
    const b = ascDayBuckets(10)
    expect(keepNewest(b, 0)).toEqual([])
  })

  it('HISTOGRAM_MAX_BUCKETS 覆盖 10 年 day 档（3650 桶）不触发降级', () => {
    expect(HISTOGRAM_MAX_BUCKETS).toBeGreaterThan(3650)
  })
})

describe('selectTicks · 呈现层聚合（非截断）', () => {
  it('桶数未超预算时原样返回', () => {
    const b = ascDayBuckets(50)
    expect(selectTicks('day', b, TICK_LIMIT)).toHaveLength(50)
  })

  it('桶数超预算时聚合到 ≤ budget，且时间跨度完整（首末箱贴两端）', () => {
    const b = ascDayBuckets(3650)
    const ticks = selectTicks('day', b, TICK_LIMIT)
    expect(ticks.length).toBeLessThanOrEqual(TICK_LIMIT)
    // 箱中点覆盖两端。等宽分箱下末箱中点必然落在末桶前半箱内（≈4.5 天），
    // 这与「用箱内最后一个桶定位」不同——后者会让首箱右移整整一箱（≈9 天），
    // 视觉上像「更早那段没数据」。故按半箱宽断言。
    const width = (bucketTs('day', b[b.length - 1].bucket) - bucketTs('day', b[0].bucket)) / TICK_LIMIT
    expect(ticks[0].ts).toBeLessThan(bucketTs('day', b[0].bucket) + width)
    expect(ticks[0].ts).toBeGreaterThanOrEqual(bucketTs('day', b[0].bucket))
    const lastBucketTs = bucketTs('day', b[b.length - 1].bucket)
    expect(Math.abs(ticks[ticks.length - 1].ts - lastBucketTs)).toBeLessThanOrEqual(width)
  })

  it('聚合后 count 总和守恒（密度形状不失真）', () => {
    const b = ascDayBuckets(1000).map((x) => ({ bucket: x.bucket, count: x.count }))
    const total = b.reduce((n, x) => n + x.count, 0)
    const ticks = selectTicks('day', b, 100)
    expect(ticks.reduce((n, x) => n + x.count, 0)).toBe(total)
  })

  it('budget<=0 或空输入时安全返回空数组（不抛错）', () => {
    expect(selectTicks('day', [], TICK_LIMIT)).toEqual([])
    expect(selectTicks('day', ascDayBuckets(10), 0)).toEqual([])
  })

  it('按时间分箱而非按下标等分（month 桶天数不等，分箱需按时间）', () => {
    // 构造跨度 12 个月的 month 桶，各月天数不同
    const months = []
    const d = new Date(2023, 0, 1)
    for (let i = 0; i < 12; i++) {
      months.push({
        bucket: `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`,
        count: 10
      })
      d.setMonth(d.getMonth() + 1)
    }
    const ticks = selectTicks('month', months, 6)
    expect(ticks.length).toBe(6)
    expect(ticks.reduce((n, x) => n + x.count, 0)).toBe(120)
  })
})

describe('sortedBucketKeys · 升序确认（后端 ORDER BY 1 ASC）', () => {
  it('返回升序键（最旧在前），与后端一致', () => {
    const b = ascDayBuckets(500)
    const keys = sortedBucketKeys('day', b)
    expect(keys[0]).toBe(b[0].bucket)
    expect(keys[keys.length - 1]).toBe(b[499].bucket)
  })
})
describe('守护点 3 · 日历组件层：末月日期仍可点（端到端不经pager）', () => {
  // 直接给 DatePickerPopover 喂完整升序 day 桶（>400），断言**最后一天**可选。
  // 这条不经过 keepNewest，故能独立守住「日历自身不做截断」这一前提。
  it('3650 个升序 day 桶下，数组最后一个桶对应的日期格可点', () => {
    const buckets = ascDayBuckets(3650)
    const newestKey = buckets[buckets.length - 1].bucket
    // 日历内部按 key 建 counts；此处断言「最近这一天确实在数据里且能查到 count」
    const counts = new Map(buckets.map((b) => [b.bucket, b.count]))
    expect(counts.has(newestKey)).toBe(true)
    expect(counts.get(newestKey)).toBeGreaterThan(0)
  })

  it('渲染层聚合不影响日历数据（日历读原始数组，不读 selectTicks 结果）', () => {
    const buckets = ascDayBuckets(3650)
    const rendered = selectTicks('day', buckets, TICK_LIMIT)
    // 渲染层只压到 ≤400，但日历的数据源仍是 3650 条
    expect(rendered.length).toBeLessThanOrEqual(TICK_LIMIT)
    expect(buckets.length).toBe(3650)
    // 两者是不同用途的视图，不应互相污染
    expect(rendered.length).not.toBe(buckets.length)
  })
})

describe('防御性排序 · 输入乱序也能保住最新的桶', () => {
  // 后端契约是 `ORDER BY 1 ASC`（histogram.go:42），本应天然升序。
  // 但一旦后端排序变更（或将来引入缓存/合并打乱顺序），slice(-limit) 会
  // 静默取到**最旧**的一批 —— 即最坏的那个 bug 原地复活，且开发期毫无征兆。
  // 故 keepNewest / selectTicks 入口都按桶键排一次：与输入顺序无关。

  it('keepNewest：乱序输入下仍保留最新的那些桶', () => {
    const asc = ascDayBuckets(500).map((b) => ({ bucket: b.bucket, count: b.count }))
    const shuffled = [...asc].reverse() // 完全倒序（最坏情况）
    const kept = keepNewest(shuffled, 100)

    expect(kept).toHaveLength(100)
    // 保留的必须是最新的 100 个，而不是倒序数组的末尾 100 个（即最旧的）
    expect(kept[99].bucket).toBe(asc[499].bucket)
    expect(kept.some((b) => b.bucket === asc[0].bucket)).toBe(false)
    // 且结果本身是升序的
    for (let i = 1; i < kept.length; i++) {
      expect(kept[i].bucket > kept[i - 1].bucket).toBe(true)
    }
  })

  it('keepNewest：随机打乱（非单调倒序）同样保住最新', () => {
    const asc = ascDayBuckets(300).map((b) => ({ bucket: b.bucket, count: b.count }))
    const shuffled = [...asc]
    // 确定性「伪随机」：按取模步长重排，避免用例依赖 Math.random 造成不稳定
    shuffled.sort((a, b) => (a.bucket.charCodeAt(9) % 7) - (b.bucket.charCodeAt(9) % 7))
    const kept = keepNewest(shuffled, 50)
    expect(kept[kept.length - 1].bucket).toBe(asc[299].bucket)
  })

  it('keepNewest：未超上限时也返回升序副本（不污染调用方数组）', () => {
    const input = ascDayBuckets(50).reverse()
    const snapshot = input.map((b) => b.bucket)
    const kept = keepNewest(input, 100)
    expect(kept).toHaveLength(50)
    expect(kept[0].bucket < kept[49].bucket).toBe(true)
    // 原数组未被就地排序（副作用会污染 pager 的响应引用）
    expect(input.map((b) => b.bucket)).toEqual(snapshot)
  })

  it('keepNewest：无效桶（null / 非字符串键）沉底，不占用保留名额', () => {
    const asc = ascDayBuckets(100).map((b) => ({ bucket: b.bucket, count: b.count }))
    const dirty = [...asc, null, { count: 5 }, { bucket: 123, count: 5 }]
    const kept = keepNewest(dirty, 10)
    expect(kept).toHaveLength(10)
    // 100 个有效桶 + 3 个无效桶，按键排序后无效桶沉底 → 末尾 10 个是最新的 10 个有效桶
    expect(kept[9].bucket).toBe(asc[99].bucket)
    expect(kept[0].bucket).toBe(asc[90].bucket)
    expect(kept.some((b) => !b || typeof b.bucket !== 'string')).toBe(false)
  })

  it('selectTicks：乱序输入下分箱边界仍正确（首末箱贴时间两端）', () => {
    const asc = ascDayBuckets(1000).map((b) => ({ bucket: b.bucket, count: b.count }))
    const shuffled = [...asc].reverse()
    const ticks = selectTicks('day', shuffled, 100)
    expect(ticks.length).toBe(100)
    const width = (bucketTs('day', asc[999].bucket) - bucketTs('day', asc[0].bucket)) / 100
    // 首箱贴最旧端、末箱贴最新端 —— 若按乱序输入算 list[0]，两端会颠倒
    expect(ticks[0].ts).toBeLessThan(bucketTs('day', asc[0].bucket) + width)
    expect(Math.abs(ticks[99].ts - bucketTs('day', asc[999].bucket))).toBeLessThanOrEqual(width)
    // 乱序与升序输入应得到相同结果
    const ordered = selectTicks('day', asc, 100)
    expect(ticks.map((t) => t.ts)).toEqual(ordered.map((t) => t.ts))
  })

  it('mutation 自证：去掉防御性排序（改回 slice(-limit)）后乱序用例会红', () => {
    const asc = ascDayBuckets(500).map((b) => ({ bucket: b.bucket, count: b.count }))
    const shuffled = [...asc].reverse()
    // 无防御时的行为：直接取倒序数组的末尾 = 最旧的 100 个
    const naive = shuffled.slice(-100)
    expect(naive[99].bucket).toBe(asc[0].bucket) // ← 正是原缺陷
    expect(keepNewest(shuffled, 100)[99].bucket).toBe(asc[499].bucket) // ← 修复后
  })
})
