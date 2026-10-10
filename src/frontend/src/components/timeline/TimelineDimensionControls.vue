<template>
  <DimensionTabs
    :dimension="panel.dim.dimension.value"
    :busy="panel.dim.switching.value"
    :disabled="panel.dim.dragLocked.value"
    @change="panel.onDimensionChange"
  />

  <button
    ref="btnEl"
    class="tool-btn"
    title="跳转到指定日期"
    :aria-disabled="!hasAnyMedia"
    :aria-expanded="panel.calendarOpen.value"
    aria-haspopup="dialog"
    @click="panel.toggleCalendar"
  >
    <svg viewBox="0 0 24 24" width="15" height="15" fill="none" aria-hidden="true">
      <path d="M4 6.5h16v13a1.5 1.5 0 0 1-1.5 1.5h-13A1.5 1.5 0 0 1 4 19.5zM4 10.5h16M8 4v4M16 4v4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
    日期
  </button>

  <Teleport to="body">
    <div
      v-if="panel.calendarOpen.value"
      ref="popoverRef"
      class="dp-wrap"
      :style="{ top: pos.top + 'px', left: pos.left + 'px' }"
      :data-placement="pos.placement"
    >
      <DatePickerPopover
        :buckets="dayBuckets"
        :unknown-count="unknownCount"
        :anchor-key="panel.anchorDayKey.value"
        @close="onClose"
        @seek="panel.onDaySeek"
      />
    </div>
  </Teleport>
</template>

<script setup>
// 时间轴工具条内的「视图粒度 + 日期跳转」控件组（Job000145）。
//
// 位置：紧贴筛选器之后、.spacer 之前——从左到右「过滤 → 视图粒度」的阅读顺序；
// 视图粒度不与回收站等破坏性动作同列（后者可预期性会被削弱）。
//
// 纯展示 + 事件上抛：状态与时序全在 useTimelineDimensionPanel，此处不含任何业务判断。
// 拆出成组件而非留在 TimelineView.vue，是因为加上日历浮层后宿主会超 300 行组织红线。
//
// dayBuckets 直接来自 pager（挂载时一次取齐），故这里**不发请求**：
// 既满足 AC-04「切档零请求」，也让日历圆点与滑块刻度读同一份 count。
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import DimensionTabs from './DimensionTabs.vue'
import DatePickerPopover from './DatePickerPopover.vue'
import { computePopoverPosition } from './popoverPosition'

const props = defineProps({
  panel: { type: Object, required: true }, // useTimelineDimensionPanel 的返回值
  hasAnyMedia: { type: Boolean, default: false },
  dayBuckets: { type: Array, default: () => [] },
  unknownCount: { type: Number, default: 0 }
})

// —— 浮层定位 ——
// 原实现是 `position: absolute` 且**没有 top/left**，完全依赖静态位置；其包含块
// 一路向上全是 static，最终落到视口，再叠加祖先 `.content { overflow: auto }`
// 与工具条 `flex-wrap: wrap`，面板会被 TOP 栏压住或被裁掉一角。
// 现改为 Teleport 到 body + fixed，并按**触发按钮的实时视口坐标**定位，
// 空间不足时上下翻转、越界时贴边夹紧（算法见 popoverPosition.js，含单测）。
//
// ⚠️ 触发按钮必须用**本组件内的本地 ref**，不能写 ref="panel.calendarBtnRef"：
// Vue 3.5 的点号字符串 ref 只在 setupState 直接声明的变量上生效，而 panel 是 prop，
// 其内嵌 ref 不会被绑定 → 取到 undefined → 定位计算直接 return，
// 面板便停在初值 (0,0)，表现为「跑到页面左上角」。
const btnEl = ref(null)
const popoverRef = ref(null)
const pos = ref({ top: 0, left: 0, placement: 'bottom' })

function updatePos() {
  const btn = btnEl.value
  const el = popoverRef.value
  if (!btn || !el) return
  const r = btn.getBoundingClientRect()
  pos.value = computePopoverPosition({
    trigger: { top: r.top, left: r.left, right: r.right, bottom: r.bottom },
    // 面板尺寸取实测值：有无「本月无照片」提示会让高度差几十像素
    panel: { width: el.offsetWidth, height: el.offsetHeight },
    viewport: { width: window.innerWidth, height: window.innerHeight }
  })
}

/** 关闭：焦点回到触发按钮（WCAG 焦点管理）。焦点在此归还——按钮 ref 是本组件的。 */
function onClose() {
  props.panel.closeCalendar()
  nextTick(() => btnEl.value?.focus())
}

/** 面板外的任意点击都关闭（不点按钮本身，否则会开→关→开）。 */
function onDocPointerDown(e) {
  if (!props.panel.calendarOpen.value) return
  if (popoverRef.value?.contains(e.target) || btnEl.value?.contains(e.target)) return
  props.panel.closeCalendar()
}

// 打开后要等一帧才能量到面板自身高度；开着的这段时间里窗口尺寸/滚动变化都要重算
watch(
  () => props.panel.calendarOpen.value,
  async (open) => {
    if (open) {
      await nextTick()
      updatePos()
      window.addEventListener('resize', updatePos)
      window.addEventListener('scroll', updatePos, true)
      document.addEventListener('pointerdown', onDocPointerDown, true)
    } else {
      window.removeEventListener('resize', updatePos)
      window.removeEventListener('scroll', updatePos, true)
      document.removeEventListener('pointerdown', onDocPointerDown, true)
    }
  }
)

onBeforeUnmount(() => {
  window.removeEventListener('resize', updatePos)
  window.removeEventListener('scroll', updatePos, true)
  // 面板开着时卸载组件，watch 的 else 分支不会跑到，这里必须兜住，否则监听泄漏
  document.removeEventListener('pointerdown', onDocPointerDown, true)
})
</script>

<style scoped>
/* 无媒体时禁用日期跳转（AC-08）：保留可聚焦以便读屏播报，但不给点击反馈 */
.tool-btn[aria-disabled='true'] {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

/* 日历浮层：贴触发按钮定位（top/left 由 popoverPosition.js 按按钮实时坐标算），
   不用居中 modal——居中会遮挡整个时间轴，与「锚点跳转」的低承诺语义不符。

   position: fixed + Teleport 到 body 是修复的关键：原来的 absolute + 无 top/left
   依赖静态位置，其包含块一路向上全是 static，最终落到视口，既会被 TOP 栏压住，
   又会被祖先 .content{overflow:auto} 裁掉。挂到 body 后脱离一切祖先裁剪与层叠竞争。

   z-index 150：高于浮层/菜单类（TrashPanel 90、ThumbContextMenu 70、UserMenu 10），
   低于模态对话框（DialogHost 200）——浮层不应盖住模态。 */
.dp-wrap {
  position: fixed;
  z-index: 150;
}
</style>