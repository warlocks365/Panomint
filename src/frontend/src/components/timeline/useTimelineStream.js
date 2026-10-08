import { computed, watch } from 'vue'
import { useTimelinePager } from './useTimelinePager'
import { useTimelineGrouping } from './useTimelineGrouping'
import { useTimelineSeek } from './useTimelineSeek'
import { headerHeightOf } from './timelineDimensions'

// 时间轴数据流装配（Job000145）——把「分页 → 分组 → 锚点」三层 composable 的接线
// 从 TimelineGrid.vue 抽出。抽它的理由不是行数好看，而是**接线本身是一条不变式**：
//
//   1. grouping 的档位与 seek 的档位必须是**同一个**响应式来源，否则两者会按
//      不同粒度算offsets 与 headerIndex，锚点静默错位（不报错、只是跳错位置）；
//   2. 档位变化只允许重取直方图，**不得触碰媒体取数**（AC-04）；
//   3. seek 的翻页等待必须用 pager 的 whenSettled，否则会abort 在途请求（AC-14）。
//
// 这三条一旦散在模板旁的 .vue 里，改接线时极易只改一处。故收敛到单一入口。
export function useTimelineStream(props, cols, rowHeight) {
  const pager = useTimelinePager(props)

  // 档位真源是 props（宿主 TimelineView 持有），派生 ref 不产生第二份状态。
  const dimension = computed(() => props.dimension)
  // 标题高度下发给 CSS（--tl-header-h），使 JS 与 CSS 不再各写一份px 魔数
  const headerHeight = computed(() => headerHeightOf(props.dimension))

  // AC-04：切档只改 pager 内部的取数来源（三档直方图已在挂载时取齐），
  // **不发任何网络请求**，也不触碰 items / nextCursor / 筛选参数。
  watch(dimension, (d) => pager.setDimension(d), { immediate: true })

  const grouping = useTimelineGrouping(pager.items, cols, rowHeight, dimension)

  const seek = useTimelineSeek({
    flat: grouping.flat,
    offsets: grouping.offsets,
    headerIndex: grouping.headerIndex,
    histogram: pager.histogram,
    dimension,
    items: pager.items,
    loading: pager.loading,
    loadMore: pager.loadMore,
    whenSettled: pager.whenSettled,
    finished: pager.finished
  })

  /** 切档锚点：抓当前视口顶部媒体 → 宿主换档→ 按新档位重定位（±2px，AC-05）。 */
  function captureAnchor(scrollEl) {
    return seek.captureAnchor(scrollEl)
  }

  function restoreAnchor(scrollEl, anchor) {
    seek.restoreAnchor(scrollEl, anchor)
  }

  return { pager, grouping, seek, dimension, headerHeight, captureAnchor, restoreAnchor }
}