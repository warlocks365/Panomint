// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import DatePickerPopover from '../DatePickerPopover.vue'
import { headerKeyOf } from '../timelineDimensions'

// 日历四态（AC-09/10/11/12）。这些是最容易被「视觉优化」顺手改坏的规则：
// 把未来日期改成可点、把unknown 桶画成圆点、或用颜色单独传达「有照片」，
// 都不会报错，只是产品语义悄悄变了。故用测试钉住。

function daysAgo(n) {
  const d = new Date()
  d.setDate(d.getDate() - n)
  return headerKeyOf('day', d)
}

function mountCal(props = {}) {
  return mount(DatePickerPopover, {
    props: { buckets: [], unknownCount: 0, loading: false, anchorKey: '', ...props },
    attachTo: document.body
  })
}

describe('DatePickerPopover · 基本形态', () => {
  it('是自建面板而非原生 input[type=date]', () => {
    const w = mountCal()
    expect(w.find('input[type="date"]').exists()).toBe(false)
    expect(w.find('[role="dialog"]').exists()).toBe(true)
    expect(w.find('[role="grid"]').exists()).toBe(true)
  })

  it('固定 6 行 × 7 列 = 42 个日期格', () => {
    const w = mountCal()
    expect(w.findAll('[role="gridcell"]')).toHaveLength(42)
  })

  it('星期行以周一为首列', () => {
    const w = mountCal()
    expect(w.find('.cg-week').text()).toBe('一二三四五六日')
  })
})

describe('AC-09 · 有照片的日期为可选态且带主色圆点', () => {
  it('count > 0 的日期可点、无 disabled，且 data-has="photo"', () => {
    const withPhoto = daysAgo(3)
    const w = mountCal({ buckets: [{ bucket: withPhoto, count: 5 }] })
    const cells = w.findAll('[role="gridcell"]').filter((c) => c.attributes('data-has') === 'photo')
    expect(cells).toHaveLength(1)
    expect(cells[0].attributes('disabled')).toBeUndefined()
    expect(cells[0].attributes('aria-disabled')).toBe('false')
    // 圆点只由 CSS ::after 画，DOM 里没有多余的图形节点
    expect(cells[0].find('svg').exists()).toBe(false)
  })

  it('aria-label 写明数量（不依赖颜色单独传达信息，WCAG 1.4.1）', () => {
    const k = daysAgo(3)
    const w = mountCal({ buckets: [{ bucket: k, count: 7 }] })
    const cell = w.findAll('[role="gridcell"]').find((c) => c.attributes('data-has') === 'photo')
    expect(cell.attributes('aria-label')).toContain('有 7 项照片')
  })

  it('圆点由 CSS ::after 绘制（data-has 驱动），非内联 SVG 元素', () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(3), count: 1 }] })
    const cell = w.findAll('[role="gridcell"]').find((c) => c.attributes('data-has') === 'photo')
    expect(cell.attributes('data-has')).toBe('photo')
    // 格内只有数字文本，没有 svg / img 图标节点
    expect(cell.find('svg').exists()).toBe(false)
    expect(cell.text()).toMatch(/^\d+ 日$/)
  })
})

describe('AC-10 · 无照片与未来日期不可跳转', () => {
  it('无照片的过去日：灰显 + aria-disabled + disabled', () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(3), count: 1 }] })
    const empties = w.findAll('[role="gridcell"]').filter((c) => c.attributes('data-has') === 'none')
    const inMonthPast = empties.filter((c) => c.attributes('aria-disabled') === 'true')
    expect(inMonthPast.length).toBeGreaterThan(0)
    for (const c of inMonthPast) expect(c.attributes('disabled')).toBeDefined()
  })

  it('点击无照片日期不触发 seek（Enter/Space/点击均无效）', async () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(3), count: 1 }] })
    const emptyCell = w
      .findAll('[role="gridcell"]')
      .find((c) => c.attributes('data-has') === 'none' && c.attributes('aria-disabled') === 'true')
    await emptyCell.trigger('click')
    await emptyCell.trigger('keydown', { key: 'Enter' })
    await emptyCell.trigger('keydown', { key: ' ' })
    expect(w.emitted('seek')).toBeUndefined()
  })

  it('未来日期全部禁用（不渲染圆点，aria-label 写明未来）', () => {
    const future = new Date()
    future.setDate(future.getDate() + 5)
    const w = mountCal({ buckets: [{ bucket: daysAgo(3), count: 1 }, { bucket: headerKeyOf('day', future), count: 4 }] })
    const cell = w
      .findAll('[role="gridcell"]')
      .find((c) => c.attributes('aria-label')?.includes('未来日期'))
    expect(cell).toBeTruthy()
    // 未来日期即便有count 也不可选、不带圆点
    expect(cell.attributes('data-has')).toBe('none')
    expect(cell.attributes('disabled')).toBeDefined()
  })

  it('跨月补位格不可选且无键', () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(3), count: 1 }] })
    const outs = w.findAll('[role="gridcell"]').filter((c) => c.text().length && c.attributes('aria-disabled') === 'true')
    expect(outs.length).toBeGreaterThan(0)
    const outLabels = w.findAll('[role="gridcell"]').map((c) => c.attributes('aria-label'))
    expect(outLabels.some((l) => l.includes('不在本月'))).toBe(true)
  })
})

