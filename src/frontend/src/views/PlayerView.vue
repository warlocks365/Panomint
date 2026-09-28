<template>
  <div class="player-view">
    <!-- 舞台区：按类型渲染核心（照片 / 普通视频 / 360 全景直接球面渲染） -->
    <div ref="stageRef" class="stage-area">
      <!-- Job000135：全屏/退出全屏（播放窗口内右上角浮动） -->
      <!-- Job000136 实时码流显示开关 -->
      <button
        class="fs-btn br-btn"
        data-testid="bitrate-toggle"
        :class="{ on: showBitrate }"
        :title="showBitrate ? '隐藏码流信息' : '显示码流信息'"
        @click="toggleBitrate"
      >
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none">
          <path d="M4 18V10M10 18V6M16 18v-8M21 18H3" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
      </button>
      <button
        class="fs-btn"
        data-testid="stage-fullscreen"
        :title="isFs ? '退出全屏' : '全屏'"
        @click="toggleFullscreen"
      >
        <svg v-if="!isFs" viewBox="0 0 24 24" width="18" height="18" fill="none">
          <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
        <svg v-else viewBox="0 0 24 24" width="18" height="18" fill="none">
          <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" />
        </svg>
      </button>
      <!-- Job000136：实时码流信息卡（开关控制） -->
      <div v-if="showBitrate" class="bitrate-card" data-testid="bitrate-card">
        <div class="br-row br-live">
          <span class="br-label">实时码率</span>
          <span class="br-value">{{ liveBitrate || '—' }}</span>
        </div>
        <div class="br-row">
          <span class="br-label">视频码率</span>
          <span class="br-value">{{ avgBitrate || '—' }}</span>
        </div>
        <div class="br-row">
          <span class="br-label">缓冲余量</span>
          <span class="br-value">{{ bufferSec != null ? bufferSec.toFixed(1) + ' s' : '—' }}</span>
        </div>
        <div class="br-row br-dim">
          <span class="br-label">规格</span>
          <span class="br-value">{{ specLine }}</span>
        </div>
      </div>

      <div v-if="media.loading.value" class="state">加载中…</div>
      <!-- Job000131 P1：HEVC 编码且浏览器不支持 → 错误态旁提供「兼容播放」（实时转码兜底，显式触发） -->
      <div v-else-if="media.error.value && !liveMode" class="state error">
        {{ media.error.value }}
        <button
          v-if="media.liveEligible.value"
          class="btn live-entry"
          data-testid="live-play-entry"
          @click="liveMode = true"
        >兼容播放（720p 实时转码）</button>
      </div>
      <LivePlayer v-else-if="liveMode" :media-id="mediaId" class="live-wrap" />

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

      <!-- 360 视频：原始回退 blob 拉取中（Job000126，防 TranscodePrompt 闪现） -->
      <div v-else-if="media.mode.value === 'pano' && media.panoKind.value === 'video' && media.panoFallbackLoading.value" class="state">
        加载中…
      </div>

      <!-- 360 视频：原始回退也不可用 → 提示并发起转码（照片无需转码）；关闭自动转码时如实提示（Job000120） -->
      <TranscodePrompt
        v-else-if="media.mode.value === 'pano' && media.panoKind.value === 'video'"
        :transcode="media.transcode.value"
        :disabled="media.transcodeDisabled.value"
        @start="media.startTranscode"
      />

      <!-- Job000126：360 原始回退提示条（叠加在播放器上，贴底不遮控制）——
           转码中/失败/可手动发起/闸门关闭四态；HLS 就绪后 panoSrc 切走，本条随条件消失 -->
      <div
        v-if="media.mode.value === 'pano' && media.panoKind.value === 'video' && media.panoOriginalUrl.value"
        class="tc-notice pano-fallback-notice"
        data-testid="pano-fallback-notice"
      >
        <template v-if="media.transcode.value.jobId && !media.transcode.value.failed">
          {{ media.transcode.value.status === 'done' ? '已切换为多码率自适应流' : '转码中…（' + (media.transcode.value.status || 'pending') + '）完成后自动切换' }}
        </template>
        <template v-else-if="media.transcode.value.failed">
          转码发起失败，已保持原始文件播放
        </template>
        <template v-else-if="!media.transcodeDisabled.value">
          未转码，原始文件播放中（清晰度/拖动受限）
          <button class="fb-btn" data-testid="pano-fallback-transcode" @click="media.startTranscode(false)">立即转码</button>
        </template>
        <template v-else>
          原始文件播放中——系统已关闭自动转码，多码率流不可用
        </template>
      </div>

      <!-- 普通视频（v-else-if 显式条件——上方回退提示条是独立 v-if，裸 v-else 会与其配对，
           导致照片/全景/loading 态意外渲染空 video 播放器；e2e 截图目检逮出后修复） -->
      <div v-else-if="media.mode.value === 'video'" class="video-wrap">
        <video ref="plainVideoRef" controls playsinline class="plain-video"></video>
        <!-- Job000124：后台自动转码的轻提示（不遮播放）——进行中「完成后自动切换」、失败「保持原始播放」 -->
        <div v-if="media.mode.value === 'video' && media.transcode.value.jobId && !media.transcode.value.failed" class="tc-notice" data-testid="tc-inline-notice">
          {{ media.transcode.value.status === 'done' ? '已切换为多码率自适应流' : '转码中…（' + (media.transcode.value.status || 'pending') + '）完成后自动切换' }}
        </div>
        <div v-else-if="media.mode.value === 'video' && media.transcode.value.failed" class="tc-notice tc-notice--warn" data-testid="tc-inline-failed">
          转码失败，已保持原始文件播放
        </div>
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
      @share="openShare"
      @deleted="exit"
    />

    <!-- 移动端：信息抽屉 -->
    <div v-if="!isDesktop && drawerOpen" class="drawer-mask" @click.self="drawerOpen = false">
      <div class="drawer">
        <MediaInfoPanel
          :media-id="mediaId"
          :detail="media.detail.value"
          :loading="media.loading.value"
          @share="openShare"
          @close="drawerOpen = false"
          @deleted="exit"
        />
      </div>
    </div>

    <!-- Job000135：创建分享（kind=media；Job000134 扩展「我的分享」管理页与二维码） -->
    <ShareCreateDialog
      v-if="shareOpen"
      kind="media"
      :target-id="mediaId"
      :default-title="media.detail.value?.filename || ''"
      @cancel="shareOpen = false"
      @created="shareOpen = false"
    />
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
import LivePlayer from '../components/player/LivePlayer.vue'
import ShareCreateDialog from '../components/shares/ShareCreateDialog.vue'
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
const liveMode = ref(false) // Job000131 P1：HEVC 兼容播放（用户显式触发实时转码兜底）

