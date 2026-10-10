<template>
  <div class="cg-week" aria-hidden="true">
    <span v-for="w in matrix.WEEK" :key="w">{{ w }}</span>
  </div>

  <!-- Loading：骨架色块 + 底部 2px 进度条。禁圆形 spinner（DESIGN.md §9-7） -->
  <div v-if="loading" class="cg-skeleton" aria-hidden="true">
    <i class="cg-bar"></i>
  </div>

  <div v-else class="cg-grid" role="grid" :aria-label="`${matrix.cursorYear.value} 年 ${matrix.cursorMonth.value} 月`">
    <button
      v-for="c in matrix.cells.value"
      :key="c.key || `x${c.ts}`"
      type="button"
      role="gridcell"
      class="cg-cell"
      :class="cellClass(c)"
      :data-has="c.hasPhoto ? 'photo' : 'none'"
      :data-anchor="c.key === anchorKey && !!c.key ? 'true' : 'false'"
      :aria-disabled="!c.selectable"
      :aria-label="cellLabel(c)"
      :aria-selected="c.key === anchorKey && !!c.key"
      :disabled="!c.selectable"
      @click="matrix.select(c.key)"
    >
      {{ c.day }} 日
    </button>
  </div>
</template>

<script setup>
// 日历日期网格（Job000145）。从 DatePickerPopover 拆出——四态格子样式 + 模板
// 会把宿主推过 300 行组织红线（frontend_org_guard O1）。
//
// 四态日期格：可选（有照片，实底数字 + 下方 4px 主色实心圆点）/ 无照片灰显 /
// 未来日期（失效态）/ 跨月补位格；另叠加今天（底色）与当前锚点（唯一实底态）。
// 圆点用 CSS ::after 画而非 SVG：4px 实心圆用 CSS 更省，且颜色随格内文字色跟随。
const props = defineProps({
  matrix: { type: Object, required: true },
  loading: { type: Boolean, default: false },
  anchorKey: { type: String, default: '' }
})

function cellClass(c) {
  return {
    'cg-out': c.out,
    'cg-has': c.selectable,
    'cg-empty': c.inMonth && !c.future && !c.hasPhoto,
    'cg-future': c.inMonth && c.future,
    'cg-today': c.today,
    'cg-anchor': !!c.key && c.key === props.anchorKey
  }
}

/** 不依赖颜色单独传达信息（WCAG 1.4.1）：每格 aria-label 写明状态与数量。 */
function cellLabel(c) {
  if (c.out) return `${c.day} 日（不在本月）`
  const base = `${props.matrix.cursorMonth.value} 月 ${c.day} 日`
  if (c.future) return `${base}，未来日期`
  if (c.selectable) {
    const n = props.matrix.counts.value.get(c.key) || 0
    return `${base}，有 ${n} 项照片${c.key === props.anchorKey ? '，当前定位' : ''}`
  }
  return `${base}，无照片`
}
</script>

<style scoped>
.cg-week,
.cg-grid {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 4px;
}

.cg-week {
  height: 28px;
  align-items: center;
  font-size: var(--font-size-xs);
  color: var(--color-text-disabled);
  text-align: center;
}

.cg-cell {
  position: relative;
  height: 36px;
  border: 0;
  border-radius: var(--radius-sm);
  background-color: transparent;
  color: var(--color-text-secondary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  cursor: pointer;
  transition: background-color 150ms cubic-bezier(0.32, 0.72, 0, 1);
}

.cg-cell:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.cg-cell:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-primary);
}

/* 有照片：实底数字（9.22:1）+ 下方 4px 主色实心圆点 */
.cg-has {
  color: var(--color-text-primary);
}

.cg-cell[data-has='photo']::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: 5px;
  transform: translateX(-50%);
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--color-primary);
}

.cg-cell[data-anchor='true']::after {
  background: var(--color-on-primary);
}

/* 今天用底色而非仅靠数字色区分，与「无照片的过去日」拉开一个通道 */
.cg-today {
  background-color: var(--color-primary-active-bg);
}

/* 当前锚点：全日历唯一实底态 */
.cg-anchor,
.cg-anchor:hover:not(:disabled) {
  background-color: var(--color-primary);
  color: var(--color-on-primary);
  font-weight: 500;
}

/* 未来日期：--color-text-disabled（WCAG 1.4.3 失效组件例外豁免，见设计文档 §6） */
.cg-future,
.cg-out {
  color: var(--color-text-disabled);
  cursor: default;
}

.cg-cell:disabled {
  cursor: default;
}

/* Loading 骨架：42 格色块用 repeating 渐变画出，避免 42 个 DOM 节点 */
.cg-skeleton {
  position: relative;
  height: 232px;
  background-image: repeating-linear-gradient(
    to bottom,
    var(--color-surface-hover) 0 36px,
    transparent 36px 40px
  );
  background-size: calc((100% - 24px) / 7) 100%;
}

.cg-bar {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 0;
  height: 2px;
  background-color: var(--color-primary);
}

/* 移动端：格放大到 44px 触控目标（7×44+6×4+24×2 = 356px ≤ 374px 可用宽） */
@media (max-width: 640px) {
  .cg-cell {
    height: 44px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .cg-cell {
    transition: none;
  }
}
</style>