<template>
  <teleport to="body">
    <div v-if="store.open" class="viewer-mask" @click.self="close">
      <div class="viewer">
        <div class="stage-area">
          <button class="nav-btn prev" :disabled="!store.hasPrev" title="上一张（←）" @click="go(-1)">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
              <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>

          <div ref="stageRef" class="stage" @touchstart.passive="onTouchStart" @touchend="onTouchEnd">
            <div v-if="loadingMedia" class="stage-tip">加载中…</div>

            <template v-else-if="current">
              <transition name="fade" mode="out-in">
                <video
                  v-if="isVideo && mediaUrl"
                  :key="current.id"
                  :src="mediaUrl"
                  class="stage-media"
                  :style="editStyle"
                  controls
                  autoplay
                ></video>
                <img
                  v-else-if="mediaUrl"
                  :key="current.id"
                  :src="mediaUrl"
                  class="stage-media"
                  :style="editStyle"
                  :alt="current.filename"
                />
                <div v-else key="fail" class="stage-tip">媒体加载失败</div>
              </transition>

              <div v-if="is360 && !(editRef && editRef.cropMode)" class="pano-entry">
                <button class="pano-btn" @click="goPano">
                  <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
                    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
                    <ellipse cx="12" cy="12" rx="8.5" ry="3.6" stroke="currentColor" stroke-width="1.4" />
                  </svg>
                  360 播放
                </button>
              </div>

              <!-- 底部工具条：幻灯片 + 基本编辑 -->
              <div class="viewer-toolbar" @click.stop>
                <div class="tb-group">
                  <button class="tb-btn" :title="store.playing ? '暂停幻灯片（空格）' : '播放幻灯片（空格）'" @click="store.togglePlay">
                    <svg v-if="!store.playing" viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M8 5l11 7-11 7z" /></svg>
                    <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor"><path d="M7 5h4v14H7zM13 5h4v14h-4z" /></svg>
                  </button>
                  <select class="tb-select" :value="store.interval" title="幻灯片间隔" @change="onIntervalChange">
                    <option v-for="ms in INTERVALS" :key="ms" :value="ms">{{ ms / 1000 }}s</option>
                  </select>
                  <span class="tb-count">{{ store.index + 1 }} / {{ store.count }}</span>
                  <label class="tb-check" title="幻灯片自动跳过视频与 360">
                    <input type="checkbox" :checked="store.skipVideo360" @change="store.skipVideo360 = $event.target.checked" />
                    跳过视频/360
                  </label>
                </div>

                <ViewerEditTools
                  v-if="editable && current"
                  ref="editRef"
                  :editable="editable"
                  :media-id="String(current.id)"
                  :stage-ref="stageRef"
                  @updated="onEditsUpdated"
                />
              </div>
            </template>
          </div>

          <button class="nav-btn next" :disabled="!store.hasNext" title="下一张（→）" @click="go(1)">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
              <path d="M9 5l7 7-7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>

        <MediaInfoPanel
          v-if="current"
          :media-id="String(current.id)"
          :detail="detail"
          :loading="detailLoading"
          @close="close"
          @deleted="onDeletedFromPanel"
        />
      </div>
    </div>
  </teleport>
</template>

