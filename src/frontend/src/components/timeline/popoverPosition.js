/**
 * 浮层定位算法（Job000145 缺陷修复）。
 *
 * 缺陷原委：`.dp-wrap` 用了 `position: absolute` 却**没有 top/left**，
 * 完全依赖「静态位置」。它的包含块一路向上到 `.timeline-page` / `.content`
 * 都没有 `position`，故包含块最终是视口；再叠加祖先 `.content { overflow: auto }`
 * 与工具条 `flex-wrap: wrap`，面板既可能被 TOP 栏压住、也可能被裁掉一角，
 * 且在任何窗口尺寸下都不会自适应。
 *
 * 修法（与本文件配套）：浮层用 `Teleport` 挂到 `body` + `position: fixed`，
 * 脱离一切祖先裁剪与层叠竞争，再由本函数按**触发按钮的视口坐标**算出落点。
 *
 * 本文件是纯函数：不碰 DOM、不读 window，故可用 vitest 直接覆盖边界
 * （下方 __tests__/popoverPosition.spec.js）。这一点很重要——定位算错时
 * 浏览器截图未必能稳定复现，但纯函数可以。
 */

/**
 * @param {object} trigger 触发按钮的视口坐标（getBoundingClientRect 的四项）
 * @param {{width:number,height:number}} panel 浮层自身尺寸
 * @param {{width:number,height:number}} viewport 视口尺寸
 * @param {number} gap 按钮与浮层的间距
 * @param {number} margin 浮层与视口边缘的最小留白
 * @returns {{top:number,left:number,placement:'bottom'|'top'}}
 */
export function computePopoverPosition({ trigger, panel, viewport, gap = 8, margin = 8 }) {
  const spaceBelow = viewport.height - trigger.bottom
  const spaceAbove = trigger.top

  //下方放不下、且上方比下方宽裕 → 翻到按钮上方
  const fitsBelow = panel.height + gap <= spaceBelow
  const flip = !fitsBelow && spaceAbove > spaceBelow

  let top = flip ? trigger.top - panel.height - gap : trigger.bottom + gap
  let left = trigger.left

  // 水平：默认与按钮左缘对齐；越出视口就夹紧（贴边而不是被切掉）
  const maxLeft = viewport.width - panel.width - margin
  left = clamp(left, margin, Math.max(margin, maxLeft))

  // 垂直：翻转后仍越界（如面板比视口还高）就夹紧，保证顶部始终可见
  const maxTop = viewport.height - panel.height - margin
  top = clamp(top, margin, Math.max(margin, maxTop))

  return { top: Math.round(top), left: Math.round(left), placement: flip ? 'top' : 'bottom' }
}

function clamp(v, lo, hi) {
  return Math.min(Math.max(v, lo), hi)
}