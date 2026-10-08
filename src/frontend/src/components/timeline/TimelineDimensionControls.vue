<template>
  <DimensionTabs
    :dimension="panel.dim.dimension.value"
    :busy="panel.dim.switching.value"
    :disabled="panel.dim.dragLocked.value"
    @change="panel.onDimensionChange"
  />

  <button
    ref="panel.calendarBtnRef"
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

  <div v-if="panel.calendarOpen.value" class="dp-wrap">
    <DatePickerPopover
      :buckets="dayBuckets"
      :unknown-count="unknownCount"
      :anchor-key="panel.anchorDayKey.value"
      @close="panel.closeCalendar"
      @seek="panel.onDaySeek"
    />
  </div>
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
import DimensionTabs from './DimensionTabs.vue'
import DatePickerPopover from './DatePickerPopover.vue'

defineProps({
  panel: { type: Object, required: true }, // useTimelineDimensionPanel 的返回值
  hasAnyMedia: { type: Boolean, default: false },
  dayBuckets: { type: Array, default: () => [] },
  unknownCount: { type: Number, default: 0 }
})
</script>

<style scoped>
/* 无媒体时禁用日期跳转（AC-08）：保留可聚焦以便读屏播报，但不给点击反馈 */
.tool-btn[aria-disabled='true'] {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

/* 日历浮层：贴工具条下沿左对齐，不用居中 modal——
   居中会遮挡整个时间轴，与「锚点跳转」的低承诺语义不符 */
.dp-wrap {
  position: absolute;
  z-index: 20;
  margin-top: 4px;
}
</style>