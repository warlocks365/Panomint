<template>
  <teleport to="body">
    <div v-if="modelValue" class="viewer-mask" @click.self="close">
      <div class="viewer">
        <div class="stage-area">
          <button class="nav-btn prev" :disabled="index <= 0" title="上一张" @click="go(-1)">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
              <path d="M15 5l-7 7 7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>

          <div class="stage">
            <div v-if="loadingMedia" class="stage-tip">加载中…</div>
            <template v-else-if="current">
              <video
                v-if="isVideo && mediaUrl"
                :src="mediaUrl"
                class="stage-media"
                controls
                autoplay
              ></video>
              <img v-else-if="mediaUrl" :src="mediaUrl" class="stage-media" :alt="current.filename" />
              <div v-else class="stage-tip">媒体加载失败</div>

              <div v-if="is360" class="pano-entry">
                <button class="pano-btn" @click="goPano">
                  <svg viewBox="0 0 24 24" width="15" height="15" fill="none">
                    <circle cx="12" cy="12" r="8.5" stroke="currentColor" stroke-width="1.6" />
                    <ellipse cx="12" cy="12" rx="8.5" ry="3.6" stroke="currentColor" stroke-width="1.4" />
                  </svg>
                  360 播放
                </button>
              </div>
            </template>
          </div>

          <button class="nav-btn next" :disabled="index >= items.length - 1" title="下一张" @click="go(1)">
            <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
              <path d="M9 5l7 7-7 7" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </div>

        <aside class="info-panel">
          <header class="info-header">
            <h3 class="info-title" :title="current?.filename">{{ current?.filename }}</h3>
            <button class="close-btn" title="关闭" @click="close">
              <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
                <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
              </svg>
            </button>
          </header>

          <div v-if="detailLoading" class="info-loading">信息加载中…</div>

          <div v-else-if="detail" class="info-body">
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

            <section v-if="detail.tags?.length || detail.people?.length" class="info-section">
              <h4 class="section-title">标签 / 人物</h4>
              <div class="chip-list">
                <span v-for="t in detail.tags" :key="'t' + (t.id ?? t.name ?? t)" class="chip">{{ t.name ?? t }}</span>
                <span v-for="p in detail.people" :key="'p' + (p.id ?? p.name ?? p)" class="chip chip-person">{{ p.name ?? p }}</span>
              </div>
            </section>

            <section class="info-section">
              <h4 class="section-title">评分</h4>
              <div class="stars">
                <button
                  v-for="n in 5"
                  :key="n"
                  class="star"
                  :class="{ on: n <= (detail.rating || 0) }"
                  :title="`${n} 星`"
                  @click="rate(n)"
                >
                  <svg viewBox="0 0 24 24" width="20" height="20" :fill="n <= (detail.rating || 0) ? 'currentColor' : 'none'">
                    <path
                      d="M12 3.6l2.5 5.2 5.7.7-4.2 3.9 1.1 5.6L12 16.2 6.9 19l1.1-5.6-4.2-3.9 5.7-.7z"
                      stroke="currentColor"
                      stroke-width="1.5"
                      stroke-linejoin="round"
                    />
                  </svg>
                </button>
                <button v-if="detail.rating" class="clear-rating" @click="rate(0)">清除</button>
              </div>
            </section>

            <section class="info-section actions">
              <button class="action-btn" :class="{ active: detail.favorite }" @click="toggleFavorite">
                <svg viewBox="0 0 24 24" width="16" height="16" :fill="detail.favorite ? 'currentColor' : 'none'">
                  <path
                    d="M12 20.5s-7.5-4.6-9.3-9.2C1.4 7.9 3.6 4.5 7 4.5c2 0 3.6 1.1 5 2.9 1.4-1.8 3-2.9 5-2.9 3.4 0 5.6 3.4 4.3 6.8-1.8 4.6-9.3 9.2-9.3 9.2z"
                    stroke="currentColor"
                    stroke-width="1.6"
                    stroke-linejoin="round"
                  />
                </svg>
                {{ detail.favorite ? '已收藏' : '收藏' }}
              </button>
              <button class="action-btn danger" :disabled="deleting" @click="onDelete">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none">
                  <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6.5 7l1 13h9l1-13" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
                </svg>
                {{ deleting ? '删除中…' : '删除' }}
              </button>
            </section>
          </div>

          <div v-else class="info-loading">信息加载失败</div>
        </aside>
      </div>
    </div>
  </teleport>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import { loadFullUrl } from '../timeline/mediaLoader'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  items: { type: Array, default: () => [] },
  index: { type: Number, default: 0 }
})
const emit = defineEmits(['update:modelValue', 'update:index', 'deleted'])

const router = useRouter()

const detail = ref(null)
const detailLoading = ref(false)
const mediaUrl = ref('')
const loadingMedia = ref(false)
const deleting = ref(false)
let loadSeq = 0

const current = computed(() => props.items[props.index] || null)
const isVideo = computed(() => current.value?.type === 'video')
const is360 = computed(() => !!(detail.value?.is_360 || current.value?.is_360 || current.value?.type === '360'))

const typeLabel = computed(() => {
  if (is360.value) return '360 全景'
  if (isVideo.value) return '视频'
  return '照片'
})

const hasExif = computed(() => {
  const ex = detail.value?.exif
  return ex && Object.values(ex).some((v) => v !== null && v !== '' && v !== undefined)
})

const cameraText = computed(() => {
  const ex = detail.value?.exif || {}
  return [ex.camera_make, ex.camera_model].filter(Boolean).join(' ')
})

