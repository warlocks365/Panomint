<template>
  <section class="info-section">
    <dl class="info-list">
      <div class="info-row">
        <dt>拍摄时间</dt>
        <dd>{{ formatTime(detail.taken_at) }}</dd>
      </div>
      <div class="info-row">
        <dt>类型</dt>
        <dd>{{ typeLabel }}</dd>
      </div>
      <div class="info-row">
        <dt>尺寸</dt>
        <dd>{{ detail.width }} × {{ detail.height }}</dd>
      </div>
      <div v-if="detail.video?.duration || detail.duration" class="info-row">
        <dt>时长</dt>
        <dd>{{ formatDuration(detail.video?.duration ?? detail.duration) }}</dd>
      </div>
      <div v-if="detail.codec" class="info-row">
        <dt>编码</dt>
        <dd>{{ detail.codec }}</dd>
      </div>
      <div v-if="detail.filesize" class="info-row">
        <dt>大小</dt>
        <dd>{{ formatSize(detail.filesize) }}</dd>
      </div>
      <div v-if="detail.place" class="info-row">
        <dt>地点</dt>
        <dd>{{ detail.place }}</dd>
      </div>
      <div v-else-if="gpsText" class="info-row">
        <dt>GPS</dt>
        <dd>{{ gpsText }}</dd>
      </div>
    </dl>
  </section>

  <section v-if="hasExif" class="info-section">
    <h4 class="section-title">EXIF</h4>
    <dl class="info-list">
      <div v-if="cameraText" class="info-row">
        <dt>相机</dt>
        <dd>{{ cameraText }}</dd>
      </div>
      <div v-if="detail.exif.lens_model" class="info-row">
        <dt>镜头</dt>
        <dd>{{ detail.exif.lens_model }}</dd>
      </div>
      <div v-if="exifParams" class="info-row">
        <dt>参数</dt>
        <dd>{{ exifParams }}</dd>
      </div>
    </dl>
  </section>

  <section v-if="detail.video" class="info-section">
    <h4 class="section-title">视频信息</h4>
    <dl class="info-list">
      <div v-if="detail.video.fps" class="info-row">
        <dt>帧率</dt>
        <dd>{{ detail.video.fps }} fps</dd>
      </div>
      <div class="info-row">
        <dt>HDR</dt>
        <dd>{{ detail.video.hdr ? '是' : '否' }}</dd>
      </div>
    </dl>
  </section>
</template>

<script setup>
// MediaInfoPanel 拆解（Job000082）：基本信息 / EXIF / 视频信息 三个纯展示 section。
// 只读 detail，无任何写操作或本地状态。
import { computed } from 'vue'

const props = defineProps({
  detail: { type: Object, required: true }
})

const is360 = computed(() => !!(props.detail?.is_360 || props.detail?.type === '360'))
const typeLabel = computed(() => (is360.value ? '360 全景' : props.detail?.type === 'video' ? '视频' : '照片'))

const hasExif = computed(() => {
  const ex = props.detail?.exif
  return ex && Object.values(ex).some((v) => v !== null && v !== '' && v !== undefined)
})

const cameraText = computed(() => {
  const ex = props.detail?.exif || {}
  return [ex.camera_make, ex.camera_model].filter(Boolean).join(' ')
})

const exifParams = computed(() => {
  const ex = props.detail?.exif || {}
  const parts = []
  if (ex.focal_length) parts.push(`${ex.focal_length}`)
  if (ex.aperture) parts.push(`f/${ex.aperture}`)
  if (ex.shutter_speed) parts.push(`${ex.shutter_speed}s`)
  if (ex.iso) parts.push(`ISO ${ex.iso}`)
  return parts.join(' · ')
})

const gpsText = computed(() => {
  const gps = props.detail?.gps
  if (!gps) return ''
  if (Array.isArray(gps)) return gps.join(', ')
  if (gps.lat != null && gps.lon != null) return `${gps.lat}, ${gps.lon}`
  return ''
})

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function formatDuration(sec) {
  if (!sec && sec !== 0) return '—'
  const s = Math.round(sec)
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}

function formatSize(bytes) {
  if (!bytes) return '—'
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}
</script>

<style scoped>
.section-title {
  margin: 0 0 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: 600;
  display: flex;
  align-items: center;
  gap: 8px;
}

.info-list {
  margin: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.info-row {
  display: flex;
  gap: 12px;
  font-size: var(--font-size-sm);
}

.info-row dt {
  width: 60px;
  flex-shrink: 0;
  color: var(--color-text-secondary);
}

.info-row dd {
  margin: 0;
  color: var(--color-text-primary);
  word-break: break-all;
}
</style>
