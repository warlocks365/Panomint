<template>
  <div class="dp" role="dialog" aria-modal="false" aria-label="跳转到指定日期" @keydown.esc.stop="onEsc">
    <div class="dp-head">
      <span class="dp-title">跳转到日期</span>
      <button type="button" class="dp-close" title="关闭" @click="emit('close')">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
          <path d="M6.5 6.5l11 11M17.5 6.5l-11 11" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <CalendarNavBar ref="navRef" :m="m" />

    <CalendarGrid :matrix="m" :loading="loading" :anchor-key="anchorKey" />

    <p v-if="!loading && !m.hasAny.value" class="dp-note">
      <svg viewBox="0 0 24 24" width="24" height="24" fill="none" aria-hidden="true">
        <path d="M12 21s-6.5-5.4-6.5-10.5a6.5 6.5 0 0 1 13 0C18.5 15.6 12 21 12 21z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
        <circle cx="12" cy="10.4" r="2.6" fill="currentColor" />
      </svg>
      这个月没有照片
    </p>
    <p v-else-if="unknownCount > 0" class="dp-hint">有 {{ unknownCount }} 项媒体没有拍摄日期，无法在此定位</p>

    <div class="dp-actions">
      <button type="button" class="dp-act" :disabled="!m.pickedKey.value" @click="go">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
          <path d="M12 3.5v11M8 11l4 4 4-4M5 19.5h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        查看这一天
      </button>
      <button type="button" class="dp-act" @click="goToday">
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" aria-hidden="true">
          <path d="M12 21s-7-5.6-7-10.6A7 7 0 0 1 19 10.4C19 15.4 12 21 12 21z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
          <circle cx="12" cy="10.4" r="2.6" stroke="currentColor" stroke-width="1.6" />
        </svg>
        回到今天
      </button>
    </div>
  </div>
</template>

<script setup>
// 时间轴日期选择弹层（Job000145）。**自建面板，不用原生 input[type=date]**——
// 原生控件的日期格是 UA shadow DOM，无样式钩子，既加不了「当天有照片」圆点，
// 也无法表达「仅定位不筛选」的跳转语义（Spec §2.1）。
//
// 面板只做壳（头部翻月 / 状态说明 / 动作行），日期网格在 CalendarGrid.vue，
// 月矩阵状态在 useCalendarMatrix.js —— 三者任一都撑不过 300 行组织红线。
//
// unknown 桶（taken_at IS NULL）不进 counts，故不渲染任何标记、也不可跳转（AC-11）。
import { ref, toRef, watch } from 'vue'
import CalendarGrid from './CalendarGrid.vue'
import CalendarNavBar from './CalendarNavBar.vue'
import { useCalendarMatrix } from './useCalendarMatrix'

const props = defineProps({
  // GET /media/date-histogram?granularity=day → [{ bucket:'YYYY-MM-DD', count }]
  buckets: { type: Array, default: () => [] },
  // taken_at IS NULL 的媒体数（unknown 桶）
  unknownCount: { type: Number, default: 0 },
  loading: { type: Boolean, default: false },
  // 当前锚点日（YYYY-MM-DD），用于高亮「当前定位」
  anchorKey: { type: String, default: '' }
})
const emit = defineEmits(['close', 'seek'])

const m = useCalendarMatrix(toRef(props, 'buckets'))
const navRef = ref(null)

/**
 * Esc 两级：先收起年/月选择面板，再关掉整个浮层。
 * 否则用户在看年份列表时按Esc 会直接连浮层一起关掉——
 * 相当于跳过了「我只是想取消选择年份」这一步。
 */
function onEsc() {
  if (navRef.value?.collapse?.()) return
  emit('close')
}

/** 跳转意图只发 { dimension, key } 对象载荷；**不改媒体查询参数**（AC-12）。 */
function go() {
  if (!m.pickedKey.value) return
  emit('seek', { dimension: 'day', key: m.pickedKey.value })
}

function goToday() {
  if (!m.jumpToday()) return
  go()
}

// 换了月份数据（重新取数完成）时清空待确认的选择，避免用旧选择跳到新月
watch(
  () => props.buckets,
  () => m.clearPicked()
)
</script>

<style scoped>
.dp {
  width: 308px;
  padding: 12px;
  border-radius: var(--radius-lg);
  background-color: var(--color-surface);
  box-shadow: var(--shadow-lift);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
}

.dp-head {
  display: flex;
  align-items: center;
  gap: 4px;
  height: 40px;
}

.dp-title {
  flex: 1;
  text-align: center;
  font-size: var(--font-size-md);
}

.dp-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: 0;
  border-radius: var(--radius-sm);
  background-color: transparent;
  color: var(--color-text-secondary);
  cursor: pointer;
  transition: background-color 150ms cubic-bezier(0.32, 0.72, 0, 1);
}

.dp-close {
  width: 28px;
  height: 28px;
}

.dp-close:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.dp-close:focus-visible,
.dp-act:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-primary);
}

.dp-note,
.dp-hint {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px 0 0;
  min-height: 36px;
  font-size: var(--font-size-xs);
  color: var(--color-text-secondary);
}

.dp-hint {
  color: var(--color-text-disabled);
}

.dp-actions {
  display: flex;
  gap: 8px;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid var(--color-border);
}

.dp-act {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  flex: 1;
  height: 34px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.dp-act:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.dp-act:disabled {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

@media (max-width: 640px) {
  .dp {
    width: 100%;
  }
}

</style>