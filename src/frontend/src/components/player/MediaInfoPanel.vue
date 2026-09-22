<template>
  <aside class="info-panel">
    <header class="info-header">
      <h3 class="info-title" :title="detail?.filename">{{ detail?.filename || '…' }}</h3>
      <button v-if="showClose" class="close-btn" title="关闭" @click="$emit('close')">
        <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
          <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
        </svg>
      </button>
    </header>

    <div v-if="loading" class="info-loading">信息加载中…</div>

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

      <!-- 备注：失焦或点保存提交 PATCH /media/:id {notes} -->
      <section class="info-section">
        <h4 class="section-title">
          备注
          <span class="notes-state" :class="notesState">{{ notesStateText }}</span>
        </h4>
        <textarea
          v-model="notesText"
          class="notes-input"
          rows="3"
          placeholder="为这个媒体写点备注…"
          :disabled="notesState === 'saving'"
          @blur="saveNotes"
          @keydown.esc.stop
        ></textarea>
        <button
          class="notes-save"
          :disabled="notesState === 'saving' || !notesDirty"
          @click="saveNotes"
        >
          {{ notesState === 'saving' ? '保存中…' : '保存备注' }}
        </button>
      </section>

      <!-- 标签：chips 可移除，输入可创建或从自动补全选择 -->
      <section class="info-section">
        <h4 class="section-title">标签</h4>
        <div v-if="tags.length || people.length" class="chip-list">
          <span
            v-for="t in tags"
            :key="tagKey(t)"
            class="chip tag-chip"
            :class="{ 'chip-ai': tagKind(t) === 'ai' }"
            :style="tagStyle(t)"
          >
            <i v-if="tagKind(t) === 'ai'" class="ai-mark">AI</i>
            {{ tagName(t) }}
            <button
              v-if="t && t.id != null"
              class="chip-x"
              title="移除标签"
              @click="removeTag(t)"
            >
              <svg viewBox="0 0 12 12" width="9" height="9" fill="none">
                <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </span>
          <span v-for="p in people" :key="'p' + (p.id ?? p.name ?? p)" class="chip chip-person">{{ p.name ?? p }}</span>
        </div>

        <div class="tag-editor">
          <input
            v-model="tagInput"
            class="tag-input"
            type="text"
            placeholder="输入标签名，回车添加"
            @input="onTagInput"
            @keydown.enter.prevent="onTagEnter"
            @keydown.esc.stop="closeSuggest"
            @focus="onTagInput"
            @blur="onTagBlur"
          />
          <div v-if="suggestOpen" class="suggest">
            <div v-if="suggLoading" class="suggest-tip">搜索中…</div>
            <template v-else>
              <button
                v-for="s in suggestions"
                :key="s.id"
                class="suggest-item"
                @mousedown.prevent="pickSuggest(s)"
              >
                <span class="suggest-name" :class="{ ai: s.kind === 'ai' }">{{ s.name }}</span>
                <span v-if="s.usage_count != null || s.count != null" class="suggest-count">{{ s.usage_count ?? s.count }}</span>
              </button>
              <button
                v-if="tagInput.trim() && !suggestions.some((s) => s.name === tagInput.trim())"
                class="suggest-item create"
                @mousedown.prevent="addTag(tagInput.trim())"
              >
                创建标签「{{ tagInput.trim() }}」
              </button>
              <div v-if="!suggestions.length && !tagInput.trim()" class="suggest-tip">输入以搜索已有标签</div>
            </template>
          </div>
        </div>
        <p v-if="tagError" class="tag-error">{{ tagError }}</p>
      </section>

      <!-- AI 待确认：origin='ai' 且 confirmed=false 的标签，接受后才计入正式标签 -->
      <section v-if="pendingAI.length" class="info-section">
        <h4 class="section-title">
          AI 标签（待确认）
          <button class="accept-all" :disabled="aiBusy" @click="acceptAllAI">全部接受</button>
        </h4>
        <div class="chip-list">
          <span v-for="t in pendingAI" :key="'ai' + t.id" class="chip chip-ai">
            <i class="ai-mark">AI</i>
            {{ t.name }}
            <button class="chip-x" title="接受该标签" @click="acceptAI(t)">
              <svg viewBox="0 0 12 12" width="9" height="9" fill="none">
                <path d="M2.5 6.5l2.5 2.5L9.5 4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
              </svg>
            </button>
            <button class="chip-x" title="移除该标签" @click="removeTag(t)">
              <svg viewBox="0 0 12 12" width="9" height="9" fill="none">
                <path d="M2 2l8 8M10 2l-8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
              </svg>
            </button>
          </span>
        </div>
        <p class="ai-hint">AI 自动建议，点 ✓ 接受、✕ 移除或「全部接受」。</p>
        <p v-if="tagError" class="tag-error">{{ tagError }}</p>
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
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import http from '../../api/http'
import { dialogs } from '../dialogs/dialogs'

const props = defineProps({
  mediaId: { type: String, required: true },
  // 父组件持有的详情对象（含 notes/tags），面板直接就地更新其字段
  detail: { type: Object, default: null },
  loading: { type: Boolean, default: false },
  showClose: { type: Boolean, default: true }
})
const emit = defineEmits(['close', 'deleted'])

