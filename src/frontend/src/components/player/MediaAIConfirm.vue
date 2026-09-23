<template>
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
</template>

<script setup>
// MediaInfoPanel 拆解（Job000082）：AI 待确认 section 独立组件。
// 待确认 = 关联 origin='ai' 且 confirmed=false（detail.tags 现返回逐图态）；就地改 detail.tags，与拆解前一致。
import { computed, ref } from 'vue'
import http from '../../api/http'

const props = defineProps({
  mediaId: { type: String, required: true },
  detail: { type: Object, required: true }
})

const tags = computed(() => (Array.isArray(props.detail?.tags) ? props.detail.tags : []))
const pendingAI = computed(() => tags.value.filter((t) => t.origin === 'ai' && !t.confirmed))
const aiBusy = ref(false)
const tagError = ref('')

async function refreshTags() {
  try {
    const { data } = await http.get(`/media/${props.mediaId}`)
    if (props.detail && Array.isArray(data.tags)) props.detail.tags = data.tags
  } catch {
    // 静默：保留本地状态
  }
}

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

.tag-error {
  margin: 6px 0 0;
  font-size: 12px;
  color: var(--color-danger);
}
</style>