// ---- Job000135：全屏/退出全屏 + 分享对话框 ----
const stageRef = ref(null)
const isFs = ref(false)
function toggleFullscreen() {
  const el = stageRef.value
  if (!el) return
  if (document.fullscreenElement) {
    document.exitFullscreen()
  } else {
    el.requestFullscreen().catch(() => {})
  }
}
function onFsChange() {
  isFs.value = !!document.fullscreenElement
}
onMounted(() => document.addEventListener('fullscreenchange', onFsChange))
onBeforeUnmount(() => document.removeEventListener('fullscreenchange', onFsChange))

const shareOpen = ref(false)
function openShare() {
  shareOpen.value = true
}

// ---- Job000136：实时码流信息（performance 资源采样 + 元数据平均码率） ----
const showBitrate = ref(false)
const liveBitrate = ref('')
const bufferSec = ref(null)
const brTimer = ref(null)
let brLastTotal = 0
let brLastAt = 0
function fmtMbps(bps) {
  if (!bps || bps <= 0) return ''
  return bps >= 1e6 ? (bps / 1e6).toFixed(2) + ' Mbps' : Math.round(bps / 1e3) + ' kbps'
}
const avgBitrate = computed(() => {
  const d = media.detail.value
  const dur = d?.video?.duration ?? d?.duration
  if (!d?.filesize || !dur || dur <= 0) return ''
  return fmtMbps((d.filesize * 8) / dur)
})
const specLine = computed(() => {
  const d = media.detail.value
  if (!d) return '—'
  const parts = []
  if (d.width && d.height) parts.push(d.width + '×' + d.height)
  if (d.codec) parts.push(d.codec)
  if (d.video?.fps) parts.push(d.video.fps + 'fps')
  return parts.join(' · ') || '—'
})
function sampleBitrate() {
  const v = stageRef.value && stageRef.value.querySelector('video')
  if (!v || !v.currentSrc) { liveBitrate.value = ''; bufferSec.value = null; return }
  const entries = performance.getEntriesByName(v.currentSrc)
  const total = entries.reduce((sum, e) => sum + (e.transferSize || e.encodedBodySize || 0), 0)
  const now = performance.now()
  if (brLastAt && total >= brLastTotal) {
    const dBytes = total - brLastTotal
    const dSec = (now - brLastAt) / 1000
    if (dSec > 0.5) liveBitrate.value = dBytes > 0 ? fmtMbps((dBytes * 8) / dSec) : '0 kbps'
  }
  brLastTotal = total
  brLastAt = now
  try {
    bufferSec.value = v.buffered.length ? v.buffered.end(v.buffered.length - 1) - v.currentTime : null
  } catch { bufferSec.value = null }
}
function toggleBitrate() {
  showBitrate.value = !showBitrate.value
  if (showBitrate.value) {
    brLastTotal = 0
    brLastAt = 0
    sampleBitrate()
    brTimer.value = setInterval(sampleBitrate, 2000)
  } else if (brTimer.value) {
    clearInterval(brTimer.value)
    brTimer.value = null
  }
}
onBeforeUnmount(() => { if (brTimer.value) clearInterval(brTimer.value) })

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

