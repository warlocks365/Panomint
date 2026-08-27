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
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import http from '../../api/http'
import { loadFullUrl } from '../timeline/mediaLoader'
import MediaInfoPanel from '../player/MediaInfoPanel.vue'

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
let loadSeq = 0

const current = computed(() => props.items[props.index] || null)
const isVideo = computed(() => current.value?.type === 'video')
const is360 = computed(() => !!(detail.value?.is_360 || current.value?.is_360 || current.value?.type === '360'))

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
  if (e.key === 'Escape') {
    // 输入框/文本域内的 Esc 留给控件自身
    const tag = e.target?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA') return
    close()
  } else if (e.key === 'ArrowLeft') go(-1)
  else if (e.key === 'ArrowRight') go(1)
}

function goPano() {
  if (!current.value) return
  close()
  router.push(`/player/${current.value.id}`)
}

function onDeletedFromPanel(id) {
  emit('deleted', id)
  if (!props.items.length) close()
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
</style>
