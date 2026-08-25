<template>
  <div class="timeline-page">
    <header class="tl-toolbar">
      <div class="view-switch">
        <button
          v-for="v in viewOptions"
          :key="v.key"
          class="view-btn"
          :class="{ active: view === v.key }"
          @click="switchView(v.key)"
        >
          {{ v.label }}
        </button>
      </div>

      <nav v-if="crumbs.length" class="crumbs">
        <template v-for="(c, i) in crumbs" :key="i">
          <button class="crumb" :class="{ tail: i === crumbs.length - 1 }" @click="goCrumb(c)">
            {{ c.label }}
          </button>
          <span v-if="i < crumbs.length - 1" class="crumb-sep">/</span>
        </template>
      </nav>

      <div class="spacer"></div>

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

      <button class="tool-btn" title="回收站" @click="trashOpen = true">
        <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
          <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6.5 7l1 13h9l1-13" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        回收站
      </button>
    </header>

    <section v-if="view === 'year' || view === 'month'" class="bucket-area">
      <p v-if="bucketsLoading" class="area-tip">加载中…</p>
      <p v-else-if="bucketsError" class="area-tip error">
        {{ bucketsError }}
        <button class="retry-btn" @click="loadBuckets">重试</button>
      </p>
      <p v-else-if="!buckets.length" class="area-tip">暂无媒体，导入照片与视频后将在此按{{ view === 'year' ? '年份' : '月份' }}归档</p>
      <div v-else class="bucket-grid">
        <button v-for="b in buckets" :key="b.key" class="bucket-card" @click="drill(b.key)">
          <span class="bucket-key">{{ fmtBucketKey(b.key) }}</span>
          <span class="bucket-count">{{ b.count }} 项</span>
        </button>
      </div>
    </section>

    <div v-if="view === 'day' && dayChips.length" class="day-strip">
      <button
        v-for="chip in dayChips"
        :key="chip.key"
        class="day-chip"
        :class="{ active: chip.active }"
        @click="pickDay(chip)"
      >
        {{ chip.label }}
        <span v-if="chip.count != null" class="chip-count">{{ chip.count }}</span>
      </button>
    </div>

    <TimelineGrid
      v-if="showGrid"
      ref="gridRef"
      :view="view"
      :date="date"
      :type="typeFilter"
      :favorites="favOnly"
      @open="openViewer"
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
import { computed, ref, watch } from 'vue'
import http from '../api/http'
import TimelineGrid from '../components/timeline/TimelineGrid.vue'
import MediaViewer from '../components/viewer/MediaViewer.vue'
import TrashPanel from '../components/timeline/TrashPanel.vue'

const viewOptions = [
  { key: 'year', label: '年' },
  { key: 'month', label: '月' },
  { key: 'day', label: '日' },
  { key: 'all', label: '全部' }
]

const view = ref('year')
const date = ref('')
const typeFilter = ref('')
const favOnly = ref(false)

const buckets = ref([])
const bucketsLoading = ref(false)
const bucketsError = ref('')

const gridRef = ref(null)
const viewerOpen = ref(false)
const viewerIndex = ref(0)
const trashOpen = ref(false)

const showGrid = computed(() => view.value === 'day' || view.value === 'all')

const gridItems = computed(() => gridRef.value?.items ?? [])

// 日视图顶部的「天」快捷筛选条：由 day buckets 生成
const dayChips = computed(() => {
  if (view.value !== 'day' || !buckets.value.length) return []
  const chips = [{ key: '', label: '全部', count: null, active: !isDayDate.value }]
  for (const b of buckets.value) {
    chips.push({
      key: b.key,
      label: fmtDay(b.key),
      count: b.count,
      active: date.value === b.key
    })
  }
  return chips
})

const isDayDate = computed(() => /^\d{4}-\d{2}-\d{2}$/.test(date.value))

