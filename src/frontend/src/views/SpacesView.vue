<template>
  <div class="spaces-view">
    <div class="ph-heading">
      <span class="ph-eyebrow">06</span>
      <h1 class="page-title">空间</h1>
    </div>

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
        <div class="header-tools">
          <!-- Job000101：个人空间专属视图切换（共享空间无相册分组语义） -->
          <div v-if="page.activeSpace.value === 'personal'" class="view-toggle" role="tablist" aria-label="视图模式">
            <button
              type="button"
              class="toggle-btn"
              :class="{ 'toggle-btn--active': !page.groupByAlbum.value }"
              data-testid="view-mode-timeline"
              role="tab"
              :aria-selected="!page.groupByAlbum.value"
              @click="page.setGroupByAlbum(false)"
            >时间轴</button>
            <button
              type="button"
              class="toggle-btn"
              :class="{ 'toggle-btn--active': page.groupByAlbum.value }"
              data-testid="view-mode-albums"
              role="tab"
              :aria-selected="page.groupByAlbum.value"
              @click="page.setGroupByAlbum(true)"
            >按相册</button>
          </div>
          <span v-if="!page.groupByAlbum.value && !page.mediaLoading.value" class="muted">共 {{ page.mediaTotal.value }} 项</span>
        </div>
      </div>

      <!-- Job000101：按相册分组视图 -->
      <template v-if="page.activeSpace.value === 'personal' && page.groupByAlbum.value">
        <div v-if="page.groupsLoading.value" class="muted grid-tip" data-testid="groups-loading">加载中…</div>
        <AlbumGroupsPanel
          v-else
          :groups="page.groups.value"
          :ungrouped="page.ungrouped.value"
          @open="openItem"
          @view-album="goAlbum"
        />
      </template>

      <!-- 时间轴网格视图（原语义） -->
      <template v-else>
        <div v-if="page.mediaLoading.value" class="muted grid-tip">加载中…</div>
        <div v-else-if="page.mediaItems.value.length === 0" class="muted grid-tip">该空间暂无媒体</div>

        <MediaTileGrid :items="page.mediaItems.value" selectable @open="openItem" @changed="page.loadMedia" />

        <div v-if="page.nextCursor.value" class="load-more">
          <button class="btn" data-testid="load-more" :disabled="page.loadingMore.value" @click="page.loadMore">
            {{ page.loadingMore.value ? '加载中…' : '加载更多' }}
          </button>
        </div>
      </template>
    </template>
  </div>
</template>

<script setup>
// 空间页面宿主（Job000093：数据编排走 useSpacesPage，空间卡片+空态拆 SpaceCards；
// Job000101：增个人空间「按相册分组」视图切换与分组面板装配）。
// 宿主职责：页级布局、媒体网格/分组面板容器装配、openItem 跳转 player（router 属视图职责）。
import { nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import gsap from 'gsap'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'
import SpaceCards from './spaces/SpaceCards.vue'
import AlbumGroupsPanel from './spaces/AlbumGroupsPanel.vue'
import { useSpacesPage } from './spaces/useSpacesPage'

const router = useRouter()
const page = useSpacesPage()

// DESIGN.md §8 cards-in：空间卡首屏一次性瀑布（personal 数据就绪后触发；clearProps 还原）
let cardsDone = false
watch(
  () => page.personal.value,
  async (p) => {
    if (p && !cardsDone) {
      cardsDone = true
      await nextTick()
      requestAnimationFrame(() => {
        const mm = gsap.matchMedia()
        mm.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.from('.space-cards .space-card', {
            y: 24,
            opacity: 0,
            duration: 0.55,
            ease: 'power2.out',
            stagger: 0.08,
            clearProps: 'transform,opacity'
          })
        })
      })
    }
  }
)

// 视图偏好防抖窗口内离开页面 → 补写一次（不 await，与地图侧同范式）
onBeforeUnmount(() => page.flushOnUnmount())

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id } })
}

function goAlbum(albumId) {
  router.push({ name: 'album-detail', params: { id: albumId } })
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
  font-size: var(--font-size-lg);
  font-weight: 500;
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}

/* 页头规范（DESIGN.md §5） */
.ph-heading {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.ph-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--color-text-disabled);
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
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.section-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 500;
  letter-spacing: 0.04em;
}

.header-tools {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* Job000101 分段切换钮：时间轴 | 按相册 —— 胶囊形态（DESIGN.md 深化稿 VIEW.06） */
.view-toggle {
  display: inline-flex;
  background-color: var(--color-surface);
  border-radius: 999px;
  padding: 3px;
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.06);
}

.toggle-btn {
  padding: 6px 16px;
  border: none;
  border-radius: 999px;
  background-color: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  font-family: var(--font-family);
  transition: background-color 0.5s cubic-bezier(0.32, 0.72, 0, 1), color 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.toggle-btn + .toggle-btn {
  border-left: none;
}

.toggle-btn--active {
  background-color: var(--color-primary);
  color: #eef1f4;
  font-weight: 500;
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
