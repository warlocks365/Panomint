<template>
  <div class="spaces-view">
    <h1 class="page-title">空间</h1>

    <div v-if="page.loadError.value" class="error-banner">{{ page.loadError.value }}</div>

    <SpaceCards
      :personal="page.personal.value"
      :shared="page.shared.value"
      :active-space="page.activeSpace.value"
      :format-bytes="page.formatBytes"
      @switch="page.switchSpace"
    />

    <template v-if="!(page.activeSpace.value === 'shared' && page.shared.value.length === 0)">
      <div class="grid-header">
        <h2 class="section-title">
          {{ page.activeSpace.value === 'personal' ? '个人空间媒体' : page.sharedSpaceName.value }}
        </h2>
        <span v-if="!page.mediaLoading.value" class="muted">共 {{ page.mediaTotal.value }} 项</span>
      </div>

      <div v-if="page.mediaLoading.value" class="muted grid-tip">加载中…</div>
      <div v-else-if="page.mediaItems.value.length === 0" class="muted grid-tip">该空间暂无媒体</div>

      <MediaTileGrid :items="page.mediaItems.value" selectable @open="openItem" @changed="page.loadMedia" />

      <div v-if="page.nextCursor.value" class="load-more">
        <button class="btn" data-testid="load-more" :disabled="page.loadingMore.value" @click="page.loadMore">
          {{ page.loadingMore.value ? '加载中…' : '加载更多' }}
        </button>
      </div>
    </template>
  </div>
</template>

<script setup>
// 空间页面宿主（Job000093：数据编排走 useSpacesPage，空间卡片+空态拆 SpaceCards）。
// 宿主职责：页级布局、媒体网格容器装配、openItem 跳转 player（router 属视图职责）。
import { useRouter } from 'vue-router'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'
import SpaceCards from './spaces/SpaceCards.vue'
import { useSpacesPage } from './spaces/useSpacesPage'

const router = useRouter()
const page = useSpacesPage()

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id } })
}
</script>

<style scoped>
.spaces-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 1080px;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.error-banner {
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
}

.grid-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.section-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
}

.muted {
  color: var(--color-text-secondary);
}

.grid-tip {
  padding: 32px 0;
  text-align: center;
}

.load-more {
  display: flex;
  justify-content: center;
}

.btn {
  padding: 8px 20px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  cursor: pointer;
  font-family: var(--font-family);
}

.btn:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.btn:disabled {
  color: var(--color-text-disabled);
  cursor: default;
}
</style>