.fs-btn {
  position: absolute;
  top: 10px;
  left: 10px;
  z-index: 30; /* 高于 360 播放器控制层（z10/20）与 PlaysetNav（z25），与退出/信息按钮同惯例层 */
  width: 36px;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.45);
  color: #fff;
  cursor: pointer;
}
.fs-btn:hover {
  background: rgba(0, 0, 0, 0.65);
}
/* Job000136：码率按钮在全屏按钮右侧 */
.br-btn {
  left: 54px;
}
.br-btn.on {
  background: rgba(37, 99, 235, 0.85);
}
.bitrate-card {
  position: absolute;
  top: 56px;
  left: 10px;
  z-index: 30;
  min-width: 168px;
  padding: 10px 12px;
  border-radius: 8px;
  background: rgba(0, 0, 0, 0.72);
  color: #fff;
  font-size: 12px;
  line-height: 1.7;
  pointer-events: none;
}
.br-row {
  display: flex;
  justify-content: space-between;
  gap: 14px;
}
.br-label { color: rgba(255, 255, 255, 0.62); }
.br-value { font-family: ui-monospace, monospace; font-weight: 600; }
.br-live .br-value { color: #4ade80; }
.br-dim .br-value { color: rgba(255, 255, 255, 0.72); font-weight: 400; }

/* Job000136：移动端紧凑化——播放器控件缩小、信息抽屉全宽、内边距收紧 */
@media (max-width: 768px) {
  .fs-btn {
    width: 32px;
    height: 32px;
    top: 8px;
    left: 8px;
  }
  .br-btn {
    left: 46px;
  }
  .bitrate-card {
    top: 46px;
    left: 8px;
    min-width: 150px;
    font-size: 11px;
    padding: 8px 10px;
  }
  .exit-btn {
    width: 32px;
    height: 32px;
  }
  .drawer {
    width: 100%;
    max-width: 100%;
    border-radius: 12px 12px 0 0;
  }
  .drawer :deep(.info-panel) {
    padding: 12px;
  }
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
  position: relative;
}
.plain-video { max-width: 100%; max-height: 100%; }

/* Job000124：后台自动转码轻提示——贴底悬浮，不遮播放控制条 */
.tc-notice {
  position: absolute;
  bottom: 52px;
  left: 50%;
  transform: translateX(-50%);
  padding: 6px 14px;
  border-radius: var(--radius-sm);
  background: rgba(0, 0, 0, 0.62);
  color: #fff;
  font-size: var(--font-size-sm);
  white-space: nowrap;
  pointer-events: none;
  z-index: 20;
}
.tc-notice--warn {
  background: rgba(180, 40, 40, 0.82);
}

/* Job000126：360 原始回退提示条——含「立即转码」按钮，需恢复指针事件 */
.pano-fallback-notice { pointer-events: auto; display: flex; align-items: center; gap: 8px; }
.fb-btn {
  background: var(--color-primary); color: #fff; border: none;
  border-radius: var(--radius-sm); padding: 3px 10px;
  font-size: var(--font-size-sm); cursor: pointer; white-space: nowrap;
}
.fb-btn:hover { filter: brightness(1.1); }

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
