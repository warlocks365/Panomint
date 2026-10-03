<template>
  <div ref="rootEl" class="timeline-page">
    <header class="tl-toolbar">
      <select v-model="typeFilter" class="type-select" title="类型筛选">
        <option value="">全部类型</option>
        <option value="photo">照片</option>
        <option value="video">视频</option>
        <option value="360">360</option>
      </select>

      <button
        class="tool-btn"
        :class="{ active: favOnly }"
        title="只看收藏"
        @click="favOnly = !favOnly"
      >
        <svg viewBox="0 0 24 24" width="15" height="15" :fill="favOnly ? 'currentColor' : 'none'">
          <path
            d="M12 20.5s-7.5-4.6-9.3-9.2C1.4 7.9 3.6 4.5 7 4.5c2 0 3.6 1.1 5 2.9 1.4-1.8 3-2.9 5-2.9 3.4 0 5.6 3.4 4.3 6.8-1.8 4.6-9.3 9.2-9.3 9.2z"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linejoin="round"
          />
        </svg>
        收藏
      </button>

      <button
        v-if="placeFilter"
        class="tool-btn active"
        data-testid="place-filter-chip"
        :title="`仅显示地点：${placeFilter}`"
        @click="clearPlace"
      >
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
          <path d="M12 21s-6.5-5.4-6.5-10.5a6.5 6.5 0 0 1 13 0C18.5 15.6 12 21 12 21z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" />
          <circle cx="12" cy="10.2" r="2.3" stroke="currentColor" stroke-width="1.6" />
        </svg>
        {{ placeFilter }} ×
      </button>

      <div class="spacer"></div>

      <button class="tool-btn" title="回收站" @click="trashOpen = true">
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
          <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6.5 7l1 13h9l1-13" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        回收站
      </button>
    </header>

    <!-- 单一连续时间流（最新在前，内联月/日标题 + 右侧日期滑块）；Job000079 批量操作 -->
    <TimelineGrid
      ref="gridRef"
      :type="typeFilter"
      :favorites="favOnly"
      :place="placeFilter"
      selectable
      @open="openViewer"
      @changed="onBatchChanged"
      @thumb-action="onThumbAction"
    />

    <MediaViewer
      v-model="viewerOpen"
      v-model:index="viewerIndex"
      :items="gridItems"
      @deleted="onDeleted"
    />

    <TrashPanel v-model="trashOpen" @restored="onRestored" />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import gsap from 'gsap'
import TimelineGrid from '../components/timeline/TimelineGrid.vue'
import MediaViewer from '../components/viewer/MediaViewer.vue'
import TrashPanel from '../components/timeline/TrashPanel.vue'
import { deleteMedia, setFavorite } from '../api/media'

const rootEl = ref(null)
// DESIGN.md §8 page-enter（GSAP 官方 Vue 模式：gsap.context + 生命周期清理；
// 注意 @gsap/react 是 React 专用绑定，Vue 中引入会因缺少 React hooks 崩掉整个 chunk）
let pageCtx = null
onMounted(() => {
  const mm = gsap.matchMedia()
  mm.add('(prefers-reduced-motion: no-preference)', () => {
    pageCtx = gsap.context(() => {
      gsap.from('.tl-toolbar', { y: 24, opacity: 0, duration: 0.6, ease: 'power2.out' })
    }, rootEl.value)
  })
})
onBeforeUnmount(() => {
  if (pageCtx) pageCtx.revert()
})

const route = useRoute()
const router = useRouter()

const typeFilter = ref('')
const favOnly = ref(false)
// Job000062：地点页带 ?place= 进入时按地点过滤（'/media?place=' 后端已支持）
const placeFilter = ref(typeof route.query.place === 'string' ? route.query.place : '')
// Job000102 历史栈同步：同路由 query 变化（浏览器前进/后退、地点页改点另一地点）时同步过滤——
// 此前只在 setup 初始化一次，前进/后退到不同 place 的条目时过滤不刷新（SearchResultsView person watch 同模式）
watch(
  () => (typeof route.query.place === 'string' ? route.query.place : ''),
  (p) => {
    if (placeFilter.value !== p) placeFilter.value = p
  }
)
const gridRef = ref(null)
const viewerOpen = ref(false)
const viewerIndex = ref(0)
const trashOpen = ref(false)

