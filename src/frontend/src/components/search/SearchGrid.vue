<template>
  <div ref="wrapRef" class="grid-wrap">
    <div v-if="store.error" class="grid-error">
      <p>{{ store.error }}</p>
      <button class="retry-btn" @click="store.run()">重试</button>
    </div>

    <template v-else>
      <RecycleScroller
        v-if="rows.length"
        class="scroller"
        :items="rows"
        :item-size="rowHeight"
        key-field="__key"
      >
        <template #default="{ item: row }">
          <div class="row" :style="{ gridTemplateColumns: `repeat(${cols}, 1fr)` }">
            <ThumbItem v-for="m in row.cells" :key="m.id" :item="m" @open="$emit('open', $event)" />
          </div>
        </template>
        <template #after>
          <div ref="sentinelRef" class="sentinel">
            <span v-if="store.loading">加载中…</span>
            <span v-else-if="!store.hasMore" class="muted">已加载全部 {{ store.results.length }} 条</span>
          </div>
        </template>
      </RecycleScroller>

      <div v-if="!store.loading && !store.results.length" class="grid-empty">
        <svg viewBox="0 0 24 24" width="40" height="40" fill="none">
          <circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="1.5" />
          <path d="M16 16l5 5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
        <p>没有找到匹配的媒体，换个关键词或调整筛选条件试试</p>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RecycleScroller } from 'vue-virtual-scroller'
import 'vue-virtual-scroller/dist/vue-virtual-scroller.css'
import ThumbItem from '../timeline/ThumbItem.vue'
import { useSearchStore } from '../../stores/search'

defineEmits(['open'])

const store = useSearchStore()

const GAP = 8
const MIN_CELL = 140

const wrapRef = ref(null)
const sentinelRef = ref(null)
const wrapWidth = ref(0)

const cols = computed(() => {
  if (!wrapWidth.value) return 5
  return Math.max(2, Math.floor((wrapWidth.value + GAP) / (MIN_CELL + GAP)))
})

const cellSize = computed(() => {
  if (!wrapWidth.value) return MIN_CELL
  return Math.floor((wrapWidth.value - GAP * (cols.value - 1)) / cols.value)
})

const rowHeight = computed(() => cellSize.value + GAP)

const rows = computed(() => {
  const out = []
  const list = store.results
  for (let i = 0; i < list.length; i += cols.value) {
    const cells = list.slice(i, i + cols.value)
    out.push({ __key: `${cells[0].id}:${cols.value}:${i}`, cells })
  }
  return out
})

let resizeObserver = null
let intersectionObserver = null

onMounted(() => {
  if (wrapRef.value) {
    wrapWidth.value = wrapRef.value.clientWidth
    resizeObserver = new ResizeObserver((entries) => {
      wrapWidth.value = entries[0].contentRect.width
    })
    resizeObserver.observe(wrapRef.value)
  }
  intersectionObserver = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting) store.loadMore()
    },
    { root: wrapRef.value, rootMargin: '240px' }
  )
  if (sentinelRef.value) intersectionObserver.observe(sentinelRef.value)
})

// sentinel 随首屏结果渲染后才出现，需在其挂载后补挂观察
watch(sentinelRef, (el, prev) => {
  if (prev) intersectionObserver?.unobserve(prev)
  if (el) intersectionObserver?.observe(el)
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
})
</script>

<style scoped>
.grid-wrap {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  position: relative;
  display: flex;
  flex-direction: column;
}

.scroller {
  flex: 1;
}

.row {
  display: grid;
  gap: 8px;
  padding-bottom: 8px;
}

.sentinel {
  padding: 16px 0 4px;
  text-align: center;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.muted {
  color: var(--color-text-disabled);
}

.grid-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--color-text-disabled);
  font-size: var(--font-size-md);
  text-align: center;
}

.grid-error {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 12px;
  color: var(--color-danger);
  font-size: var(--font-size-md);
}

.retry-btn {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 6px 16px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.retry-btn:hover {
  background-color: var(--color-surface-hover);
}
</style>