const exifParams = computed(() => {
  const ex = detail.value?.exif || {}
  const parts = []
  if (ex.focal_length) parts.push(`${ex.focal_length}`)
  if (ex.aperture) parts.push(`f/${ex.aperture}`)
  if (ex.shutter_speed) parts.push(`${ex.shutter_speed}s`)
  if (ex.iso) parts.push(`ISO ${ex.iso}`)
  return parts.join(' · ')
})

const gpsText = computed(() => {
  const gps = detail.value?.gps
  if (!gps) return ''
  if (Array.isArray(gps)) return gps.join(', ')
  if (gps.lat != null && gps.lon != null) return `${gps.lat}, ${gps.lon}`
  return ''
})

watch(
  () => [props.modelValue, props.index, current.value?.id],
  () => {
    if (props.modelValue && current.value) loadCurrent()
  },
  { immediate: true }
)

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      window.addEventListener('keydown', onKeydown)
    } else {
      window.removeEventListener('keydown', onKeydown)
      revokeCurrent()
    }
  }
)

async function loadCurrent() {
  const item = current.value
  if (!item) return
  const seq = ++loadSeq
  detailLoading.value = true
  loadingMedia.value = true
  detail.value = null
  revokeCurrent()
  try {
    const [detailRes, url] = await Promise.all([
      http.get(`/media/${item.id}`),
      loadFullUrl(item)
    ])
    if (seq !== loadSeq) return
    detail.value = detailRes.data
    mediaUrl.value = url
  } catch (e) {
    if (seq !== loadSeq) return
    detail.value = null
    mediaUrl.value = ''
  } finally {
    if (seq === loadSeq) {
      detailLoading.value = false
      loadingMedia.value = false
    }
  }
}

function revokeCurrent() {
  // 大图 URL 由 mediaLoader 缓存统一管理，此处不主动 revoke
}

function go(delta) {
  const next = props.index + delta
  if (next < 0 || next >= props.items.length) return
  emit('update:index', next)
}

function close() {
  emit('update:modelValue', false)
}

function onKeydown(e) {
  if (!props.modelValue) return
  if (e.key === 'Escape') close()
  else if (e.key === 'ArrowLeft') go(-1)
  else if (e.key === 'ArrowRight') go(1)
}

function goPano() {
  if (!current.value) return
  close()
  router.push(`/player/${current.value.id}`)
}

async function rate(n) {
  if (!current.value) return
  try {
    await http.post(`/media/${current.value.id}/rate`, { rating: n })
    if (detail.value) detail.value.rating = n
  } catch (e) {
    // 静默失败，保持界面原状
  }
}

async function toggleFavorite() {
  if (!current.value || !detail.value) return
  const next = !detail.value.favorite
  try {
    await http.post(`/media/${current.value.id}/favorite`, { favorite: next })
    detail.value.favorite = next
  } catch (e) {
    // 静默失败
  }
}

async function onDelete() {
  if (!current.value || deleting.value) return
  if (!window.confirm(`确定删除「${current.value.filename}」吗？文件将移入回收站。`)) return
  deleting.value = true
  try {
    await http.delete(`/media/${current.value.id}`)
    emit('deleted', current.value.id)
    if (!props.items.length) close()
  } catch (e) {
    window.alert('删除失败，请稍后重试')
  } finally {
    deleting.value = false
  }
}

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
  const m = Math.floor(s / 60)
  return `${m}:${String(s % 60).padStart(2, '0')}`
}

function formatSize(bytes) {
  if (!bytes) return '—'
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`
}
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
  background-color: #111318;
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
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
}

.stage-tip {
  color: #a8acb3;
  font-size: var(--font-size-md);
}

.pano-entry {
  position: absolute;
  bottom: 20px;
  left: 0;
  right: 0;
  display: flex;
  justify-content: center;
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

.nav-btn {
  width: 44px;
  border: none;
  background: transparent;
  color: #a8acb3;
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
  color: #4a4f57;
  cursor: default;
}

.info-panel {
  width: 300px;
  flex-shrink: 0;
  border-left: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  overflow-y: auto;
}

.info-header {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 16px 16px 12px;
  border-bottom: 1px solid var(--color-border);
}

.info-title {
  flex: 1;
  margin: 0;
  font-size: var(--font-size-lg);
  word-break: break-all;
}

.close-btn {
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  padding: 4px;
  border-radius: var(--radius-sm);
}

.close-btn:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.info-loading {
  padding: 24px 16px;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.info-body {
  padding: 12px 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.section-title {
  margin: 0 0 8px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: 600;
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

.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.chip {
  padding: 2px 10px;
  border-radius: 10px;
  font-size: var(--font-size-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
}

.chip-person {
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}

.stars {
  display: flex;
  align-items: center;
  gap: 2px;
}

.star {
  border: none;
  background: transparent;
  padding: 2px;
  color: var(--color-text-disabled);
}

.star.on,
.star:hover {
  color: var(--color-primary);
}

.clear-rating {
  margin-left: 8px;
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.clear-rating:hover {
  color: var(--color-text-primary);
}

.actions {
  display: flex;
  gap: 10px;
}

.action-btn {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 8px 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.action-btn:hover {
  background-color: var(--color-surface-hover);
}

.action-btn.active {
  color: var(--color-danger);
  border-color: var(--color-danger);
}

.action-btn.danger {
  color: var(--color-danger);
}

.action-btn.danger:hover {
  background-color: var(--color-surface-hover);
}
</style>
