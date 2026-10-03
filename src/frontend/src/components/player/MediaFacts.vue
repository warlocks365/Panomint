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
      <!-- 拍摄地 / 详细地址 / GPS：空值也**保留行**（显示「没有数据」）。
           早期版本用 v-if 门控，空值时整行消失 —— 用户只看到一个「编辑拍摄信息」
           按钮，看不出这几项存在，也就无从知道可以编辑。
           拍摄时间是常驻项（媒体必有 EXIF 或导入时间）。 -->
      <div class="info-row">
        <dt>拍摄地</dt>
        <dd :class="{ 'info-empty': !detail.place }">{{ detail.place || '没有数据' }}</dd>
      </div>
      <div class="info-row">
        <dt>详细地址</dt>
        <dd :class="{ 'info-empty': !detail.address }">{{ detail.address || '没有数据' }}</dd>
      </div>
      <div class="info-row">
        <dt>GPS</dt>
        <dd :class="{ 'info-empty': !gpsText }">{{ gpsText || '没有数据' }}</dd>
      </div>
    </dl>

    <!-- Job000143：元数据编辑入口。四类媒体（照片/视频/360照片/360视频）共用本面板，
         故此处一次改动即全覆盖，无需按类型分别实现。 -->
    <button v-if="!editing" class="edit-meta-btn" type="button" data-testid="edit-metadata" @click="startEdit">
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.8"
        stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M4 20h4l10-10-4-4L4 16v4z" />
        <path d="M13.5 6.5l4 4" />
      </svg>
      <span>编辑拍摄信息</span>
    </button>

    <MetadataEditor
      v-else
      :detail="detail"
      @saved="onSaved"
      @cancel="editing = false"
    />
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
import { computed, ref } from 'vue'
import MetadataEditor from './MetadataEditor.vue'

const props = defineProps({
  detail: { type: Object, required: true }
})
const emit = defineEmits(['metadata-saved'])

const editing = ref(false)
function startEdit() {
  editing.value = true
}
function onSaved(resp) {
  editing.value = false
  // 用服务端归一化后的值就地更新面板，避免整条 detail 重拉（那会打断正在播放的媒体）
  if (resp) {
    const d = props.detail
    if ('taken_at' in resp) d.taken_at = resp.taken_at
    if ('place' in resp) d.place = resp.place
    if ('address' in resp) d.address = resp.address
    if ('gps' in resp) {
      // 两个键都写上：后端发 lng，但既有代码有读 lon 的地方。
      // 只写一个会让「保存过一次」与「没保存过」表现不同（实测踩过）。
      d.gps = resp.gps
        ? { lat: resp.gps.lat, lng: resp.gps.lng, lon: resp.gps.lng }
        : null
    }
  }
  emit('metadata-saved', resp)
}

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

// GPS 展示文本。
//
// 🔴 键名：后端 `media.Detail.Geo` 的 JSON tag 是 **`lng`**（Go 字段名 Lng），
//    这里原来读 `gps.lon` → 永远 undefined → 整行被 v-if 隐藏，
//    表现为「有 GPS 的媒体却不显示 GPS 行」。e2e 实测逮到（API 返回
//    {"lat":39.909187,"lng":116.397463} 而面板无 GPS 行）。
//    两种键名都兼容：不同接口（列表 /search、geo/*）的形状不完全一致。
const gpsText = computed(() => {
  const gps = props.detail?.gps
  if (!gps) return ''
  if (Array.isArray(gps)) {
    return gps.length >= 2 ? `${gps[1]}, ${gps[0]}` : gps.join(', ')
  }
  const lat = gps.lat ?? gps.latitude
  // lng / lon 都接受：后端 Detail 用 lng，GeoJSON 惯例用 lon
  const lng = gps.lng ?? gps.lon ?? gps.longitude
  if (lat == null || lng == null) return ''
  // 6 位小数 ≈ 0.11 米，足够精确又不至于喧宾夺主
  return `${Number(lat).toFixed(6)}, ${Number(lng).toFixed(6)}`
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

/* 编辑入口：低调的胶囊按钮，与面板内既有的操作钮同语言 */
/* 空值：弱化显示，让用户知道「有这项但没填」而不是「没这项」。
   用既有的 --color-text-disabled（tokens.css 里没有 tertiary，别硬编码色值）。 */
.info-empty {
  color: var(--color-text-disabled);
}

.edit-meta-btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 10px;
  padding: 5px 12px;
  font-size: var(--font-size-xs);
  font-family: inherit;
  color: var(--color-text-secondary);
  background: transparent;
  border: none;
  border-radius: 999px;
  box-shadow: inset 0 0 0 1px var(--color-border));
  cursor: pointer;
  transition: color 0.16s ease, box-shadow 0.16s ease;
}

.edit-meta-btn:hover {
  color: var(--color-text-primary);
  box-shadow: inset 0 0 0 1px var(--color-primary);
}
</style>