const deleting = ref(false)

/* ---------- 展示计算 ---------- */
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

/* ---------- 备注 ---------- */
const notesText = ref('')
const notesState = ref('') // '' | saving | saved | error
let notesSavedTimer = 0

const notesDirty = computed(() => notesText.value !== (props.detail?.notes || ''))
const notesStateText = computed(() => {
  if (notesState.value === 'saving') return '保存中…'
  if (notesState.value === 'saved') return '已保存'
  if (notesState.value === 'error') return '保存失败'
  return ''
})

watch(
  () => [props.mediaId, props.detail?.notes],
  () => {
    // 仅在未编辑（或换媒体）时同步远端值，避免覆盖用户输入中的草稿
    if (!notesDirty.value || notesText.value === '') {
      notesText.value = props.detail?.notes || ''
    }
    if (notesState.value !== 'saving') notesState.value = ''
  },
  { immediate: true }
)

async function saveNotes() {
  if (!props.detail || notesState.value === 'saving' || !notesDirty.value) return
  notesState.value = 'saving'
  try {
    await http.patch(`/media/${props.mediaId}`, { notes: notesText.value })
    props.detail.notes = notesText.value
    notesState.value = 'saved'
    clearTimeout(notesSavedTimer)
    notesSavedTimer = setTimeout(() => {
      if (notesState.value === 'saved') notesState.value = ''
    }, 2000)
  } catch {
    notesState.value = 'error'
  }
}

/* ---------- 标签 ---------- */
const tags = computed(() => (Array.isArray(props.detail?.tags) ? props.detail.tags : []))
const people = computed(() => (Array.isArray(props.detail?.people) ? props.detail.people : []))

function tagName(t) { return t?.name ?? t }
function tagKind(t) { return t?.kind || 'user' }
function tagKey(t) { return t?.id != null ? 't' + t.id : 'tn' + tagName(t) }
function tagStyle(t) {
  if (!t?.color) return null
  return {
    backgroundColor: t.color + '22',
    color: t.color,
    borderColor: t.color + '55'
  }
}

const tagInput = ref('')
const suggestions = ref([])
const suggLoading = ref(false)
const suggestOpen = ref(false)
const tagError = ref('')
let suggTimer = 0
let suggSeq = 0

function onTagInput() {
  tagError.value = ''
  clearTimeout(suggTimer)
  const q = tagInput.value.trim()
  if (!q) {
    suggestions.value = []
    suggestOpen.value = false
    return
  }
  suggTimer = setTimeout(fetchSuggest, 300)
}

async function fetchSuggest() {
  const q = tagInput.value.trim()
  if (!q) return
  const seq = ++suggSeq
  suggLoading.value = true
  suggestOpen.value = true
  try {
    const { data } = await http.get('/tags', { params: { q } })
    if (seq !== suggSeq) return
    suggestions.value = Array.isArray(data) ? data : (Array.isArray(data?.tags) ? data.tags : [])
  } catch {
    if (seq === suggSeq) suggestions.value = []
  } finally {
    if (seq === suggSeq) suggLoading.value = false
  }
}

function onTagEnter() {
  const name = tagInput.value.trim()
  if (name) addTag(name)
}

function pickSuggest(s) {
  addTag(s.name)
}

function closeSuggest() {
  clearTimeout(suggTimer)
  suggestOpen.value = false
}

function onTagBlur() {
  // 延迟关闭，让 mousedown 先触发（选项点击用 mousedown.prevent）
  setTimeout(() => { suggestOpen.value = false }, 150)
}

async function refreshTags() {
  try {
    const { data } = await http.get(`/media/${props.mediaId}`)
    if (props.detail && Array.isArray(data.tags)) props.detail.tags = data.tags
  } catch {
    // 静默：保留本地状态
  }
}

async function addTag(name) {
  name = (name || '').trim()
  if (!name || !props.detail) return
  closeSuggest()
  tagInput.value = ''
  tagError.value = ''
  try {
    const res = await http.post(`/media/${props.mediaId}/tags`, { name })
    const d = res.data
    const created = d?.tag || (d && d.id != null && d.name ? d : null)
    if (d && Array.isArray(d.tags)) {
      props.detail.tags = d.tags
    } else if (created && created.id != null) {
      if (!tags.value.some((t) => t.id === created.id)) tags.value.push(created)
    } else {
      // 响应未携带标签实体：拉一次详情只更新 tags 字段（不影响备注草稿）
      await refreshTags()
    }
  } catch (e) {
    tagError.value = e.response?.data?.error?.message || '添加标签失败'
  }
}

async function removeTag(t) {
  if (!props.detail || t?.id == null) return
  try {
    await http.delete(`/media/${props.mediaId}/tags/${t.id}`)
    const i = tags.value.findIndex((x) => x.id === t.id)
    if (i >= 0) tags.value.splice(i, 1)
  } catch (e) {
    tagError.value = e.response?.data?.error?.message || '移除标签失败'
  }
}

