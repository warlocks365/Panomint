// TimelineGrid · vue-virtual-scroller 已弃用 prop 回归防线
//
// 缺陷（2026-10-10 实机发现）：控制台长期输出唯一一条警告
//   [vue-virtual-scroller] `sizeDependencies` is deprecated and will be removed
//   in the next major release. Dynamic item sizing uses ResizeObserver in modern browsers.
//
// 上游实现（node_modules/vue-virtual-scroller/dist/dynamicScrollerMeasurement-*.js）：
//   const Q = "[vue-virtual-scroller] `sizeDependencies` is deprecated ..."
//   let U = !1
//   function R(l) { l == null || U || (U = !0, console.warn(Q)) }
// 即该 prop 在 3.x 已**完全无功能**，唯一作用是打印一次警告；动态尺寸一律由
// 库内 ResizeObserver 测量。所以正确处置是删 prop，而不是压制 console。
//
// 🔴 为什么必须源码级断言：
//   1) 警告本身被上游用「单次打印」标志节流，行为测试跑第二遍就看不到；
//   2) 断言库版本号无法证明「本项目没传这个 prop」；
//   3) 反向验证方式：把 prop 加回去 → 本文件立刻变红。

import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const SRC = readFileSync(fileURLToPath(new URL('../TimelineGrid.vue', import.meta.url)), 'utf8')

// 剔除 HTML 注释与 JS 行注释后再检测：防回退说明写在注释里，不该被判为「仍在传 prop」
const CODE = SRC.replace(/<!--[\s\S]*?-->/g, '').replace(/^\s*\/\/.*$/gm, '')

describe('TimelineGrid · 已弃用虚拟滚动 prop 防线', () => {
  it('不得再向 DynamicScrollerItem 传 size-dependencies（3.x 已弃用且无功能）', () => {
    expect(
      /:size-dependencies\b/.test(CODE),
      '检测到 size-dependencies：该 prop 在 vue-virtual-scroller 3.x 只会打印弃用警告，动态尺寸已由 ResizeObserver 负责'
    ).toBe(false)
    expect(/\bsizeDependencies\b/.test(CODE), '检测到驼峰写法 sizeDependencies').toBe(false)
  })

  it('不得使用 watch-data（仅 legacy 无 ResizeObserver 回退路径需要，现代浏览器不适用）', () => {
    expect(/watch-data\b|\bwatchData\b/.test(CODE), '检测到 watch-data：仅 legacy 回退路径需要').toBe(false)
  })

  it('min-item-size 必须保留（虚拟滚动高度预估的唯一依据，删掉会导致滚动跳动）', () => {
    expect(/:min-item-size\s*=\s*"\d+"/.test(CODE), 'DynamicScroller 缺少 min-item-size').toBe(true)
  })

  it('禁加回注释必须留在位，说明为何不再传该 prop', () => {
    expect(/勿加回\s*:size-dependencies/.test(SRC), '缺少防止回退的注释说明').toBe(true)
  })

  it('DynamicScrollerItem 仍保留 item/active/index 三个必要绑定', () => {
    const m = CODE.match(/<DynamicScrollerItem\b[^>]*>/)
    expect(m, '未找到 DynamicScrollerItem').toBeTruthy()
    expect(m[0]).toMatch(/:item=/)
    expect(m[0]).toMatch(/:active=/)
    expect(m[0]).toMatch(/:index=/)
  })
})
