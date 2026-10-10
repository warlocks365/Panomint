// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { ref } from 'vue'
import { mount } from '@vue/test-utils'
import CalendarNavBar from '../CalendarNavBar.vue'
import { useCalendarMatrix } from '../useCalendarMatrix'

const day = (offsetDays) => {
  const d = new Date()
  d.setDate(d.getDate() + offsetDays)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

/** 用真实的 useCalendarMatrix 驱动，避免桩对象掩盖状态不一致（本期两类缺陷都出在真状态层） */
function setup(buckets) {
  const m = useCalendarMatrix(ref(buckets))
  const w = mount(CalendarNavBar, { props: { m }, attachTo: document.body })
  return { m, w }
}

const steppers = (w) => w.findAll('.nav-step')

describe('CalendarNavBar · 基本形态', () => {
  it('渲染上一月/下一月两个步进按钮与年/月两个下拉', () => {
    const { w } = setup([])
    expect(steppers(w)).toHaveLength(2)
    expect(w.findAll('.nav-pick')).toHaveLength(2)
    expect(w.text()).toContain('年')
    expect(w.text()).toContain('月')
  })

  it('默认不展示年/月选择网格', () => {
    const { w } = setup([])
    expect(w.find('.nav-grid').exists()).toBe(false)
  })

  it('不是原生控件（沿用自建面板约定）', () => {
    const { w } = setup([])
    expect(w.find('select').exists()).toBe(false)
  })
})

describe('CalendarNavBar · 上下月步进边界（回归：编码统一后不能错位）', () => {
  it('无数据时两个步进都禁用', () => {
    const { w } = setup([])
    expect(steppers(w)[0].attributes('disabled')).toBeDefined()
    expect(steppers(w)[1].attributes('disabled')).toBeDefined()
  })

  it('有数据时可往前、不可超过当前月', () => {
    const { w } = setup([{ bucket: day(-200), count: 3 }, { bucket: day(-2), count: 1 }])
    expect(steppers(w)[0].attributes('disabled')).toBeUndefined()
    expect(steppers(w)[1].attributes('disabled')).toBeDefined()
  })

  it('连续回退 14 个月不产生非法月份（跨年取模）', async () => {
    const { m, w } = setup([{ bucket: day(-30), count: 1 }])
    for (let i = 0; i < 14; i++) {
      await steppers(w)[0].trigger('click')
    }
    expect(m.cursorMonth.value).toBeGreaterThanOrEqual(1)
    expect(m.cursorMonth.value).toBeLessThanOrEqual(12)
  })
})

describe('CalendarNavBar · 年份切换', () => {
  it('点「年」出现年份网格，再点收起', async () => {
    const { w } = setup([{ bucket: day(-400), count: 1 }, { bucket: day(-2), count: 1 }])
    const yearBtn = w.findAll('.nav-pick')[0]
    await yearBtn.trigger('click')
    expect(w.find('[aria-label="选择年份"]').exists()).toBe(true)
    await yearBtn.trigger('click')
    expect(w.find('[aria-label="选择年份"]').exists()).toBe(false)
  })

  it('默认档位落在当前年当前月', () => {
    const { m } = setup([{ bucket: day(-2), count: 1 }])
    expect(m.cursorYear.value).toBe(new Date().getFullYear())
    expect(m.cursorMonth.value).toBe(new Date().getMonth() + 1)
  })

  it('点某年即切换并回到日视图', async () => {
    const { m, w } = setup([{ bucket: day(-400), count: 1 }, { bucket: day(-2), count: 1 }])
    await w.findAll('.nav-pick')[0].trigger('click') // 打开年网格
    const cells = w.findAll('.nav-cell')
    expect(cells.length).toBeGreaterThan(0)
    const target = cells.find((c) => c.text() === String(new Date().getFullYear() - 1))
    expect(target).toBeTruthy()
    expect(target.attributes('disabled')).toBeUndefined()
    await target.trigger('click')
    expect(m.cursorYear.value).toBe(new Date().getFullYear() - 1)
    expect(w.find('[aria-label="选择年份"]').exists()).toBe(false) // 已收起
  })

  it('超出可导航范围的年份被禁用（有照片的最早年 ~ 当前年）', async () => {
    const { m, w } = setup([{ bucket: day(-400), count: 1 }, { bucket: day(-2), count: 1 }])
    await w.findAll('.nav-pick')[0].trigger('click')
    const early = w.findAll('.nav-cell').find((c) => Number(c.text()) < m.minYear.value)
    expect(early).toBeTruthy()
    expect(early.attributes('disabled')).toBeDefined()
  })
})

describe('CalendarNavBar · 月份切换', () => {
  it('点「月」出现 12 个月网格', async () => {
    const { w } = setup([{ bucket: day(-2), count: 1 }])
    await w.findAll('.nav-pick')[1].trigger('click')
    const cells = w.findAll('.nav-cell')
    expect(cells).toHaveLength(12)
  })

  it('2 月标注天数，闰年 29 天 / 平年 28 天', async () => {
    // 游标停在闰年 2024 → 2 月 29 天；平年 2023 → 28 天。
    // 断言走 DOM（先确认网格确有 12 格），不直接摸组件内部状态。
    const { m, w } = setup([{ bucket: day(-2), count: 1 }])
    m.cursorYear.value = 2024
    m.cursorMonth.value = 2
    await w.vm.$nextTick()
    await w.findAll('.nav-pick')[1].trigger('click')
    expect(w.findAll('.nav-cell')).toHaveLength(12)
    expect(w.findAll('.nav-cell')[1].text()).toContain('29')

    await w.findAll('.nav-pick')[1].trigger('click') // 收起
    m.cursorYear.value = 2023
    await w.vm.$nextTick()
    await w.findAll('.nav-pick')[1].trigger('click')
    expect(w.findAll('.nav-cell')).toHaveLength(12)
    expect(w.findAll('.nav-cell')[1].text()).toContain('28')
  })

  it('晚于当前月的月份被禁用（下个月及之后）', async () => {
    const now = new Date()
    const { w } = setup([{ bucket: day(-2), count: 1 }])
    await w.findAll('.nav-pick')[1].trigger('click')
    const cells = w.findAll('.nav-cell')
    expect(cells).toHaveLength(12)
    // 当前月为 1-based 的 getMonth()+1，故「下个月」的格子下标 = getMonth()+1
    const nextMonthIdx = now.getMonth() + 1
    if (nextMonthIdx < 12) {
      expect(cells[nextMonthIdx].attributes('disabled')).toBeDefined()
    }
    // 当前月本身必定可点（否则无法回到今天）
    expect(cells[now.getMonth()].attributes('disabled')).toBeUndefined()
  })

  it('点某月即切换并收起', async () => {
    const { m, w } = setup([{ bucket: day(-400), count: 1 }, { bucket: day(-2), count: 1 }])
    await w.findAll('.nav-pick')[1].trigger('click')
    const cells = w.findAll('.nav-cell')
    const target = cells.find((c) => !c.attributes('disabled') && c.text().startsWith('6'))
    if (target) {
      await target.trigger('click')
      expect(m.cursorMonth.value).toBe(6)
      expect(w.find('[aria-label="选择月份"]').exists()).toBe(false)
    }
  })
})

describe('CalendarNavBar · Esc 两级收起', () => {
  it('展开时 collapse 收起选择层；再展开仍可收起', async () => {
    const { w } = setup([{ bucket: day(-2), count: 1 }])
    expect(w.find('.nav-grid').exists()).toBe(false)
    await w.findAll('.nav-pick')[0].trigger('click')
    expect(w.find('.nav-grid').exists()).toBe(true)

    // collapse() 改的是 ref，DOM 要等下一轮渲染才更新 —— 断言前必须 await
    expect(w.vm.collapse()).toBe(true)
    await w.vm.$nextTick()
    expect(w.find('.nav-grid').exists()).toBe(false)

    // 再开一次仍能收起 —— 防止 collapse 只对首次生效
    await w.findAll('.nav-pick')[0].trigger('click')
    expect(w.find('.nav-grid').exists()).toBe(true)
    expect(w.vm.collapse()).toBe(true)
    await w.vm.$nextTick()
    expect(w.find('.nav-grid').exists()).toBe(false)
  })
})