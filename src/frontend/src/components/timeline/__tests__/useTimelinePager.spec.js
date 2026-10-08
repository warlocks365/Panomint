import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { TICK_LIMIT } from '../histogramBudget'

// Job000145 缺陷回归 · 第二层守护：**pager 真实存储路径**。
//
// 为什么必须有这层：histogramBudget.spec.js 直接测 keepNewest 这个纯函数，
// 但它**测不到调用方**。实测证明：把 useTimelinePager 的存储层改回
// `list.slice(0, TICK_LIMIT)`（即原缺陷形态）时，那 141 条用例**全绿** ——
// 因为没有任何一条用例真正跑过 pager。
//
// 纯函数测试的盲区就在这里：函数本身没坏，坏的是「谁用它、按什么预算用」。
// 故此处 mock http.get，直接驱动 loadHistogram，断言日历侧真正拿到的数据。

// 两个易踩的坑，都曾让本文件「mock 看似写了却没生效」：
//
// 1) **路径**：本文件在 __tests__/ 下（多一层目录），要mock 的
//    src/api/http 需上跳**三**级 ../../../api/http。写成 ../../ 时解析到
//    不存在的 src/components/api/http，mock 注册在幽灵路径上，
//    真实 http.js 照常被加载 → 其 tokenStore 读localStorage 报
//    「localStorage is not defined」→ 又被 loadHistogram 的 catch 吞掉，
//    症状是「调用次数 0、数据为空」，极具误导性。
// 2) **提升**：vi.mock 被提升到文件顶部，工厂若闭包引用普通 const 声明的
//    mockGet，会在注册时撞 TDZ。故用 vi.hoisted 声明。
const { mockGet } = vi.hoisted(() => ({ mockGet: vi.fn() }))

vi.mock('../../../api/http', () => ({
  default: { get: (...args) => mockGet(...args) }
}))

import { useTimelinePager } from '../useTimelinePager'

/** 造升序桶（严格模拟后端 ORDER BY 1 ASC），n 天连续 */
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

function makePager() {
  return useTimelinePager({ type: '', favorites: false, place: '' })
}

beforeEach(() => {
  mockGet.mockReset()
})

describe('pager 存储层：day 档必须保留完整数据', () => {
  it('3650 个升序 day 桶 → pager 存全量，最新桶可被日历命中', async () => {
    const buckets = ascDayBuckets(3650)
    mockGet.mockResolvedValue({ data: buckets })
    const pager = makePager()
    await pager.loadHistogram('day')

    expect(buckets.length).toBeGreaterThan(TICK_LIMIT)
    // 全量存储（未按渲染预算截断）
    expect(pager.histograms.day).toHaveLength(3650)
    // 日历侧用这些桶建 counts，末桶必须可命中
    const set = new Set(pager.histograms.day.map((b) => b.bucket))
    expect(set.has(buckets[3649].bucket)).toBe(true)
    expect(set.has(buckets[0].bucket)).toBe(true)
  })

  it('mutation 自证：若改回 slice(0, TICK_LIMIT)，最新桶消失 —— 本用例应变红', async () => {
    const buckets = ascDayBuckets(3650)
    mockGet.mockResolvedValue({ data: buckets })
    const pager = makePager()
    await pager.loadHistogram('day')

    const set = new Set(pager.histograms.day.map((b) => b.bucket))
    const newest = buckets[3649].bucket
    // 反向断言「原缺陷形态确实错」：截断到 400 后最新桶不可命中
    expect(buckets.slice(0, TICK_LIMIT).some((b) => b.bucket === newest)).toBe(false)
    expect(set.has(newest)).toBe(true)
  })

  it('unknown 桶被剔除且单独计数（不进日历数据，AC-11）', async () => {
    const buckets = [...ascDayBuckets(10), { bucket: 'unknown', count: 9 }]
    mockGet.mockResolvedValue({ data: buckets })
    const pager = makePager()
    await pager.loadHistogram('day')

    expect(pager.histograms.day).toHaveLength(10)
    expect(pager.histograms.day.some((b) => b.bucket === 'unknown')).toBe(false)
    expect(pager.unknownCount.value).toBe(9)
  })

  it('请求失败 → 空数组且不抛错（滑块隐藏，主时间流不受影响）', async () => {
    mockGet.mockRejectedValue(new Error('network'))
    const pager = makePager()
    await pager.loadHistogram('day')
    expect(pager.histograms.day).toEqual([])
    expect(pager.unknownCount.value).toBe(0)
  })

  it('day 档请求显式带 space 与 granularity（K2 泄漏防线）', async () => {
    mockGet.mockResolvedValue({ data: [] })
    const pager = makePager()
    await pager.loadHistogram('day')
    const url = mockGet.mock.calls[0][0]
    const params = mockGet.mock.calls[0][1].params
    expect(url).toBe('/media/date-histogram')
    expect(params.granularity).toBe('day')
    expect(params.space).toBe('personal') //漏传会回落到缺省作用域分支
  })

  it('非法档位不发请求', async () => {
    mockGet.mockResolvedValue({ data: [] })
    const pager = makePager()
    await pager.loadHistogram('hour')
    expect(mockGet).not.toHaveBeenCalled()
  })
})

describe('pager 切档：零网络请求（AC-04）', () => {
  it('loadHistograms 一次取齐三档后，反复 setDimension 不再发请求', async () => {
    mockGet.mockResolvedValue({ data: ascDayBuckets(30) })
    const pager = makePager()
    await pager.loadHistograms()
    const afterLoad = mockGet.mock.calls.length
    expect(afterLoad).toBe(3)

    pager.setDimension('year')
    await nextTick()
    pager.setDimension('day')
    await nextTick()
    pager.setDimension('month')
    await nextTick()
    // 切档只改本地取数来源
    expect(mockGet.mock.calls.length).toBe(afterLoad)
  })

  it('histogram 随档位切换指向不同数据集，且各档数据未被截断', async () => {
    const day = ascDayBuckets(30)
    const month = [
      { bucket: '2024-04', count: 3 },
      { bucket: '2024-05', count: 5 }
    ]
    const year = [{ bucket: '2023', count: 1 }, { bucket: '2024', count: 2 }]
    mockGet.mockImplementation((url, cfg) => {
      const g = cfg.params.granularity
      return Promise.resolve({ data: g === 'day' ? day : g === 'month' ? month : year })
    })
    const pager = makePager()
    await pager.loadHistograms()

    pager.setDimension('day')
    await nextTick()
    expect(pager.histogram.value).toHaveLength(30)

    pager.setDimension('month')
    await nextTick()
    expect(pager.histogram.value).toHaveLength(2)

    pager.setDimension('year')
    await nextTick()
    expect(pager.histogram.value).toHaveLength(2)
    expect(pager.histogram.value[0].bucket).toBe('2023')
  })

  it('非法档位的 setDimension 被忽略（不污染当前档位）', async () => {
    mockGet.mockResolvedValue({ data: [] })
    const pager = makePager()
    await pager.loadHistogram('month')
    pager.setDimension('hour')
    await nextTick()
    expect(pager.dimension.value).toBe('month')
  })
})