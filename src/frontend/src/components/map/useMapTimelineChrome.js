import { computed, ref, watch } from 'vue'
import { useResponsive } from '../../composables/useResponsive'

// MapTimeline 拆解（Job000089）：外观状态与缩放控制——
// 移动端折叠（手机默认折叠/横屏强制折叠/跨断点重置）+ 年/月/日粒度缩放。
// 与选择/拖拽逻辑（useMapTimelineSel）无关联，独立成件。
export function useMapTimelineChrome(props, emit) {
  // 手机默认折叠：展开态实测占 170~190px，812×375 横屏下会把地图压到 ~90px 高。
  // 桌面端恒为展开，观感与改动前一致。
  const { isMobile, isLandscape } = useResponsive()
  const collapsed = ref(isMobile.value)
  // 跨过 1024px 断点时重置为「移动端折叠 / 桌面端展开」
  watch(isMobile, (m) => {
    collapsed.value = m
  })
  // 手机上转成横屏 → 重新折叠（横屏只有 375px 高，必须优先保地图高度）
  watch(isLandscape, (l) => {
    if (isMobile.value && l) collapsed.value = true
  })
  const detailHidden = computed(() => isMobile.value && collapsed.value)

  const granularityLabel = computed(() => ({ year: '年', month: '月', day: '日' })[props.granularity] || '月')

  const GRANULARITY_ORDER = ['year', 'month', 'day']

  function zoomIn() {
    const i = GRANULARITY_ORDER.indexOf(props.granularity)
    if (i < GRANULARITY_ORDER.length - 1) emit('zoom', GRANULARITY_ORDER[i + 1])
  }
  function zoomOut() {
    const i = GRANULARITY_ORDER.indexOf(props.granularity)
    if (i > 0) emit('zoom', GRANULARITY_ORDER[i - 1])
  }

  return { isMobile, collapsed, detailHidden, granularityLabel, zoomIn, zoomOut }
}
