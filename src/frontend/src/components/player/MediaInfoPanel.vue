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
      <MediaFacts :detail="detail" />

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

      <MediaTagEditor :media-id="mediaId" :detail="detail" />

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
        <!-- Job000135：分享按钮——emit 给 PlayerView 打开创建分享对话框（Job000134 扩展管理页） -->
        <button class="action-btn" data-testid="info-share" @click="emit('share')">
          <svg viewBox="0 0 24 24" width="16" height="16" fill="none">
            <circle cx="6" cy="12" r="2.4" stroke="currentColor" stroke-width="1.6" />
            <circle cx="17.5" cy="5.5" r="2.4" stroke="currentColor" stroke-width="1.6" />
            <circle cx="17.5" cy="18.5" r="2.4" stroke="currentColor" stroke-width="1.6" />
            <path d="M8.2 10.9l7.1-4M8.2 13.1l7.1 4" stroke="currentColor" stroke-width="1.6" />
          </svg>
          分享
        </button>
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
// Job000082 拆解：纯展示（基本信息/EXIF/视频）→ MediaFacts；标签/AI 待确认 → MediaTagEditor。
// 本组件保留：头部、备注（PATCH notes 草稿态）、评分、收藏/删除。对外接口不变。
import { computed, ref, watch } from 'vue'
import http from '../../api/http'
import { dialogs } from '../dialogs/dialogs'
import MediaFacts from './MediaFacts.vue'
import MediaTagEditor from './MediaTagEditor.vue'

const props = defineProps({
  mediaId: { type: String, required: true },
  // 父组件持有的详情对象（含 notes/tags），面板直接就地更新其字段
  detail: { type: Object, default: null },
  loading: { type: Boolean, default: false },
  showClose: { type: Boolean, default: true }
})
const emit = defineEmits(['share', 'close', 'deleted'])

const deleting = ref(false)

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