<script setup>
import { computed, ref, watch, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import { loadFullUrl } from '../timeline/mediaLoader'
import MediaInfoPanel from '../player/MediaInfoPanel.vue'
import ViewerEditTools from './ViewerEditTools.vue'
import { useViewerStore, SLIDESHOW_INTERVALS } from '../../stores/viewer'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  items: { type: Array, default: () => [] },
  index: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue', 'update:index', 'deleted'])

const router = useRouter()
const store = useViewerStore()
const INTERVALS = SLIDESHOW_INTERVALS

const detail = ref(null)
const detailLoading = ref(false)
const mediaUrl = ref('')
const loadingMedia = ref(false)
let loadSeq = 0
let slideTimer = 0

// 播放集：props → store 镜像（兼容既有调用方；PlayerView 复用同一播放集）。
// 注意：仅在"打开"或"父级索引变化"时改写 store.index，避免 items 数组换引用时误重置索引。
watch(
  () => [props.modelValue, props.items, props.index],
  ([open, items, index]) => {
    store.setPlayset({ items, source: store.source })
    if (open && !store.open) store.setIndex(index)
    else if (Number(index) !== store.index) store.setIndex(index)
    store.setOpen(open)
  },
  { immediate: true }
)

const current = computed(() => store.current)
const isVideo = computed(() => current.value?.type === 'video')
const is360 = computed(() => !!(detail.value?.is_360 || current.value?.is_360 || current.value?.type === '360'))
// 编辑仅对非 360 照片开放（视频/360 不适用基础旋转裁剪）
const editable = computed(() => !is360.value && !isVideo.value)

// 编辑子系统（ViewerEditTools 自持）：宿主经 expose 消费预览样式，加载后重初始化
const editRef = ref(null)
const editStyle = computed(() => (editRef.value && editRef.value.mediaStyle) || {})
function onEditsUpdated(e) {
  if (detail.value) detail.value.edits = e
}

// 舞台元素：触摸手势（宿主）与裁剪拖动换算/旋转适配（经 prop 交给编辑子组件）共用
const stageRef = ref(null)

/* ---------------- 媒体加载 ---------------- */
watch(
  () => [store.open, store.index, current.value?.id],
  () => {
    if (store.open && current.value) loadCurrent()
  },
  { immediate: true }
)

async function loadCurrent() {
  const item = current.value
  if (!item) return
  const seq = ++loadSeq
  detailLoading.value = true
  loadingMedia.value = true
  detail.value = null
  try {
    const [detailRes, url] = await Promise.all([
      http.get(`/media/${item.id}`),
      loadFullUrl(item)
    ])
    if (seq !== loadSeq) return
    detail.value = detailRes.data
    mediaUrl.value = url
    // 用已保存的编辑参数初始化本地编辑态（含退出裁剪模式）
    editRef.value?.initFromEdits(detailRes.data?.edits || null)
  } catch (e) {
    if (seq !== loadSeq) return
    detail.value = null
    mediaUrl.value = ''
    editRef.value?.initFromEdits(null)
  } finally {
    if (seq === loadSeq) {
      detailLoading.value = false
      loadingMedia.value = false
    }
  }
}

/* ---------------- 导航（含幻灯片） ---------------- */
function go(delta) {
  if (!store.go(delta)) return
  emit('update:index', store.index)
}

function onIntervalChange(e) {
  store.setIntervalMs(Number(e.target.value))
}

watch(
  () => [store.playing, store.interval, store.open, store.index],
  () => {
    clearInterval(slideTimer)
    if (!store.playing || !store.open) return
    if (store.count <= 1) return
    // 幻灯片：自动播放到末尾停止（不循环，避免"永动"意外耗流量）
    slideTimer = setInterval(() => {
      const ok = store.skipVideo360 ? store.nextForSlideshow() : store.go(1)
      if (ok) emit('update:index', store.index)
      else store.pause()
    }, store.interval)
  }
)

/* ---------------- 手势（左右滑） ---------------- */
let touchX = 0
let touchY = 0
function onTouchStart(e) {
  const t = e.changedTouches?.[0]
  if (!t) return
  touchX = t.clientX
  touchY = t.clientY
}
function onTouchEnd(e) {
  const t = e.changedTouches?.[0]
  if (!t) return
  const dx = t.clientX - touchX
  const dy = t.clientY - touchY
  if (Math.abs(dx) < 50 || Math.abs(dx) < Math.abs(dy)) return // 纵向滑动不劫持
  go(dx < 0 ? 1 : -1)
}

/* ---------------- 关闭 / 键盘 ---------------- */
function close() {
  store.setOpen(false)
  emit('update:modelValue', false)
}

function onKeydown(e) {
  if (!store.open) return
  const tag = e.target?.tagName
  const typing = tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT'
  if (e.key === 'Escape') {
    if (typing) return
    if (editRef.value && editRef.value.cropMode) {
      editRef.value.cancelCrop()
      return
    }
    close()
    return
  }
  if (typing) return
  if (e.key === 'ArrowLeft') go(-1)
  else if (e.key === 'ArrowRight') go(1)
  else if (e.key === ' ') {
    e.preventDefault()
    store.togglePlay()
  }
}

// 打开时挂键盘监听（舞台尺寸测量由编辑子组件经 stageRef prop 自持）
watch(
  () => store.open,
  (open) => {
    if (open) {
      window.addEventListener('keydown', onKeydown)
    } else {
      window.removeEventListener('keydown', onKeydown)
    }
  },
  { immediate: true }
)

function goPano() {
  if (!current.value) return
  // 保留播放集，便于 PlayerView 连续播放
  store.setOpen(false)
  emit('update:modelValue', false)
  router.push({ name: 'player', params: { id: current.value.id } })
}

function onDeletedFromPanel(id) {
  emit('deleted', id)
  store.removeById(id)
  emit('update:index', store.index)
  if (!store.items.length) close()
}

onBeforeUnmount(() => {
  clearInterval(slideTimer)
  window.removeEventListener('keydown', onKeydown)
})
</script>

<style scoped>
.viewer-mask {
  position: fixed;
  inset: 0;
  background-color: rgba(0, 0, 0, 0.72);
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
}

.viewer {
  width: min(1200px, 94vw);
  height: min(760px, 92vh);
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  overflow: hidden;
  display: flex;
  box-shadow: var(--shadow-card);
}

.stage-area {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: stretch;
  background-color: var(--player-bg);
}

.stage {
  flex: 1;
  min-width: 0;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: center;
  overflow: hidden;
}

.stage-media {
  width: 100%;
  height: 100%;
  object-fit: contain;
}

.stage-tip {
  color: var(--color-text-disabled);
  font-size: var(--font-size-md);
}

/* 淡入淡出切换 */
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.28s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

.pano-entry {
  position: absolute;
  bottom: 20px;
  left: 0;
  right: 0;
  display: flex;
  justify-content: center;
  z-index: 12;
}

.pano-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: none;
  border-radius: var(--radius-md);
  padding: 8px 18px;
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-md);
}

