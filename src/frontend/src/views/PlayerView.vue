<template>
  <div class="player-view">
    <!-- 舞台区：按类型渲染核心（照片 / 普通视频 / 360 全景直接球面渲染） -->
    <div class="stage-area">
      <div v-if="media.loading.value" class="state">加载中…</div>
      <div v-else-if="media.error.value" class="state error">{{ media.error.value }}</div>

      <!-- 照片：直接显示原图 -->
      <div v-else-if="media.mode.value === 'photo'" class="photo-wrap">
        <img v-if="media.photoUrl.value" :src="media.photoUrl.value" :alt="media.detail.value?.filename || '照片'" class="photo">
      </div>

      <!-- 360：照片 → 球面渲染（TextureLoader）；视频已转码 → 全景播放 -->
      <Player360
        v-else-if="media.mode.value === 'pano' && media.panoReady.value"
        :key="media.panoSrc.value"
        :media-id="mediaId"
        :mode="media.panoKind.value"
        :src="media.panoSrc.value"
        :title="media.detail.value?.filename || ''"
        class="pano"
      />

      <!-- 360 视频：未转码 → 提示并发起转码（照片无需转码） -->
      <TranscodePrompt
        v-else-if="media.mode.value === 'pano' && media.panoKind.value === 'video'"
        :transcode="media.transcode.value"
        @start="media.startTranscode"
      />

      <!-- 普通视频 -->
      <div v-else class="video-wrap">
        <video ref="plainVideoRef" controls playsinline class="plain-video"></video>
      </div>

      <!-- 退出：移动端返回按钮（左上） + 右上 ×（全端） -->
      <button v-if="!isDesktop" class="exit-btn back-btn" title="返回" @click="exit">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
          <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </button>
      <button class="exit-btn close-btn" title="退出查看器（Esc）" @click="exit">
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none">
          <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
      </button>
      <button v-if="!isDesktop" class="exit-btn info-btn" title="媒体信息" @click="drawerOpen = true">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
          <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
          <path d="M12 11v5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
          <circle cx="12" cy="7.8" r="1.1" fill="currentColor" />
        </svg>
      </button>

      <!-- 播放集导航：上一张 / 下一张（← / →），计数器 -->
      <PlaysetNav
        :playset="nav.playset.value"
        :idx="nav.idx.value"
        :has-prev="nav.hasPrev.value"
        :has-next="nav.hasNext.value"
        @go="nav.go"
      />
    </div>

    <!-- 桌面端：右侧信息窗格平铺 -->
    <MediaInfoPanel
      v-if="isDesktop"
      :media-id="mediaId"
      :detail="media.detail.value"
      :loading="media.loading.value"
      :show-close="false"
      @deleted="exit"
    />

    <!-- 移动端：信息抽屉 -->
    <div v-if="!isDesktop && drawerOpen" class="drawer-mask" @click.self="drawerOpen = false">
      <div class="drawer">
        <MediaInfoPanel
          :media-id="mediaId"
          :detail="media.detail.value"
          :loading="media.loading.value"
          @close="drawerOpen = false"
          @deleted="exit"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
// Job000091 拆解（媒体加载状态机 + 播放集导航双 composable，转码提示/播放集导航拆展示件）：
// - usePlayerMedia：load 四态分支/blob 池代次守卫/转码发起轮询（plainVideoRef 宿主传入）
// - usePlaysetNav：来源解析/索引定位/go 切换（router+store 编排）
// - TranscodePrompt：转码三态提示（start 上抛宿主编排）；PlaysetNav：导航箭头+计数器
// 宿主留：舞台五态分发/退出钮组/键盘/Esc 退出/抽屉开合与页面布局样式。
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { useRoute } from 'vue-router'
import { useResponsive } from '../composables/useResponsive'
import { useBackNavigation } from '../composables/useBackNavigation'
import Player360 from '../components/player/360Player.vue'
import MediaInfoPanel from '../components/player/MediaInfoPanel.vue'
import PlaysetNav from '../components/player/PlaysetNav.vue'
import TranscodePrompt from '../components/player/TranscodePrompt.vue'
import { usePlayerMedia } from './player/usePlayerMedia'
import { usePlaysetNav } from './player/usePlaysetNav'