const crumbs = computed(() => {
  const out = []
  if (view.value === 'month' && date.value) {
    out.push({ label: '全部年份', view: 'year', date: '' })
    out.push({ label: fmtYear(date.value), view: 'month', date: date.value })
  } else if (view.value === 'day' && date.value) {
    const y = date.value.slice(0, 4)
    const m = date.value.slice(0, 7)
    out.push({ label: '全部年份', view: 'year', date: '' })
    out.push({ label: fmtYear(y), view: 'month', date: y })
    if (isDayDate.value) {
      out.push({ label: fmtMonth(m), view: 'day', date: m })
      out.push({ label: fmtDay(date.value), view: 'day', date: date.value })
    } else {
      out.push({ label: fmtMonth(m), view: 'day', date: m })
    }
  }
  return out
})

watch([view, date, typeFilter, favOnly], () => {
  if (view.value !== 'all') loadBuckets()
})

function switchView(v) {
  if (view.value === v) return
  view.value = v
  date.value = ''
}

function goCrumb(c) {
  view.value = c.view
  date.value = c.date
}

function drill(key) {
  if (view.value === 'year') {
    view.value = 'month'
    date.value = key // YYYY
  } else if (view.value === 'month') {
    view.value = 'day'
    date.value = key // YYYY-MM
  }
}

function pickDay(chip) {
  if (!chip.key) {
    // 回到上一级范围：月内全部 / 全部日期
    date.value = date.value.length >= 7 ? date.value.slice(0, 7) : ''
  } else {
    date.value = chip.key
  }
}

async function loadBuckets() {
  bucketsLoading.value = true
  bucketsError.value = ''
  try {
    const params = { view: view.value, limit: 1 }
    if (date.value) params.date = date.value
    if (typeFilter.value) params.type = typeFilter.value
    if (favOnly.value) params.favorites = 'true'
    const { data } = await http.get('/media', { params })
    buckets.value = Array.isArray(data.buckets) ? data.buckets : []
  } catch (e) {
    buckets.value = []
    bucketsError.value = e.response ? `加载失败：HTTP ${e.response.status}` : '加载失败：网络不可达'
  } finally {
    bucketsLoading.value = false
  }
}

function openViewer(item) {
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
  loadBuckets()
}

function fmtBucketKey(key) {
  if (view.value === 'year') return fmtYear(key)
  if (view.value === 'month') return fmtMonth(key)
  return key
}

function fmtYear(key) {
  return `${key.slice(0, 4)} 年`
}

function fmtMonth(key) {
  const [y, m] = key.split('-')
  return `${y} 年 ${Number(m)} 月`
}

function fmtDay(key) {
  const parts = key.split('-')
  return `${Number(parts[1])} 月 ${Number(parts[2])} 日`
}

loadBuckets()
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

.view-switch {
  display: inline-flex;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  overflow: hidden;
  background-color: var(--color-surface);
}

.view-btn {
  border: none;
  background: transparent;
  padding: 7px 18px;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.view-btn + .view-btn {
  border-left: 1px solid var(--color-border);
}

.view-btn:hover {
  background-color: var(--color-surface-hover);
}

.view-btn.active {
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-weight: 600;
}

.crumbs {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--font-size-sm);
}

.crumb {
  border: none;
  background: transparent;
  padding: 4px 6px;
  border-radius: var(--radius-sm);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}

.crumb:hover {
  background-color: var(--color-primary-active-bg);
}

.crumb.tail {
  color: var(--color-text-primary);
  font-weight: 600;
}

.crumb-sep {
  color: var(--color-text-disabled);
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

.bucket-area {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.area-tip {
  color: var(--color-text-secondary);
  font-size: var(--font-size-md);
  padding: 40px 0;
  text-align: center;
}

.area-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 4px 12px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.bucket-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 12px;
}

.bucket-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
  padding: 18px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  background-color: var(--color-surface);
  box-shadow: var(--shadow-card);
  text-align: left;
}

.bucket-card:hover {
  background-color: var(--color-surface-hover);
  border-color: var(--color-primary);
}

.bucket-key {
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.bucket-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.day-strip {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  flex-shrink: 0;
}

.day-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: 16px;
  padding: 5px 14px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.day-chip:hover {
  background-color: var(--color-surface-hover);
}

.day-chip.active {
  background-color: var(--color-primary-active-bg);
  border-color: var(--color-primary);
  color: var(--color-primary);
  font-weight: 600;
}

.chip-count {
  font-size: 11px;
  color: var(--color-text-disabled);
}

.day-chip.active .chip-count {
  color: var(--color-primary);
}
</style>
