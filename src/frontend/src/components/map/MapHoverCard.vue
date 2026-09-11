<template>
  <div class="hover-card" :style="cardStyle">
    <div v-if="loading" class="hc-loading">加载中…</div>
    <template v-else>
      <div class="hc-place">{{ place }}</div>
      <div class="hc-grid">
        <div v-for="it in previewItems" :key="it.id" class="hc-thumb" :class="'hc-' + it.type + (it.is_360 ? '-360' : '')">
          <img v-if="thumbs[it.id]" :src="thumbs[it.id]" :alt="it.filename" />
          <div v-else class="hc-ph"></div>
          <span v-if="it.is_360" class="hc-badge">360</span>
        </div>
      </div>
      <div class="hc-stats">
        照片 {{ counts.photos }} · 视频 {{ counts.videos }} · 全景照片 {{ counts.panoPhotos }} · 全景视频 {{ counts.panoVideos }}
      </div>
      <div class="hc-hint">点击查看全部</div>
    </template>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, reactive, watch } from 'vue'
import { thumbBlobUrl } from '../../api/map'

const props = defineProps({
  items: { type: Array, default: () => [] },
  loading: { type: Boolean, default: false },
  place: { type: String, default: '' },
  pos: { type: Object, default: null } // { x, y } 屏幕坐标（鼠标/触摸点）
})

// 缩略图 blob URL（前 3 个）
const thumbs = reactive({})
const objectUrls = []
watch(
  () => props.items,
  (list) => {
    for (const it of list.slice(0, 3)) {
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

const previewItems = computed(() => props.items.slice(0, 3))

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

// 方向自适应：预览窗宽约 240、高约 120，优先出现在触点右下方，空间不足翻转到左/上
const CARD_W = 240
const CARD_H = 150
const MARGIN = 12

const cardStyle = computed(() => {
  if (!props.pos) return { left: '12px', top: '12px' }
  const { x, y } = props.pos
  const vw = window.innerWidth
  const vh = window.innerHeight
  let left = x + 14
  let top = y + 14
  if (left + CARD_W + MARGIN > vw) left = x - CARD_W - 14 // 右空间不足 → 左侧
  if (top + CARD_H + MARGIN > vh) top = y - CARD_H - 14 // 下空间不足 → 上方
  // 兜底：极端情况钳制到屏幕内
  left = Math.max(MARGIN, Math.min(left, vw - CARD_W - MARGIN))
  top = Math.max(MARGIN, Math.min(top, vh - CARD_H - MARGIN))
  return { left: left + 'px', top: top + 'px' }
})
</script>

<style scoped>
.hover-card {
  position: fixed;
  width: 240px;
  background: rgba(255, 255, 255, 0.98);
  border: 1px solid rgba(15, 23, 42, 0.1);
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(15, 23, 42, 0.16);
  padding: 10px 12px;
  z-index: 20;
  pointer-events: none; /* 不挡鼠标事件，避免抖动 */
}

.hc-loading {
  font-size: 12px;
  color: #94a3b8;
  padding: 12px 0;
  text-align: center;
}

.hc-place {
  font-size: 12px;
  font-weight: 600;
  color: #0f172a;
  margin-bottom: 8px;
}

.hc-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 4px;
  margin-bottom: 8px;
}

.hc-thumb {
  position: relative;
  aspect-ratio: 1;
  border-radius: 4px;
  overflow: hidden;
  background: #e2e8f0;
}

.hc-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.hc-ph {
  width: 100%;
  height: 100%;
}

.hc-badge {
  position: absolute;
  top: 2px;
  left: 2px;
  background: rgba(37, 99, 235, 0.92);
  color: #fff;
  font-size: 9px;
  border-radius: 3px;
  padding: 0 3px;
}

.hc-stats {
  font-size: 11px;
  color: #64748b;
  margin-bottom: 4px;
}

.hc-hint {
  font-size: 11px;
  color: #94a3b8;
}
</style>
