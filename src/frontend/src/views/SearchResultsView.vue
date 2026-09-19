<template>
  <div class="search-page">
    <header class="search-head">
      <h1 class="search-title">
        <template v-if="store.query">“{{ store.query }}”</template>
        <template v-else>搜索</template>
        <span v-if="store.searched && !store.loading" class="search-total">共 {{ store.total }} 项</span>
      </h1>

      <div class="head-side">
        <ActiveFilterChips :chips="store.chips" @remove="store.removeChip" />
        <button v-if="store.query || store.activeFilterCount" class="clear-all" @click="onClearAll">
          清除
        </button>
        <button v-if="!isDesktop" class="filter-toggle" @click="drawerOpen = true">
          <svg viewBox="0 0 24 24" width="14" height="14" fill="none">
            <path d="M4 6h16M7 12h10M10 18h4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
          </svg>
          筛选
          <span v-if="store.activeFilterCount" class="filter-badge">{{ store.activeFilterCount }}</span>
        </button>
      </div>
    </header>

    <FilterBar
      v-if="isDesktop"
      :model-value="store.filters"
      @update:model-value="onFiltersUpdate"
      @apply="store.run()"
    />

    <div v-if="store.placeFallback" class="fallback-banner">
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" class="fallback-icon">
        <path d="M12 21s-7-5.5-7-11a7 7 0 0 1 14 0c0 5.5-7 11-7 11z" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        <circle cx="12" cy="10" r="2.5" stroke="currentColor" stroke-width="1.5" />
      </svg>
      <span>
        未找到地名“{{ store.filters.place || store.query }}”，已按地图位置周边
        {{ fallbackKm }} 公里检索
      </span>
      <button class="fallback-close" title="关闭提示" @click="store.placeFallback = null">
        <svg viewBox="0 0 12 12" width="10" height="10" fill="none">
          <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>
    </div>

    <p v-if="!store.searched" class="idle-tip">输入关键词或设置筛选条件开始搜索</p>

    <SearchGrid v-else @open="openPlayer" />

    <FilterDrawer
      v-if="!isDesktop"
      v-model="drawerOpen"
      :filters="store.filters"
      @apply="onDrawerApply"
    />
  </div>
</template>

<script setup>
import { computed, ref, onMounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSearchStore } from '../stores/search'
import { useResponsive } from '../composables/useResponsive'
import FilterBar from '../components/search/FilterBar.vue'
import FilterDrawer from '../components/search/FilterDrawer.vue'
import ActiveFilterChips from '../components/search/ActiveFilterChips.vue'
import SearchGrid from '../components/search/SearchGrid.vue'

const router = useRouter()
const route = useRoute()
const store = useSearchStore()
const { isDesktop } = useResponsive()

const drawerOpen = ref(false)

// 从非搜索页跳入（顶栏搜索框回车 / PeopleView 的 ?person=）时首跳即执行搜索
onMounted(() => {
  const person = String(route.query.person || '')
  if (person) store.filters.person = person
  if (store.query || person || store.activeFilterCount) store.run()
})

// 已在搜索页时路由筛选参数变化（如再次从人物页跳入）同步进 store 并重跑
watch(
  () => route.query.person,
  (p) => {
    const person = String(p || '')
    if (store.filters.person === person) return
    store.filters.person = person
    if (person) store.run()
  }
)

const fallbackKm = computed(() => {
  const m = store.placeFallback?.radius_m
  if (!m) return ''
  return m % 1000 === 0 ? m / 1000 : (m / 1000).toFixed(1)
})

function onFiltersUpdate(filters) {
  store.filters = filters
}

function onDrawerApply(filters) {
  store.filters = filters
  store.run()
}

function onClearAll() {
  store.clearAll()
}

function openPlayer(item) {
  router.push({ name: 'player', params: { id: item.id } })
}
</script>

<style scoped>
.search-page {
  height: 100%;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.search-head {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  flex-shrink: 0;
}

.search-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.search-total {
  margin-left: 8px;
  font-size: var(--font-size-sm);
  font-weight: 400;
  color: var(--color-text-secondary);
}

.head-side {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-left: auto;
}

.clear-all {
  border: none;
  background: transparent;
  color: var(--color-primary);
  font-size: var(--font-size-sm);
  padding: 4px 8px;
  border-radius: var(--radius-sm);
}

.clear-all:hover {
  background-color: var(--color-primary-active-bg);
}

.filter-toggle {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.filter-toggle:hover {
  background-color: var(--color-surface-hover);
}

.filter-badge {
  min-width: 16px;
  height: 16px;
  border-radius: 8px;
  background-color: var(--color-primary);
  color: #fff;
  font-size: 11px;
  line-height: 16px;
  text-align: center;
  padding: 0 4px;
}

.fallback-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
  flex-shrink: 0;
}

.fallback-icon {
  flex-shrink: 0;
}

.fallback-close {
  margin-left: auto;
  border: none;
  background: transparent;
  color: var(--color-warning-text);
  display: inline-flex;
  padding: 4px;
  border-radius: var(--radius-sm);
}

.idle-tip {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  margin: 0;
  color: var(--color-text-disabled);
  font-size: var(--font-size-md);
}
</style>
