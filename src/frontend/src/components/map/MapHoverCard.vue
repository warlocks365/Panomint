<template>
  <div
    class="hover-card"
    :style="cardStyle"
    @mouseenter="onCardEnter"
    @mouseleave="onCardLeave"
    @touchstart="onCardTouchStart"
    @touchend="onCardTouchEnd"
  >
    <div v-if="loading" class="hc-loading">加载中…</div>
    <template v-else-if="items.length">
      <div class="hc-place">{{ place }}</div>

      <!-- 主显预览图 + 翻书 -->
      <div
        class="hc-stage"
        @pointerdown="onSwipeDown"
        @pointerup="onSwipeUp"
      >
        <div
          class="hc-book"
          :style="{ transform: 'translateX(' + (-mainIndex * 100) + '%)' }"
        >
          <div v-for="it in items" :key="it.id" class="hc-page">
            <img v-if="thumbs[it.id]" :src="thumbs[it.id]" :alt="it.filename" />
            <div v-else class="hc-ph"></div>
            <span v-if="it.is_360" class="hc-badge">360</span>
          </div>
        </div>
        <span v-if="items.length > 1" class="hc-counter">{{ mainIndex + 1 }}/{{ items.length }}</span>
      </div>

      <!-- 缩略图条（可点选） -->
      <div class="hc-strip">
        <button
          v-for="(it, i) in items"
          :key="it.id"
          class="hc-mini"
          :class="{ active: i === mainIndex }"
          type="button"
          @click.stop="openItem(it)"
          @mouseenter="mainIndex = i"
        >
          <img v-if="thumbs[it.id]" :src="thumbs[it.id]" :alt="it.filename" />
          <div v-else class="hc-ph"></div>
        </button>
      </div>

      <div class="hc-stats">
        照片 {{ counts.photos }} · 视频 {{ counts.videos }} · 全景照片 {{ counts.panoPhotos }} · 全景视频 {{ counts.panoVideos }}
      </div>
    </template>
    <div v-else class="hc-loading">没有媒体</div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { thumbBlobUrl } from '../../api/map'

const props = defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  place: { type: String, default: '' },
  pos: { type: Object, default: null } // { x, y } 屏幕坐标
})
const emit = defineEmits(['open', 'enter', 'leave'])

const mainIndex = ref(0)

// 缩略图 blob URL（全部加载，供翻书和缩略图条）
const thumbs = reactive({})
const objectUrls = []
watch(
  () => props.items,
  (list) => {
    mainIndex.value = 0
    for (const it of list) {
      if (thumbs[it.id]) continue
      thumbBlobUrl(it.id)
        .then((url) => {
          thumbs[it.id] = url
          objectUrls.push(url)
        })
        .catch(() => {})
    }
  },
  { immediate: true }
)
onBeforeUnmount(() => objectUrls.forEach((u) => URL.revokeObjectURL(u)))

const counts = computed(() =>
  props.items.reduce(
    (acc, it) => {
      const is360 = !!it.is_360
      if (it.type === 'photo' && !is360) acc.photos++
      else if (it.type === 'video' && !is360) acc.videos++
      else if (it.type === 'photo' && is360) acc.panoPhotos++
      else if (it.type === 'video' && is360) acc.panoVideos++
      return acc
    },
    { photos: 0, videos: 0, panoPhotos: 0, panoVideos: 0 }
  )
)

function openItem(it) {
  emit('open', it)
}

// 翻书手势：横向滑动切换主显
let swipeStartX = null
function onSwipeDown(e) {
  swipeStartX = e.clientX
}
function onSwipeUp(e) {
  if (swipeStartX === null) return
  const dx = e.clientX - swipeStartX
  swipeStartX = null
  if (Math.abs(dx) < 30) return // 阈值：<30px 视为点击而非滑动
  if (dx < 0) next()
  else prev()
}
function next() {
  if (mainIndex.value < props.items.length - 1) mainIndex.value++
}
function prev() {
  if (mainIndex.value > 0) mainIndex.value--
}

function onCardEnter() {
  emit('enter')
}
function onCardLeave() {
  emit('leave')
}
function onCardTouchStart() {
  emit('enter')
}
function onCardTouchEnd() {
  emit('leave')
}

// 方向自适应（加大尺寸：宽 320、高约 320）
const CARD_W = 320
const CARD_H = 340
const MARGIN = 12

const cardStyle = computed(() => {
  if (!props.pos) return { left: '12px', top: '12px' }
  const { x, y } = props.pos
  const vw = window.innerWidth
  const vh = window.innerHeight
  let left = x + 16
  let top = y + 16
  if (left + CARD_W + MARGIN > vw) left = x - CARD_W - 16
  if (top + CARD_H + MARGIN > vh) top = y - CARD_H - 16
  left = Math.max(MARGIN, Math.min(left, vw - CARD_W - MARGIN))
  top = Math.max(MARGIN, Math.min(top, vh - CARD_H - MARGIN))
  return { left: left + 'px', top: top + 'px' }
})
</script>

<style scoped>
.hover-card {
  position: fixed;
  width: 320px;
  background: rgba(255, 255, 255, 0.99);
  border: 1px solid rgba(15, 23, 42, 0.12);
  border-radius: 12px;
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.2);
  padding: 12px 14px;
  z-index: 20;
  pointer-events: auto; /* 可点选缩略图 */
}

.hc-loading {
  font-size: 12px;
  color: #94a3b8;
  padding: 12px 0;
  text-align: center;
}

.hc-place {
  font-size: 13px;
  font-weight: 600;
  color: #0f172a;
  margin-bottom: 10px;
}

.hc-stage {
  position: relative;
  width: 100%;
  aspect-ratio: 16 / 10;
  border-radius: 8px;
  overflow: hidden;
  background: #e2e8f0;
  cursor: grab;
  touch-action: pan-y;
}

.hc-book {
  display: flex;
  width: 100%;
  height: 100%;
  transition: transform 0.3s ease;
}

.hc-page {
  position: relative;
  flex: 0 0 100%;
  height: 100%;
}

.hc-page img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.hc-ph {
  width: 100%;
  height: 100%;
  background: #cbd5e1;
}

.hc-badge {
  position: absolute;
  top: 6px;
  left: 6px;
  background: rgba(37, 99, 235, 0.92);
  color: #fff;
  font-size: 11px;
  border-radius: 4px;
  padding: 1px 6px;
}

.hc-counter {
  position: absolute;
  right: 8px;
  bottom: 8px;
  background: rgba(15, 23, 42, 0.6);
  color: #fff;
  font-size: 11px;
  border-radius: 8px;
  padding: 1px 8px;
  pointer-events: none;
}

.hc-strip {
  display: flex;
  gap: 5px;
  margin-top: 8px;
  overflow-x: auto;
  padding-bottom: 2px;
}

.hc-strip::-webkit-scrollbar {
  height: 4px;
}

.hc-mini {
  flex: 0 0 40px;
  width: 40px;
  height: 40px;
  border: 2px solid transparent;
  border-radius: 6px;
  padding: 0;
  cursor: pointer;
  overflow: hidden;
  background: #e2e8f0;
}

.hc-mini.active {
  border-color: #2563eb;
}

.hc-mini img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.hc-stats {
  font-size: 11px;
  color: #64748b;
  margin-top: 8px;
}
</style>
