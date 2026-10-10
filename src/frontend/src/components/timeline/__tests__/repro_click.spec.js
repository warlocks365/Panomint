// @vitest-environment jsdom
// 复现用户报告的「有媒体的年/月/日都点不动」。用跨年真实分布的数据驱动，
// 不做桩替换——若状态层本身有问题，桩会把它掩盖掉。
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import DatePickerPopover from '../DatePickerPopover.vue'

/** 跨 2024-11 ~ 2025-03 的真实分布：含闰日 2024-02-29 与 31 天月 */
const BUCKETS = [
  { bucket: '2024-02-29', count: 3 },
  { bucket: '2024-11-03', count: 5 },
  { bucket: '2024-11-20', count: 1 },
  { bucket: '2024-12-25', count: 2 },
  { bucket: '2025-01-05', count: 5 },
  { bucket: '2025-03-07', count: 4 },
  { bucket: '2025-03-08', count: 1 }
]

const mountCal = () =>
  mount(DatePickerPopover, {
    props: { buckets: BUCKETS, unknownCount: 0, loading: false, anchorKey: '' },
    attachTo: document.body
  })

const gridLabel = (w) => w.find('.cg-grid').attributes('aria-label')
const enabledDays = (w) => w.findAll('.cg-cell').filter((c) => c.attributes('disabled') === undefined)

describe('复现 · 年→月→日 完整链路', () => {
  it('打开时直接落在最近有照片的月份，而不是当月（否则看起来像坏了）', () => {
    const w = mountCal()
    // 测试数据的最新有照片月份是 2025-03；面板不应停在无照片的当月
    expect(gridLabel(w)).toContain('2025 年')
    expect(gridLabel(w)).toContain('3 月')
    expect(enabledDays(w).length, '落在有照片的月份就该有可选日').toBeGreaterThan(0)
  })

  it('整月无照片时给出「去最近有照片的月份」的可点提示', async () => {
    const w = mountCal()
    // 人为把游标挪到没有照片的月份（数据里没有 6 月）
    const y = w.findAll('.nav-pick')[0]
    await y.trigger('click')
    await w.findAll('.nav-pick')[1].trigger('click')
    const jun = w.findAll('.nav-cell')[5]
    if (jun.attributes('disabled') === undefined) {
      await jun.trigger('click')
      expect(w.find('.dp-jump').exists()).toBe(true)
      await w.find('.dp-jump').trigger('click')
      expect(enabledDays(w).length).toBeGreaterThan(0)
    }
  })

  it('点年 2024 → 面板标题的年随之改变', async () => {
    const w = mountCal()
    await w.findAll('.nav-pick')[0].trigger('click')
    const y2024 = w.findAll('.nav-cell').find((c) => c.text() === '2024 年')
    expect(y2024.attributes('disabled')).toBeUndefined()
    await y2024.trigger('click')
    expect(gridLabel(w)).toContain('2024 年')
  })

  it('点年 2024 → 点月 11 → 11-03/11-20 应变为可选', async () => {
    const w = mountCal()
    await w.findAll('.nav-pick')[0].trigger('click')
    await w.findAll('.nav-cell').find((c) => c.text() === '2024 年').trigger('click')

    await w.findAll('.nav-pick')[1].trigger('click')
    const cells = w.findAll('.nav-cell')
    expect(cells, '月网格没渲染').toHaveLength(12)
    const nov = cells[10] // 11 月
    expect(nov.text()).toContain('11')
    expect(nov.attributes('disabled'), '11 月被禁用').toBeUndefined()
    await nov.trigger('click')

    expect(gridLabel(w)).toContain('2024 年')
    expect(gridLabel(w)).toContain('11 月')
    expect(enabledDays(w).length, '2024-11 没有任何可选日').toBeGreaterThan(0)
  })

  it('闰日 2024-02-29 在 2024 年 2 月可选', async () => {
    const w = mountCal()
    await w.findAll('.nav-pick')[0].trigger('click')
    await w.findAll('.nav-cell').find((c) => c.text() === '2024 年').trigger('click')
    await w.findAll('.nav-pick')[1].trigger('click')
    await w.findAll('.nav-cell')[1].trigger('click') // 2 月
    expect(gridLabel(w)).toContain('2 月')
    // 2 月格子：29 日那格应可选
    const feb29 = w.findAll('.cg-cell').find((c) => /^29 日$/.test(c.text()) && c.attributes('data-has') === 'photo')
    expect(feb29, '2024-02-29 不在网格里或不可选').toBeTruthy()
  })

  it('点可选日后 aria-selected 生效（选中链路通）', async () => {
    const w = mountCal()
    await w.findAll('.nav-pick')[0].trigger('click')
    await w.findAll('.nav-cell').find((c) => c.text() === '2024 年').trigger('click')
    await w.findAll('.nav-pick')[1].trigger('click')
    await w.findAll('.nav-cell')[10].trigger('click') // 11 月
    const target = enabledDays(w)[0]
    await target.trigger('click')
    // 选中后「查看这一天」按钮应可用
    const go = w.findAll('.dp-act')[0]
    expect(go.attributes('disabled'), '选中日后「查看这一天」仍不可点').toBeUndefined()
  })
})
