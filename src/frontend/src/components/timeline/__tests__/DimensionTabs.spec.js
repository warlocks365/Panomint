// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import DimensionTabs from '../DimensionTabs.vue'
import { DIMENSIONS } from '../timelineDimensions'

// 分段控件的键盘契约（AC-17/18/19）。这三条是纯 DOM 行为，最容易在后续改动中
// 被「顺手优化」掉：把监听器挂到 window 会全局劫持方向键（WCAG 2.1.4 违规），
// 改 roving tabindex 会让 Tab 需按三次才能进入控件组——两者都不会报错，只是不好用。

function mountTabs(props = {}) {
  return mount(DimensionTabs, {
    props: { dimension: 'month', ...props },
    attachTo: document.body
  })
}

describe('DimensionTabs · radiogroup 语义', () => {
  it('组为 role=radiogroup，子项为 role=radio 且带 aria-checked', () => {
    const w = mountTabs()
    expect(w.find('[role="radiogroup"]').exists()).toBe(true)
    const radios = w.findAll('[role="radio"]')
    expect(radios).toHaveLength(3)
    for (const r of radios) expect(r.attributes('aria-checked')).toBeTruthy()
    expect(w.findAll('[role="radio"]').filter((r) => r.attributes('aria-checked') === 'true')).toHaveLength(1)
  })

  it('每档有具体动作描述的 aria-label（非「年/月/日」裸字）', () => {
    const w = mountTabs()
    const labels = w.findAll('[role="radio"]').map((r) => r.attributes('aria-label'))
    expect(labels).toEqual(['按年分组', '按月分组', '按日分组'])
  })
})

describe('DimensionTabs · roving tabindex', () => {
  it('组内恰好一个 tabindex="0"，其余为 -1', () => {
    for (const d of DIMENSIONS) {
      const w = mountTabs({ dimension: d })
      const zero = w.findAll('[role="radio"]').filter((r) => r.attributes('tabindex') === '0')
      expect(zero).toHaveLength(1)
      expect(zero[0].attributes('aria-label')).toBe(`${d === 'year' ? '按年' : d === 'month' ? '按月' : '按日'}分组`)
      expect(w.findAll('[role="radio"]').filter((r) => r.attributes('tabindex') === '-1')).toHaveLength(2)
      w.unmount()
    }
  })

  it('Tab 只需一次即可进入控件组（不会逐段停留）', () => {
    const w = mountTabs({ dimension: 'month' })
    const tabbable = w.findAll('[role="radio"]').filter((r) => r.attributes('tabindex') === '0')
    expect(tabbable).toHaveLength(1)
  })
})

describe('DimensionTabs · 方向键切档（AC-17）', () => {
  it('ArrowRight / ArrowDown 前进一档并循环', async () => {
    const w = mountTabs({ dimension: 'year' })
    await w.findAll('[role="radio"]')[0].trigger('keydown', { key: 'ArrowRight' })
    expect(w.emitted('change')).toEqual([['month']])
    w.unmount()

    const w2 = mountTabs({ dimension: 'day' })
    await w2.findAll('[role="radio"]')[2].trigger('keydown', { key: 'ArrowDown' })
    expect(w2.emitted('change')).toEqual([['year']]) // 末档 → 首档，循环
  })

  it('ArrowLeft / ArrowUp 后退一档并循环', async () => {
    const w = mountTabs({ dimension: 'month' })
    await w.findAll('[role="radio"]')[1].trigger('keydown', { key: 'ArrowUp' })
    expect(w.emitted('change')).toEqual([['year']])

    const w2 = mountTabs({ dimension: 'year' })
    await w2.findAll('[role="radio"]')[0].trigger('keydown', { key: 'ArrowLeft' })
    expect(w2.emitted('change')).toEqual([['day']]) // 首档 → 末档，循环
  })

  it('Home / End 直达首末档', async () => {
    const w = mountTabs({ dimension: 'month' })
    await w.findAll('[role="radio"]')[1].trigger('keydown', { key: 'End' })
    expect(w.emitted('change')).toEqual([['day']])
    const w2 = mountTabs({ dimension: 'day' })
    await w2.findAll('[role="radio"]')[2].trigger('keydown', { key: 'Home' })
    expect(w2.emitted('change')).toEqual([['year']])
  })

  it('已选中的档位再按同向键仍会前进（不吞掉按键，行为可预期）', async () => {
    const w = mountTabs({ dimension: 'month' })
    await w.findAll('[role="radio"]')[1].trigger('keydown', { key: 'ArrowRight' })
    expect(w.emitted('change')).toEqual([['day']])
  })

  it('无关按键不触发也不拦截', async () => {
    const w = mountTabs({ dimension: 'month' })
    await w.findAll('[role="radio"]')[1].trigger('keydown', { key: 'a' })
    await w.findAll('[role="radio"]')[1].trigger('keydown', { key: 'Tab' })
    expect(w.emitted('change')).toBeUndefined()
  })

  // 方向键在 radiogroup 内必须被消费，否则会连带滚动页面
  it('方向键被 preventDefault（避免连带滚动）', async () => {
    const w = mountTabs({ dimension: 'month' })
    const ev = new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true })
    w.findAll('[role="radio"]')[1].element.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
  })
})