/* ---------- AI 待确认（Phase 4） ---------- */
// 待确认 = 关联 origin='ai' 且 confirmed=false（detail.tags 现返回逐图态）
const pendingAI = computed(() => tags.value.filter((t) => t.origin === 'ai' && !t.confirmed))
const aiBusy = ref(false)

async function acceptAI(t) {
  if (!props.detail || t?.id == null) return
  aiBusy.value = true
  try {
    await http.post(`/media/${props.mediaId}/tags/confirm`, { tag_ids: [t.id] })
    t.confirmed = true
  } catch (e) {
    tagError.value = e.response?.data?.error?.message || '接受失败'
  } finally {
    aiBusy.value = false
  }
}

async function acceptAllAI() {
  if (!props.detail || !pendingAI.value.length) return
  aiBusy.value = true
  try {
    await http.post(`/media/${props.mediaId}/tags/confirm`, {})
    await refreshTags()
  } catch (e) {
    tagError.value = e.response?.data?.error?.message || '接受失败'
  } finally {
    aiBusy.value = false
  }
}

/* ---------- 评分 / 收藏 / 删除 ---------- */
async function rate(n) {
  if (!props.detail) return
  try {
    await http.post(`/media/${props.mediaId}/rate`, { rating: n })
    props.detail.rating = n
  } catch {
    // 静默失败，保持界面原状
  }
}

async function toggleFavorite() {
  if (!props.detail) return
  const next = !props.detail.favorite
  try {
    await http.post(`/media/${props.mediaId}/favorite`, { favorite: next })
    props.detail.favorite = next
  } catch {
    // 静默失败
  }
}

async function onDelete() {
  if (!props.detail || deleting.value) return
  const ok = await dialogs.confirm({ title: '删除媒体', text: `确定删除「${props.detail.filename}」吗？文件将移入回收站。`, confirmText: '删除', danger: true })
  if (!ok) return
  deleting.value = true
  try {
    await http.delete(`/media/${props.mediaId}`)
    emit('deleted', props.mediaId)
  } catch {
    await dialogs.alert('删除失败，请稍后重试')
  } finally {
    deleting.value = false
  }
}

/* ---------- 格式化 ---------- */
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
.info-panel {
  width: 300px;
  flex-shrink: 0;
  border-left: 1px solid var(--color-border);
  background-color: var(--color-surface);
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

/* 备注 */
.notes-state {
  font-weight: 400;
  font-size: 12px;
  color: var(--color-text-disabled);
}

.notes-state.saved {
  color: var(--color-success);
}

.notes-state.error {
  color: var(--color-danger);
}

.notes-input {
  width: 100%;
  resize: vertical;
  min-height: 64px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-family: inherit;
  font-size: var(--font-size-sm);
  padding: 8px 10px;
}

.notes-input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.notes-save {
  margin-top: 8px;
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 6px 14px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.notes-save:hover:not(:disabled) {
  background-color: var(--color-surface-hover);
}

.notes-save:disabled {
  color: var(--color-text-disabled);
  cursor: default;
}

/* 标签 */
.chip-list {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 8px;
}

.chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 10px;
  border-radius: 10px;
  font-size: var(--font-size-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  border: 1px solid transparent;
}

.chip-ai {
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}

.ai-mark {
  font-style: normal;
  font-size: 10px;
  font-weight: 700;
  padding: 0 3px;
  border-radius: 4px;
  background-color: var(--color-warning-text);
  color: var(--color-warning-bg);
}

.chip-person {
  background-color: var(--color-surface-hover);
  color: var(--color-text-secondary);
}

.chip-x {
  display: inline-flex;
  border: none;
  background: transparent;
  color: inherit;
  opacity: 0.6;
  padding: 2px;
  margin-right: -4px;
  border-radius: 50%;
}

.chip-x:hover {
  opacity: 1;
}

.tag-editor {
  position: relative;
}

.tag-input {
  width: 100%;
  height: 32px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 10px;
}

.tag-input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.suggest {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  right: 0;
  z-index: 20;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  box-shadow: var(--shadow-card);
  max-height: 200px;
  overflow-y: auto;
  padding: 4px;
}

.suggest-item {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  border: none;
  background: transparent;
  text-align: left;
  padding: 7px 10px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.suggest-item:hover {
  background-color: var(--color-surface-hover);
}

.suggest-item.create {
  color: var(--color-primary);
}

.suggest-name.ai {
  color: var(--color-warning-text);
}

.suggest-count {
  margin-left: auto;
  font-size: 11px;
  color: var(--color-text-disabled);
}

.suggest-tip {
  padding: 8px 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-disabled);
}

.tag-error {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-danger);
}

.accept-all {
  margin-left: auto;
  border: 1px solid var(--color-warning-text);
  background: transparent;
  color: var(--color-warning-text);
  border-radius: var(--radius-sm);
  padding: 2px 8px;
  font-size: 12px;
  cursor: pointer;
}

.accept-all:disabled {
  opacity: 0.5;
  cursor: default;
}

.ai-hint {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-text-disabled);
}

/* 评分 */
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

/* 操作 */
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
</style>
