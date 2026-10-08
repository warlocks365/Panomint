<template>
  <div
    ref="rootRef"
    class="dim-tabs"
    role="radiogroup"
    aria-label="分组粒度"
    :aria-busy="busy"
    :class="{ busy }"
    @keydown="onKeydown"
  >
    <button
      v-for="(opt, i) in OPTIONS"
      :key="opt.id"
      :ref="(el) => setSegRef(el, i)"
      type="button"
      role="radio"
      class="dim-seg"
      :class="{ active: opt.id === dimension }"
      :aria-checked="opt.id === dimension"
      :aria-label="opt.aria"
      :tabindex="opt.id === dimension ? 0 : -1"
      :disabled="disabled"
      @click="pick(opt.id)"
    >
      <svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
        <path :d="opt.icon" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <span class="dim-text">{{ opt.text }}</span>
    </button>
  </div>
</template>

<script setup>
// 时间轴三档分段控件（Job000145）。APG Radio Group 语义：
//   - role=radiogroup + 子项 role=radio + aria-checked
//   - roving tabindex：组内**恰好一个** tabindex="0"，其余 -1（Tab 只进出一次）
//   - 方向键/Home/End 切档并立即生效，首尾循环
//   - 切档后焦点**留在新选中项**，不丢回 <body>（AC-18）
//   - 只在自身容器上监听 keydown，**不做全局劫持**（WCAG 2.1.4 / AC-19）
//
// 图标为手写内联 SVG（viewBox 0 0 24 24，stroke=currentColor）：
// 本项目锁定「内联 SVG」单一约定（package.json 无图标库，守卫 O2钉死），
// currentColor 使图标随选中/悬停态自动变色，0 额外 HTTP。
import { ref, watch } from 'vue'
import { DIMENSIONS } from './timelineDimensions'

const props = defineProps({
  dimension: { type: String, required: true },
  // 分页在途且切换已挂起 → aria-busy（AC-07）
  busy: { type: Boolean, default: false },
  // 拖拽滑块期间拒绝切档（AC-16）
  disabled: { type: Boolean, default: false }
})
const emit = defineEmits(['change'])

// 三枚图标的 path 均为 24 网格描边路径：年=六格矩阵（粒度最粗）、月=单页日历、日=格内加日内刻度
const OPTIONS = [
  {
    id: 'year',
    text: '年',
    aria: '按年分组',
    icon: 'M4 7h4v4H4zM10 7h4v4h-4zM16 7h4v4h-4zM4 13h4v4H4zM10 13h4v4h-4zM16 13h4v4h-4z'
  },
  {
    id: 'month',
    text: '月',
    aria: '按月分组',
    icon: 'M4 6.5h16v13a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19.5zM4 10.5h16M8 4v4M16 4v4'
  },
  {
    id: 'day',
    text: '日',
    aria: '按日分组',
    icon: 'M4 6.5h16v13a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19.5zM4 10.5h16M8 4v4M16 4v4M8 14h2M14 14h2M8 17.5h2M14 17.5h2'
  }
]

const rootRef = ref(null)
const segEls = ref([])

function setSegRef(el, i) {
  if (el) segEls.value[i] = el
}

function focusIndex(i) {
  const el = segEls.value[i]
  if (el && typeof el.focus === 'function') el.focus()
}

/** 选中并把焦点移到该段——顺序不可颠倒：先改 tabindex 再 focus，否则焦点会掉到 body。 */
function pick(id) {
  if (props.disabled) return
  const i = DIMENSIONS.indexOf(id)
  if (i < 0 || id === props.dimension) return
  emit('change', id)
  focusIndex(i)
}

function onKeydown(e) {
  if (props.disabled) return
  const cur = DIMENSIONS.indexOf(props.dimension)
  let next = -1
  if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (cur + 1) % DIMENSIONS.length
  else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (cur - 1 + DIMENSIONS.length) % DIMENSIONS.length
  else if (e.key === 'Home') next = 0
  else if (e.key === 'End') next = DIMENSIONS.length - 1
  else return
  // 方向键/Home/End 在 radiogroup 内被消费：必须 preventDefault，
  // 否则 Space/方向键会连带滚动页面；也确保只作用于本组（监听器在本容器上）
  e.preventDefault()
  pick(DIMENSIONS[next])
}

// 档位被外部改（挂起切换落地 / 恢复默认）时同步 roving tabindex 的落点
watch(
  () => props.dimension,
  (d) => {
    const i = DIMENSIONS.indexOf(d)
    if (i >= 0 && rootRef.value?.contains(document.activeElement)) focusIndex(i)
  }
)
</script>

<style scoped>
.dim-tabs {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 3px;
  border-radius: 999px;
  background-color: var(--color-surface);
  box-shadow: var(--shadow-card);
}

.dim-tabs.busy {
  opacity: 0.7;
}

.dim-seg {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 26px;
  padding: 0 14px;
  border: 0;
  border-radius: 999px;
  background-color: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  font-family: inherit;
  cursor: pointer;
  transition: color 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.dim-seg:hover:not(:disabled) {
  color: var(--color-text-primary);
}

.dim-seg.active {
  background-color: var(--color-primary);
  color: var(--color-on-primary);
  font-weight: 500;
}

.dim-seg:disabled {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

.dim-seg:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-primary), 0 0 0 4px var(--color-surface);
}

/* 移动端（≤640px）：三等分 + 44px 触控目标（WCAG 2.5.5 / DESIGN.md §7） */
@media (max-width: 640px) {
  .dim-tabs {
    display: flex;
    width: 100%;
  }

  .dim-seg {
    flex: 1 1 0;
    justify-content: center;
    height: 44px;
    padding: 0 4px;
  }
}

@media (prefers-reduced-motion: reduce) {
  .dim-seg {
    transition: none;
  }
}
</style>