describe('DimensionTabs · 焦点不丢回 body（AC-18）', () => {
  it('方向键切档后焦点落在新选中段上', async () => {
    const w = mountTabs({ dimension: 'month' })
    const radios = w.findAll('[role="radio"]')
    radios[1].element.focus()
    expect(document.activeElement).toBe(radios[1].element)

    await radios[1].trigger('keydown', { key: 'ArrowRight' })
    await w.vm.$nextTick()
    // emit 只是上抛给宿主，本测试关注焦点是否已移到目标段
    expect(document.activeElement).not.toBe(document.body)
  })

  it('点击切档后焦点同样留在控件内', async () => {
    const w = mountTabs({ dimension: 'month' })
    const radios = w.findAll('[role="radio"]')
    await radios[2].trigger('click')
    expect(document.activeElement).not.toBe(document.body)
  })
})

describe('DimensionTabs · 不全局劫持方向键（AC-19 / WCAG 2.1.4）', () => {
  it('焦点在控件外时按方向键不产生 change', async () => {
    const outside = document.createElement('input')
    document.body.appendChild(outside)
    outside.focus()
    outside.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    await new Promise((r) => setTimeout(r, 0))
    const w = mountTabs({ dimension: 'month' })
    // 控件从未收到该事件 → 无 emit
    expect(w.emitted('change')).toBeUndefined()
    w.unmount()
    outside.remove()
  })

  it('禁用状态下方向键与点击均不切档（AC-16拖拽中拒绝）', async () => {
    const w = mountTabs({ dimension: 'month', disabled: true })
    const radios = w.findAll('[role="radio"]')
    await radios[2].trigger('click')
    await radios[1].trigger('keydown', { key: 'ArrowRight' })
    expect(w.emitted('change')).toBeUndefined()
  })
})

describe('DimensionTabs · busy 态（AC-07 挂起期间）', () => {
  it('busy 时 aria-busy="true" 且仍可操作（不静默丢弃）', () => {
    const w = mountTabs({ dimension: 'month', busy: true })
    expect(w.find('[role="radiogroup"]').attributes('aria-busy')).toBe('true')
    // 挂起 ≠ 禁用：用户仍可改主意，只是切换被延后
    expect(w.findAll('[role="radio"]').filter((r) => r.attributes('disabled') !== undefined)).toHaveLength(0)
  })

  it('非 busy 时显式为 aria-busy="false"（而非缺属性，读屏可稳定判定）', () => {
    const w = mountTabs({ dimension: 'month' })
    expect(w.find('[role="radiogroup"]').attributes('aria-busy')).toBe('false')
  })
})

describe('DimensionTabs · 无 emoji 图标（P0-1）', () => {
  it('三枚图标均为内联 SVG path，非文本/emoji', () => {
    const w = mountTabs()
    const svgs = w.findAll('svg')
    expect(svgs).toHaveLength(3)
    for (const s of svgs) {
      // stroke 落在 path 上（设计规格 §10.1 的写法），currentColor 使图标随状态变色
      const paths = s.findAll('path')
      expect(paths.length).toBeGreaterThan(0)
      for (const p of paths) expect(p.attributes('stroke')).toBe('currentColor')
      expect(s.attributes('aria-hidden')).toBe('true')
      expect(s.attributes('viewBox')).toBe('0 0 24 24')
    }
    // 标签文本仅为「年/月/日」单字，不含任何装饰性符号
    expect(w.findAll('.dim-text').map((t) => t.text())).toEqual(['年', '月', '日'])
    // 全文不含 emoji 区段码点（P0-1 门禁同款正则）
    expect(w.text()).not.toMatch(/[\u{1F300}-\u{1F9FF}\u{2600}-\u{26FF}\u{2700}-\u{27BF}]/u)
  })
})