<template>
  <div class="media-grid">
    <button
      v-for="m in items"
      :key="m.id"
      class="media-cell"
      type="button"
      @click="$emit('open', m)"
    >
      <img v-if="thumbUrl(m.id) && thumbUrl(m.id) !== 'x-failed'" :src="thumbUrl(m.id)" :alt="m.filename || ''" class="thumb" loading="lazy" />
      <div v-else-if="thumbUrl(m.id) === 'x-failed'" class="thumb-placeholder">
        <svg viewBox="0 0 24 24" width="26" height="26" fill="none" class="thumb-failed-icon">
          <rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5" />
          <path d="M3 15l5-5 4 4 3-3 6 6" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
        </svg>
      </div>
      <div v-else class="thumb-placeholder">
        <div class="spinner sm"></div>
      </div>
      <span v-if="isVideo(m)" class="badge video-badge">
        <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor">
          <path d="M8 5.5v13l11-6.5z" />
        </svg>
        {{ durationLabel(m) }}
      </span>
      <span v-if="is360(m)" class="badge pano-badge">360</span>
      <ShareDownloadBtn :token="token" :id="m.id" :filename="m.filename" :password="password" :allowed="allowDownload" />
    </button>
  </div>
</template>

<script setup>
// SharePublicView 拆解（Job000084）：媒体网格单元。
// 缩略图三态（在途/成功/失败占位）由父组件注入 thumbUrl 查询函数
// （宿主持有 reactive Map 与回收责任，子组件只读不拷贝）；
// 类型判定/时长标签为纯函数。点击整格上报 open，分派逻辑在宿主。
import ShareDownloadBtn from './ShareDownloadBtn.vue'

defineProps({
  items: { type: Array, required: true },
  thumbUrl: { type: Function, required: true }, // (id) -> objectURL | 'x-failed' | ''
  token: { type: String, required: true },
  password: { type: String, default: '' },
  allowDownload: { type: Boolean, default: false }
})

defineEmits(['open'])

function isVideo(m) {
  // 360 视频走全景播放，不进普通播放器
  return m.type === 'video' && !is360(m)
}

function is360(m) {
  // MediaRef.type 恒为枚举原值（photo|video），360 以独立 is_360 标记；'360' 兼容旧契约
  return !!m.is_360 || m.type === '360'
}

function durationLabel(m) {
  const d = Number(m.duration)
  if (!d || !Number.isFinite(d)) return ''
  const mm = Math.floor(d / 60)
  const ss = Math.floor(d % 60)
  return `${mm}:${String(ss).padStart(2, '0')}`
}
</script>

<style scoped>
.media-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 3px;
  padding: 0 3px;
}

.media-cell {
  position: relative;
  aspect-ratio: 1 / 1;
  padding: 0;
  border: none;
  background-color: var(--color-surface-hover);
  cursor: pointer;
  overflow: hidden;
}

.thumb {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
}

.thumb-failed-icon {
  color: var(--color-text-disabled);
}

.badge {
  position: absolute;
  display: flex;
  align-items: center;
  gap: 3px;
  padding: 2px 6px;
  border-radius: var(--radius-sm);
  font-size: 10px;
  color: #fff;
  background-color: rgba(0, 0, 0, 0.55);
}

.video-badge {
  right: 4px;
  bottom: 4px;
}

.pano-badge {
  left: 4px;
  top: 4px;
  background-color: var(--color-primary);
}

.spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--color-border);
  border-top-color: var(--color-primary);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

.spinner.sm {
  width: 20px;
  height: 20px;
  border-width: 2px;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
