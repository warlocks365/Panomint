import { nextTick, ref, watch } from 'vue'
import { useTimelineDimension } from './useTimelineDimension'

// 时间轴「三档维度 + 日期锚点跳转」的宿主装配（Job000145）。
//
// 抽出成 composable 的理由与 TimelineGrid 里 useTimelineStream 同源：
// 这段装配里有三条**互相咬合**的时序不变式，散在 .vue 里改接线时极易只改一处，
// 而这类错误全部表现为「偶尔跳错位置 / 偶尔焦点丢失」——不报错、不可见、极难复现。
//
//   ① AC-04/05：切档零网络请求 + 以视口顶部媒体为锚点重定位（±2px）。
//   ② AC-07：分页在途时把切档挂起而非丢弃；loading 落 false 后立刻执行。
//   ③ AC-12/15：日历跳转只定位不筛选；拖拽期间点日历先终止拖拽，只执行最后一次意图。
//
// 日历的 day 粒度桶由pager 在挂载时一次取齐（见 useTimelinePager.loadHistograms），
// 本模块**不再单独请求** —— 既满足 AC-04，也保证日历圆点与滑块刻度同源。
export function useTimelineDimensionPanel({ gridRef, hasAnyMedia }) {
  /**
   * 读grid 暴露的 loading。
   *
   * 不能写 `gridRef.value.loading.value`：defineExpose 的返回值会被 proxyRefs 包一层，
   * **ref 会被自动解包**，故 `gridRef.value.loading` 已经是 boolean，再取 .value 得 undefined，
   * 于是 isLoading 恒 false —— AC-07 的「分页在途时挂起切档」会静默失效
   * （不报错、构建过、单测若用 ref 桩也测不出来）。这个坑是单测跑出来的。
   */
  const isPaging = () => !!gridRef.value?.loading

  let pendingAnchor = null
  const dim = useTimelineDimension()

  dim.bind({
    before: () => {
      pendingAnchor = gridRef.value?.captureAnchor?.() || null
    },
    after: () => {
      if (pendingAnchor) gridRef.value?.restoreAnchor?.(pendingAnchor)
      pendingAnchor = null
    },
    isLoading: isPaging
  })

  // AC-07：加载完成后执行挂起的切换（控件全程 aria-busy，不静默丢弃）
  watch(isPaging, (v) => {
    if (!v) dim.flushPending()
  })

  const anchorDayKey = ref('')
  const calendarOpen = ref(false)
  const calendarBtnRef = ref(null)

  function onDimensionChange(next) {
    dim.requestDimension(next)
  }

  // AC-15：拖拽中点日历 → 立即终止拖拽与惯性，只执行最后一次跳转意图
  function toggleCalendar() {
    if (!hasAnyMedia.value) return
    dim.setDragLocked(false)
    calendarOpen.value = !calendarOpen.value
  }

  /** Esc / 关闭按钮：焦点须回到触发按钮（WCAG 焦点管理）。 */
  function closeCalendar() {
    calendarOpen.value = false
    nextTick(() => calendarBtnRef.value?.focus())
  }

  // AC-12：跳转只定位不筛选——/media 查询参数与切换前逐字段相同
  function onDaySeek(payload) {
    closeCalendar()
    anchorDayKey.value = payload.key
    gridRef.value?.seekTo?.(payload)
  }

  return {
    dim,
    anchorDayKey,
    calendarOpen,
    calendarBtnRef,
    onDimensionChange,
    toggleCalendar,
    closeCalendar,
    onDaySeek
  }
}