describe('AC-11 · unknown 桶不渲染标记也不可跳转', () => {
  it('unknown 不进格子的可选集合', () => {
    const w = mountCal({
      buckets: [{ bucket: 'unknown', count: 9 }, { bucket: daysAgo(2), count: 3 }],
      unknownCount: 9
    })
    // 没有任何格以 unknown 为键
    expect(w.findAll('[role="gridcell"]').some((c) => c.attributes('data-has') === 'photo' && c.text() === 'unknown')).toBe(false)
    // 如实报出数量，不静默消失
    expect(w.text()).toContain('有 9 项媒体没有拍摄日期，无法在此定位')
  })

  it('unknownCount 为 0 时不显示该提示', () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(2), count: 3 }], unknownCount: 0 })
    expect(w.text()).not.toContain('没有拍摄日期')
  })
})

describe('AC-12 · 跳转载荷与焦点管理', () => {
  it('选中日期后点「查看这一天」发出 { dimension, key } 对象载荷', async () => {
    const k = daysAgo(4)
    const w = mountCal({ buckets: [{ bucket: k, count: 2 }] })
    const cell = w.findAll('[role="gridcell"]').find((c) => c.attributes('data-has') === 'photo')
    await cell.trigger('click')
    await w.findAll('.dp-act')[0].trigger('click')
    expect(w.emitted('seek')).toEqual([[{ dimension: 'day', key: k }]])
  })

  it('未选日期时「查看这一天」禁用（不发空载荷）', async () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(4), count: 2 }] })
    expect(w.findAll('.dp-act')[0].attributes('disabled')).toBeDefined()
    await w.findAll('.dp-act')[0].trigger('click')
    expect(w.emitted('seek')).toBeUndefined()
  })

  it('Esc 发出 close（焦点归还在宿主处理）', async () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(4), count: 2 }] })
    await w.find('[role="dialog"]').trigger('keydown', { key: 'Esc' })
    expect(w.emitted('close')).toBeTruthy()
  })

  it('当前锚点日带 data-anchor="true" 与 aria-selected', () => {
    const k = daysAgo(4)
    const w = mountCal({ buckets: [{ bucket: k, count: 2 }], anchorKey: k })
    const cell = w.findAll('[role="gridcell"]').find((c) => c.attributes('data-anchor') === 'true')
    expect(cell).toBeTruthy()
    expect(cell.attributes('aria-selected')).toBe('true')
    expect(cell.text()).toBe(`${Number(k.slice(8))} 日`)  // 日期格已补中文单位「日」
  })
})

describe('空状态与加载态（Design §2.5 五态）', () => {
  it('本月无照片 → 具体文案 + 图标，不用「暂无数据」裸文案', () => {
    const w = mountCal({ buckets: [] })
    expect(w.text()).toContain('这个月没有照片')
    expect(w.text()).not.toContain('暂无数据')
    expect(w.find('.dp-note svg').exists()).toBe(true)
  })

  it('loading → 骨架而非 spinner，且无日期格可点', () => {
    const w = mountCal({ loading: true, buckets: [{ bucket: daysAgo(1), count: 9 }] })
    expect(w.find('.cg-skeleton').exists()).toBe(true)
    expect(w.findAll('[role="gridcell"]')).toHaveLength(0)
    // 禁圆形 spinner：骨架里只有一个 2px 进度条
    expect(w.findAll('svg').filter((s) => s.classes().includes('spinner')).length).toBe(0)
  })

  it('翻月按钮在无数据时被禁用（不给出空白未来月份）', () => {
    const w = mountCal({ buckets: [] })
    expect(w.findAll('.nav-step')[0].attributes('disabled')).toBeDefined()
    expect(w.findAll('.nav-step')[1].attributes('disabled')).toBeDefined()
  })

  it('有数据时可翻到上一月，且不得翻到当前月之后', async () => {
    const w = mountCal({ buckets: [{ bucket: daysAgo(200), count: 3 }, { bucket: daysAgo(2), count: 1 }] })
    const navs = w.findAll('.nav-step')
    expect(navs[0].attributes('disabled')).toBeUndefined() // 可往前
    expect(navs[1].attributes('disabled')).toBeDefined() // 不可超过当前月
  })
})