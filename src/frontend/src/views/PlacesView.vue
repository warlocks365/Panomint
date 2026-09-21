<template>
  <div class="places-page">
    <header class="page-head">
      <h1 class="page-title">地点</h1>
      <p class="page-sub">按拍摄地点浏览（依赖 GPS 与逆地理编码）</p>
    </header>

    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="error" class="page-tip error" data-testid="places-error">
      {{ error }}
      <button class="retry-btn" @click="load">重试</button>
    </p>

    <div v-else-if="!places.length" class="empty" data-testid="places-empty">
      <p class="empty-title">暂无地点数据</p>
      <p class="empty-desc">媒体带 GPS 且服务端配置逆地理（AMAP_KEY）后，这里会自动出现地点卡片。</p>
    </div>

    <div v-else class="place-grid" data-testid="places-grid">
      <button
        v-for="p in places"
        :key="p.name"
        class="place-card"
        data-testid="place-card"
        @click="openPlace(p.name)"
      >
        <span class="pc-cover">
          <img
            v-if="p.cover_id"
            :src="thumbOf(p.cover_id)"
            :alt="p.name"
            loading="lazy"
            @error="onThumbError(p.cover_id)"
          />
          <span v-else class="pc-cover-fallback" v-html="icons.place"></span>
          <span class="pc-count">{{ p.count }} 项</span>
        </span>
        <span class="pc-name" :title="p.name">{{ p.name }}</span>
      </button>
    </div>
  </div>
</template>

<script setup>
// 地点页（Job000062）：按地名的全库聚合卡片；点击进入该地点的时间轴过滤视图。
// 封面加载失败静默隐藏（回退到地点图标），与缩略图模式回退同思路。
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import http from '../api/http'
import { navIcons } from '../layout/navIcons'
import { loadThumbUrl } from '../components/timeline/mediaLoader'

const router = useRouter()
const icons = navIcons

const places = ref([])
const loading = ref(true)
const error = ref('')
const brokenThumbs = new Set() // 加载失败的封面 id 集合（不重复请求）

function thumbOf(id) {
  return brokenThumbs.has(id) ? '' : loadThumbUrl({ id }, 'sm')
}

function onThumbError(id) {
  brokenThumbs.add(id)
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await http.get('/geo/places-overview')
    places.value = Array.isArray(data?.places) ? data.places : []
  } catch (e) {
    error.value = e.response?.data?.error?.message || '地点加载失败'
  } finally {
    loading.value = false
  }
}

function openPlace(name) {
  router.push({ path: '/timeline', query: { place: name } })
}

onMounted(load)
</script>

<style scoped>
.places-page {
  padding: 20px 24px;
}

.page-head {
  margin-bottom: 16px;
}

.page-title {
  margin: 0;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.page-sub {
  margin: 4px 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
}

.empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--color-text-disabled);
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.empty-desc {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.place-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 14px;
}

.place-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0 0 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  background-color: var(--color-surface);
  cursor: pointer;
  overflow: hidden;
  text-align: left;
}

.place-card:hover {
  background-color: var(--color-surface-hover);
}

.pc-cover {
  position: relative;
  display: block;
  aspect-ratio: 16 / 10;
  background-color: var(--color-surface-hover);
}

.pc-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.pc-cover-fallback {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--color-text-disabled);
}

.pc-cover-fallback svg,
.pc-cover-fallback :deep(svg) {
  width: 32px;
  height: 32px;
}

.pc-count {
  position: absolute;
  right: 8px;
  bottom: 8px;
  padding: 1px 8px;
  border-radius: 999px;
  background-color: rgba(0, 0, 0, 0.55);
  color: #fff;
  font-size: 11px;
}

.pc-name {
  padding: 0 12px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
