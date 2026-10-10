// TimelineGrid TDZ（暂时性死区）回归防线
//
// 缺陷（2026-10-10 实机定位）：`ReferenceError: Cannot access '$' before initialization`
// 真实来源不是 page-agent、也不是循环依赖，而是 **const 声明前被同步读取**：
//   第 115 行  const allIds = () => pager.items.map(...)        // 箭头函数，延迟求值 → 不报错
//   第 140 行  watch(() => pager.items.length, ...)             // source 同步求值 → TDZ 报错
//   第 164 行  const { pager, ... } = useTimelineStream(...)     // pager 在此才声明
//
// Vue 的 watch(source, cb) 在建立 watcher 时会**立即同步执行一次 source getter**
// 以收集响应式依赖，故它在声明之前就被调用 → 落入 const 的 TDZ → ReferenceError。
// 压缩后 `pager` 被重命名为 `$`，当时一度误判为第三方库问题，实测堆栈
// `at k (TimelineView-DPch4CBJ.js:1:31824)` 才定位到本文件。
//
// 🔴 为什么必须源码级断言而不是行为测试：这个缺陷在挂载时抛错、被 Vue 的
// errorHandler 吞掉后页面仍能显示（只是少了 stagger 动画），行为断言很容易漏；
// 而「声明顺序」这件事只有读源码能验。

import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const SRC = readFileSync(fileURLToPath(new URL('../TimelineGrid.vue', import.meta.url)), 'utf8')

// 把字符索引换算成 1-based 行号（只用于错误提示，不参与断言）
const lineNoAt = (idx) => SRC.slice(0, idx).split('\n').length

describe('TimelineGrid · TDZ 回归防线', () => {
  it('pager 的 const 解构必须存在，且是 watch(pager.items) 的前置条件', () => {
    const decl = SRC.match(/const\s*\{\s*pager[^}]*\}\s*=\s*useTimelineStream\(/)
    expect(decl, '未找到 pager 的解构声明——本防线的前提是它存在').toBeTruthy()
  })

  it('watch(pager.items.length) 必须排在 pager 解构之后（否则 TDZ ReferenceError）', () => {
    const declIdx = SRC.search(/const\s*\{\s*pager[^}]*\}\s*=\s*useTimelineStream\(/)
    const watchIdx = SRC.search(/watch\(\s*\(\)\s*=>\s*pager\.items\.length/)
    expect(watchIdx, '未找到 watch(() => pager.items.length)').toBeGreaterThan(-1)
    expect(declIdx, '未找到 pager 解构').toBeGreaterThan(-1)
    // 这是本防线的核心断言：watch 必须在声明之后
    expect(
      watchIdx > declIdx,
      `TDZ 缺陷复发：watch(pager.items.length) 在第 ${lineNoAt(watchIdx)} 行，` +
      `早于 pager 的 const 声明（索引 ${watchIdx} < ${declIdx}）。` +
      'Vue watch 的 source 会同步求值，读取尚未初始化的 const 会抛 ' +
      "ReferenceError: Cannot access 'pager' before initialization。"
    ).toBe(true)
  })

  it('任何 watch 的 source 直接引用 pager，都必须在 pager 声明之后', () => {
    const declIdx = SRC.search(/const\s*\{\s*pager[^}]*\}\s*=\s*useTimelineStream\(/)
    // 抓所有形如 () => pager.xxx 的 source（同步求值的 watch/computed 都会TDZ）
    const srcRefs = [...SRC.matchAll(/watch\(\s*\(\)\s*=>\s*pager\./g)]
    expect(srcRefs.length, '未找到任何 watch(pager...) —— 若已重构请同步更新本防线').toBeGreaterThan(0)
    for (const m of srcRefs) {
      expect(
        m.index > declIdx,
        `TDZ：watch 的 source 在索引 ${m.index} 引用 pager，早于声明 ${declIdx}`
      ).toBe(true)
    }
  })

  it('位置约束注释必须在位（防止后人「整理代码」时把 watch 移回去）', () => {
    expect(SRC).toMatch(/暂时性死区|TDZ/)
    // 注释应紧邻 watch 块，说明它是有意为之的护栏而非偶然
    const watchIdx = SRC.search(/watch\(\s*\(\)\s*=>\s*pager\.items\.length/)
    const before = SRC.slice(Math.max(0, watchIdx - 1200), watchIdx)
    expect(before, '位置约束注释缺失或离 watch 太远').toMatch(/勿上移|TDZ|暂时性死区/)
  })
})