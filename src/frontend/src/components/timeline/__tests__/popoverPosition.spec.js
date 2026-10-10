import { describe, it, expect } from 'vitest'
import { computePopoverPosition } from '../popoverPosition'

// 视口坐标系：触发按钮矩形四项（top/left/right/bottom）
const btn = (left, top, width = 80, height = 34) => ({
  left,
  top,
  width,
  height,
  right: left + width,
  bottom: top + height
})
const vp = (width = 1440, height = 900) => ({ width, height })
const panel = (width = 308, height = 370) => ({ width, height })

describe('浮层定位 · 正常情况', () => {
  it('下方空间充足时：贴在按钮下方、左缘对齐', () => {
    const r = computePopoverPosition({
      trigger: btn(300, 100),
      panel: panel(),
      viewport: vp()
    })
    expect(r.placement).toBe('bottom')
    expect(r.top).toBe(142)   // 100 + 34(button高) + 8(gap)
    expect(r.left).toBe(300)
  })

  it('自定义 gap 生效', () => {
    const r = computePopoverPosition({
      trigger: btn(300, 100),
      panel: panel(),
      viewport: vp(),
      gap: 16
    })
    expect(r.top).toBe(150)
  })
})

describe('浮层定位 · 垂直翻转（用户报的遮挡场景）', () => {
  it('按钮靠近视口底部且下方放不下时：翻到按钮上方', () => {
    // 按钮 top=700 bottom=734，视口高 900
    //   下方 = 900-734 = 166，装不下 370+8=378 → 必须翻
    //   上方 = 700，宽裕 → 翻上去后 top = 700-370-8 = 322，完全在视口内
    const r = computePopoverPosition({
      trigger: btn(300, 700),
      panel: panel(),
      viewport: vp()
    })
    expect(r.placement).toBe('top')
    expect(r.top).toBe(700 - 370 - 8)   // 322
    expect(r.top).toBeGreaterThanOrEqual(0)
  })

  it('上方也不宽裕时**不翻转**——宁可贴着按钮下方，也不把面板推到屏幕外', () => {
    // 按钮 top=12：上方仅 12px，下方 374px（装不下 378）
    // 翻上去 top = 12-378 = -366，面板几乎全在视口外 → 属更糟的失败
    const r = computePopoverPosition({
      trigger: btn(300, 12),
      panel: panel(),
      viewport: vp(1440, 420)
    })
    expect(r.placement).toBe('bottom')
    // 断言性质而非常数：面板完整落在视口内，且上下各留>= margin
    expect(r.top).toBeGreaterThanOrEqual(8)
    expect(r.top + 370).toBeLessThanOrEqual(420 - 8)
  })

  it('上下都放不下时：夹紧到顶部留白，保证标题与翻月按钮可见', () => {
    const r = computePopoverPosition({
      trigger: btn(300, 20),
      panel: panel(308, 800),
      viewport: vp(1440, 500)
    })
    expect(r.top).toBe(8)     // margin，顶部不被切
    expect(r.top).toBeGreaterThanOrEqual(8)
  })
})

describe('浮层定位 · 水平夹紧（贴边而非被切掉）', () => {
  it('按钮靠右：浮层左缘被夹到视口内，不会右半截跑出屏幕', () => {
    const r = computePopoverPosition({
      trigger: btn(1400, 100),   // 右缘 1480 > 1440
      panel: panel(),
      viewport: vp()
    })
    expect(r.left).toBe(1440 - 308 - 8)   // 1124
    expect(r.left + 308).toBeLessThanOrEqual(1440 - 8)
  })

  it('按钮靠左：夹到左边距，不贴死屏幕边缘', () => {
    const r = computePopoverPosition({
      trigger: btn(2, 100),
      panel: panel(),
      viewport: vp()
    })
    expect(r.left).toBe(8)
  })

  it('面板比视口还宽时：不会算出负坐标', () => {
    const r = computePopoverPosition({
      trigger: btn(700, 100),
      panel: panel(1600, 300),
      viewport: vp(1440, 900)
    })
    expect(r.left).toBe(8)
    expect(r.left).toBeGreaterThanOrEqual(0)
  })
})

describe('浮层定位 · 输出可直接进 style', () => {
  it('返回整数，避免 style 里出现小数像素抖动', () => {
    const r = computePopoverPosition({
      trigger: btn(300.4, 100.6),
      panel: panel(),
      viewport: vp()
    })
    expect(Number.isInteger(r.top)).toBe(true)
    expect(Number.isInteger(r.left)).toBe(true)
  })

  it('placement 只取 bottom / top 两种值', () => {
    for (const t of [btn(300, 10), btn(300, 400), btn(1200, 100)]) {
      const r = computePopoverPosition({ trigger: t, panel: panel(), viewport: vp() })
      expect(['bottom', 'top']).toContain(r.placement)
    }
  })
})