const route = useRoute()
const { isDesktop } = useResponsive()
const { goBack } = useBackNavigation()
const mediaId = computed(() => String(route.params.id || ''))

const plainVideoRef = ref(null) // 普通视频元素宿主自持（DOM 归属宿主）
const media = usePlayerMedia(mediaId, plainVideoRef)
const nav = usePlaysetNav(mediaId)
const drawerOpen = ref(false)

/* 退出（Job000102 抽 useBackNavigation，判定口径不变）：返回来源页（优先路由历史，直达链接则回时间轴） */
function exit() {
  goBack({ name: 'timeline' })
}

function onKeydown(e) {
  const tag = e.target?.tagName
  if (e.key === 'Escape') {
    // 输入框/文本域内的 Esc 留给控件自身（如关闭标签补全）
    if (tag === 'INPUT' || tag === 'TEXTAREA') return
    exit()
    return
  }
  // 视频原生控件聚焦时不劫持方向键（避免妨碍进度调整）
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'VIDEO' || tag === 'SELECT') return
  if (e.key === 'ArrowLeft') {
    e.preventDefault()
    nav.go(-1)
  } else if (e.key === 'ArrowRight') {
    e.preventDefault()
    nav.go(1)
  }
}

watch(mediaId, (id) => {
  if (!id) return
  media.load()
  // 保持播放集索引与当前媒体一致（供返回/切换时定位）
  nav.syncIndex(id)
}, { immediate: true })
onMounted(() => window.addEventListener('keydown', onKeydown))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  media.cleanup()
})
</script>

<style scoped>
.player-view {
  position: relative;
  /* 抵消 AppShell .content 的 24px 内边距，播放器满幅展示 */
  margin: -24px;
  width: calc(100% + 48px);
  height: calc(100vh - var(--topbar-height));
  background: var(--player-bg);
  overflow: hidden;
  display: flex;
}

.stage-area {
  position: relative;
  flex: 1;
  min-width: 0;
  overflow: hidden;
}

.pano { position: absolute; inset: 0; }

.state {
  height: 100%;
  display: flex; flex-direction: column;
  align-items: center; justify-content: center; gap: 12px;
  color: var(--player-text-dim); font-size: var(--font-size-md);
}
.state.error { color: var(--color-danger); }
.photo-wrap {
  height: 100%; display: flex; align-items: center; justify-content: center;
}
.photo { max-width: 100%; max-height: 100%; object-fit: contain; }
.video-wrap {
  height: 100%; display: flex; align-items: center; justify-content: center;
}
.plain-video { max-width: 100%; max-height: 100%; }

/* 退出 / 信息按钮：悬浮于舞台之上，高于 360 播放器的控制栏（z-index 10） */
.exit-btn {
  position: absolute;
  z-index: 30;
  width: 36px;
  height: 36px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 50%;
  background: rgba(20, 24, 29, 0.6);
  color: var(--color-text-on-dark);
  backdrop-filter: blur(6px);
}

.exit-btn:hover {
  background: rgba(20, 24, 29, 0.85);
  color: #fff;
}

.close-btn {
  top: 10px;
  right: 10px;
}

.back-btn {
  top: 10px;
  left: 10px;
}

.info-btn {
  top: 10px;
  right: 56px;
}

/* 移动端信息抽屉：底部上滑面板 */
.drawer-mask {
  position: absolute;
  inset: 0;
  z-index: 40;
  background: rgba(0, 0, 0, 0.5);
  display: flex;
  align-items: flex-end;
}

.drawer {
  width: 100%;
  max-height: 72vh;
  background: var(--color-surface);
  border-radius: var(--radius-lg) var(--radius-lg) 0 0;
  overflow: hidden;
  display: flex;
}

.drawer :deep(.info-panel) {
  width: 100%;
  border-left: none;
}
</style>
