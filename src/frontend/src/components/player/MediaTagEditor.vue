<template>
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
      <TagSuggest
        :open="suggestOpen"
        :loading="suggLoading"
        :suggestions="suggestions"
        :input-text="tagInput"
        @pick="pickSuggest"
        @create="addTag"
      />
    </div>
    <p v-if="tagError" class="tag-error">{{ tagError }}</p>
  </section>

  <!-- AI 待确认（Phase 4）：拆为独立组件，自持接受/移除逻辑 -->
  <MediaAIConfirm :media-id="mediaId" :detail="detail" />
</template>

<script setup>
// MediaInfoPanel 拆解（Job000082）：标签编辑 section。
// 就地更新 props.detail.tags（与拆解前一致的既有模式）；错误/忙碌状态本子组件自持。
// 自动补全下拉拆 TagSuggest，AI 待确认拆 MediaAIConfirm。
import { computed, ref } from 'vue'
import http from '../../api/http'
import TagSuggest from './TagSuggest.vue'
import MediaAIConfirm from './MediaAIConfirm.vue'

const props = defineProps({
  mediaId: { type: String, required: true },
  detail: { type: Object, required: true }
})

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

.tag-error {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-danger);
}
</style>
