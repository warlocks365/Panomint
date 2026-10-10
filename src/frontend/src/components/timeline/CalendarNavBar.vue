<template>
  <div class="nav">
    <!-- 日视图：上一月 / 年月下拉 / 下一月 -->
    <div class="nav-row">
      <button
        type="button"
        class="nav-step"
        :disabled="!m.canPrev.value"
        title="上一月"
        aria-label="上一月"
        @click="m.shiftMonth(-1)"
      >
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
          <path d="M14.5 5.5 8 12l6.5 6.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>

      <div class="nav-picks">
        <button
          type="button"
          class="nav-pick"
          :class="{ on: level === 'year' }"
          :aria-expanded="level === 'year'"
          title="选择年份"
          @click="toggle('year')"
        >
          {{ m.cursorYear.value }} 年
        </button>
        <button
          type="button"
          class="nav-pick"
          :class="{ on: level === 'month' }"
          :aria-expanded="level === 'month'"
          title="选择月份"
          @click="toggle('month')"
        >
          {{ m.cursorMonth.value }} 月
        </button>
      </div>
      <button
        type="button"
        class="nav-step"
        :disabled="!m.canNext.value"
        title="下一月"
        aria-label="下一月"
        @click="m.shiftMonth(1)"
      >
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" aria-hidden="true">
          <path d="M9.5 5.5 16 12l-6.5 6.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
    </div>

    <!-- 年选择：点「年」出现，点「月」或选中即回到日视图 -->
    <div v-if="level === 'year'" class="nav-grid" role="listbox" aria-label="选择年份">
      <button
        v-for="y in m.yearChoices.value"
        :key="y"
        type="button"
        class="nav-cell"
        :class="{ cur: y === m.cursorYear.value, empty: !m.yearHasPhoto(y) }"
        role="option"
        :aria-selected="y === m.cursorYear.value"
        :disabled="y < m.minYear.value || y > m.maxYear.value"
        @click="pickYear(y)"
      >{{ y }} 年</button>
    </div>

    <!-- 月选择：2 月标注 28/29 天（闰年差异可视化） -->
    <div v-else-if="level === 'month'" class="nav-grid" role="listbox" aria-label="选择月份">
      <button
        v-for="mm in 12"
        :key="mm"
        type="button"
        class="nav-cell"
        :class="{ cur: mm === m.cursorMonth.value, empty: !monthInRange(mm) }"
        role="option"
        :aria-selected="mm === m.cursorMonth.value"
        :disabled="!monthInRange(mm)"
        :title="monthTitle(mm)"
        @click="pickMonth(mm)"
      >{{ mm }} 月<span class="nav-cell-sub">共 {{ m.monthLength(m.cursorYear.value, mm) }} 天</span></button>
    </div>
  </div>
</template>

<script setup>
// 日历的年 / 月导航条（Job000145）。**与日期网格分离**：日期格是「按天选」，
// 年月切换是「换到哪一段时间看」，两者混在一个面板里会让 308px 宽度装不下。
//
// 三级切换：年下拉 → 月下拉 → 日网格。年月只换游标（看哪个月），
// 真正的「定位到某一天」仍由日网格点选完成——保持「仅定位不筛选」的语义。
import { ref, watch } from 'vue'
import { monthIndexOf } from './calendarNav'

const props = defineProps({
  m: { type: Object, required: true } // useCalendarMatrix 返回值
})

// null=日视图 / 'year'=年选择 / 'month'=月选择
const level = ref(null)

function toggle(l) {
  level.value = level.value === l ? null : l
}

/** 该年在当前可导航范围内吗（年选择器的禁用判据）。 */
function yearInRange(y) {
  return y >= props.m.minYear.value && y <= props.m.maxYear.value
}

function pickYear(y) {
  if (!yearInRange(y)) return
  props.m.setYear(y)
  level.value = null
}

/** 该月在「当前年」下是否可导航：不能晚于当前月，也不能早于最早有照片的月。 */
function monthInRange(mm) {
  const idx = monthIndexOf(props.m.cursorYear.value, mm)
  if (idx > props.m.maxIndex.value) return false
  if (props.m.minIndex.value !== null && idx < props.m.minIndex.value) return false
  return true
}

/** 月份标题带天数，让 2 月的 28/29 天差异直接可见（闰年边界不靠猜）。 */
function monthTitle(mm) {
  return `${props.m.cursorYear.value} 年 ${mm} 月，共 ${props.m.monthLength(props.m.cursorYear.value, mm)} 天`
}

/** 切换到某个月时若被夹动了年（越过最早/最新边界），把年下拉状态一并收敛。 */
function pickMonth(mm) {
  props.m.setMonth(mm)
  level.value = null
}

// 游标被外部改动（如「回到今天」）时收起选择面板，避免残留悬空浮层
watch(
  () => [props.m.cursorYear.value, props.m.cursorMonth.value],
  () => {
    level.value = null
  }
)

/**
 * 供父组件在 Esc 时优先收起年月面板（而不是直接关掉整个浮层）。
 * @returns {boolean} 确实收起了一层 → true；本来就没展开 → false
 */
function collapse() {
  if (level.value === null) return false
  level.value = null
  return true
}
defineExpose({ collapse })
</script>

<style scoped>
.nav-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  height: 28px;
}

.nav-picks {
  display: flex;
  gap: 4px;
  flex: 1;
  justify-content: center;
}

.nav-pick {
  border: 1px solid transparent;
  background: none;
  border-radius: var(--radius-sm);
  padding: 2px 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  cursor: pointer;
}

.nav-pick:hover {
  background-color: var(--color-surface-hover);
}

.nav-pick.on {
  border-color: var(--color-border);
  background-color: var(--color-surface-hover);
}

.nav-step {
  border: none;
  background: none;
  padding: 2px;
  color: var(--color-text-primary);
  cursor: pointer;
  border-radius: var(--radius-sm);
}

.nav-step:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.nav-step:disabled {
  color: var(--color-text-disabled);
  cursor: not-allowed;
}

.nav-grid {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 4px;
  margin-top: 8px;
}

.nav-cell {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 6px 2px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  cursor: pointer;
  line-height: 1.3;
}

.nav-cell:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.nav-cell.cur {
  background-color: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-surface);
}

/* 无照片的年份/月份：可看不可定位，用次要色而非禁用灰（保持可读） */
.nav-cell.empty:not(.cur) {
  color: var(--color-text-secondary);
}

.nav-cell:disabled {
  color: var(--color-text-disabled);
  cursor: not-allowed;
  border-color: transparent;
  background: none;
}

.nav-cell-sub {
  display: block;
  font-size: var(--font-size-xs);
  opacity: 0.75;
}
</style>