function clearPlace() {
  placeFilter.value = ''
  if (route.query.place) router.replace({ query: { ...route.query, place: undefined } })
}

const gridItems = computed(() => gridRef.value?.items ?? [])

function is360(item) {
  return !!(item?.is_360 || item?.type === '360')
}

// Job000143：媒体卡右键/长按菜单动作。
//
// 「编辑拍摄信息」的实现是**复用既有打开路径**：先像点卡片一样打开查看器
// （信息面板在里面），用户随即可在面板里点「编辑拍摄信息」。
// 不另造一套「菜单直接进编辑态」的通路 —— 那会让编辑器有两个入口状态，
// 后续改动要同步两处，是典型的重复维护陷阱。
function onThumbAction(action, item) {
  if (action === 'open' || action === 'edit-metadata') {
    // 两者都打开查看器：信息面板里有「编辑拍摄信息」入口，不必从菜单直达编辑态
    openViewer(item)
    return
  }
  if (action === 'favorite') {
    toggleFavoriteFromMenu(item)
    return
  }
  if (action === 'delete') {
    deleteFromMenu(item)
  }
}

// 收藏：后端语义是「设为该状态」而非 toggle，故先算 next 再提交。
// gridItems 是 computed（只读），但它返回的是 gridRef 内部的数组引用，
// 就地改 item.favorite 会同步反映到列表与查看器 —— 不重拉列表（会丢滚动位置）。
async function toggleFavoriteFromMenu(item) {
  try {
    const next = !item.favorite
    await setFavorite(item.id, next)
    item.favorite = next
  } catch (e) {
    console.warn('收藏失败', e)
  }
}

// 删除：软删入回收站（可恢复），与 api.deleteMedia 语义一致。
// 删除后**不**自行 splice gridItems —— 它是 computed（只读），且真正的数据源
// 是 gridRef 内部的 items；这里只标一个本地态让卡片消失，权威列表由
// 查看器关闭时的 onDeleted / 下次分页请求负责刷新。
async function deleteFromMenu(item) {
  if (!window.confirm(`将「${item.filename || '该媒体'}」移入回收站？可随时恢复。`)) return
  try {
    await deleteMedia(item.id)
    item.__removed = true
  } catch (e) {
    console.warn('删除失败', e)
  }
}

function openViewer(item) {
  // 360 媒体：直接进入统一播放器的全景模式，无两步跳转
  if (is360(item)) {
    router.push({ name: 'player', params: { id: item.id } })
    return
  }
  const i = gridItems.value.findIndex((m) => m.id === item.id)
  if (i < 0) return
  viewerIndex.value = i
  viewerOpen.value = true
}

function onDeleted(id) {
  gridRef.value?.removeById(id)
  // 删除后索引指向下一张（数组已前移，同索引即为下一张）
  if (viewerIndex.value >= gridItems.value.length) {
    viewerIndex.value = Math.max(0, gridItems.value.length - 1)
  }
}

function onRestored() {
  gridRef.value?.reload()
}

function onBatchChanged() {
  // 批量操作（/media/batch）已清空选中集并生效：整流重载
  gridRef.value?.reload()
}

// 工具箱「最近删除」标签带 ?trash=1 进来，直接展开面板（否则用户还得在工具栏里再找一次按钮）
onMounted(() => {
  if (route.query.trash === '1') trashOpen.value = true
})
</script>

<style scoped>
.timeline-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.tl-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  flex-shrink: 0;
}

.spacer {
  flex: 1;
}

.type-select {
  height: 34px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 10px;
}

.tool-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.tool-btn:hover {
  background-color: var(--color-surface-hover);
}

.tool-btn.active {
  color: var(--color-danger);
  border-color: var(--color-danger);
  background-color: var(--color-surface);
}
</style>
