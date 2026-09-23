import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useViewerStore } from '../../stores/viewer'
import { useSearchStore } from '../../stores/search'

// PlayerView 拆解（Job000091）：播放集导航——
// 来源解析（?source=search 用搜索结果集，否则查看器 store 镜像；直达链接兜底 searchStore）/
// 当前索引定位/hasPrev·hasNext/go 切换（同步 viewerStore 索引再路由 push）。
// go 依赖 route.query 透传与 viewerStore 索引同步，持 router/route 属编排职责。
export function usePlaysetNav(mediaId) {
  const route = useRoute()
  const router = useRouter()
  const viewerStore = useViewerStore()
  const searchStore = useSearchStore()

  const source = computed(() => String(route.query.source || viewerStore.source || 'timeline'))
  const playset = computed(() => {
    if (source.value === 'search' && searchStore.results?.length) return searchStore.results
    if (viewerStore.items?.length) return viewerStore.items
    // 兜底：直接链接进入且搜索结果集恰好包含该媒体时，仍支持连续播放
    if (searchStore.results?.length) return searchStore.results
    return []
  })
  const idx = computed(() => playset.value.findIndex((m) => String(m.id) === mediaId.value))
  const hasPrev = computed(() => idx.value > 0)
  const hasNext = computed(() => idx.value >= 0 && idx.value < playset.value.length - 1)

  function go(delta) {
    const i = idx.value + delta
    if (i < 0 || i >= playset.value.length) return
    const next = playset.value[i]
    if (!next) return
    viewerStore.setIndex(i)
    router.push({ name: 'player', params: { id: next.id }, query: route.query })
  }

  /* 路由切换后同步播放集索引（供返回/切换时定位）；外部 watch(mediaId) 调用 */
  function syncIndex(id) {
    const i = playset.value.findIndex((m) => String(m.id) === id)
    if (i >= 0) viewerStore.setIndex(i)
  }

  return { source, playset, idx, hasPrev, hasNext, go, syncIndex }
}