.pano-btn:hover {
  background-color: var(--color-primary-hover);
}

/* 底部工具条 */
.viewer-toolbar {
  position: absolute;
  left: 50%;
  bottom: 12px;
  transform: translateX(-50%);
  z-index: 18;
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 6px 12px;
  border-radius: 999px;
  background: rgba(20, 24, 29, 0.72);
  backdrop-filter: blur(8px);
  color: var(--color-text-on-dark);
  font-size: var(--font-size-sm);
  max-width: calc(100% - 24px);
  flex-wrap: wrap;
  justify-content: center;
}
.tb-group {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.tb-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  border: none;
  border-radius: var(--radius-sm);
  padding: 5px 8px;
  background: rgba(255, 255, 255, 0.1);
  color: var(--color-text-on-dark);
  font-size: var(--font-size-sm);
}
.tb-btn:hover:not(:disabled) {
  background: rgba(255, 255, 255, 0.2);
}
.tb-btn:disabled {
  opacity: 0.45;
  cursor: default;
}
.tb-btn.primary {
  background: var(--color-primary);
  color: #fff;
}
.tb-select {
  height: 28px;
  border: none;
  border-radius: var(--radius-sm);
  background: rgba(255, 255, 255, 0.1);
  color: var(--color-text-on-dark);
  font-size: var(--font-size-sm);
}
.tb-count {
  font-variant-numeric: tabular-nums;
  opacity: 0.85;
}
.tb-check {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  opacity: 0.85;
}

.nav-btn {
  width: 44px;
  border: none;
  background: transparent;
  color: var(--color-text-disabled);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.nav-btn:hover:not(:disabled) {
  color: #fff;
  background-color: rgba(255, 255, 255, 0.08);
}

.nav-btn:disabled {
  color: var(--player-text-dim);
  cursor: default;
}
</style>
