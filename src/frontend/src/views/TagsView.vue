<template>
  <div class="tags-page">
    <header class="page-toolbar">
      <h2 class="page-title">标签</h2>
      <input
        v-model.trim="filter"
        class="filter-input"
        type="text"
        placeholder="搜索标签"
        @keydown.enter="loadTags"
      />
      <div class="spacer"></div>
      <button class="btn" :disabled="aiBusy" @click="triggerAI">
        {{ aiBusy ? 'AI 打标中…' : 'AI 打标' }}
      </button>
      <button class="btn primary" @click="onCreate">新建标签</button>
    </header>

    <p v-if="loading" class="page-tip">加载中…</p>
    <p v-else-if="loadError" class="page-tip error">
      {{ loadError }}
      <button class="retry-btn" @click="loadTags">重试</button>
    </p>

    <template v-else>
      <div v-if="!tags.length" class="empty">
        <p class="empty-title">还没有标签</p>
        <p class="empty-desc">可手动新建，或点「AI 打标」让 CLIP 自动建议（需在媒体详情里确认）</p>
      </div>

      <div v-else class="cloud">
        <button
          v-for="t in tags"
          :key="t.id"
          class="tag-chip"
          :class="{ active: selected && selected.id === t.id, ai: t.kind === 'ai' && !t.confirmed }"
          :style="chipStyle(t)"
          :title="`${t.name} · ${t.usage_count} 项${t.kind === 'ai' && !t.confirmed ? ' · 待确认' : ''}`"
          @click="selectTag(t)"
        >
          {{ t.name }}
          <span class="chip-count">{{ t.usage_count }}</span>
          <i v-if="t.kind === 'ai' && !t.confirmed" class="chip-pending" title="AI 待确认">待</i>
        </button>
      </div>

      <TagDetailPanel
        v-if="selected"
        :key="selected.id"
        :tag="selected"
        :tags="tags"
        @changed="loadTags"
        @cleared="onSelectionCleared"
      />
    </template>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import http from '../api/http'
import TagDetailPanel from './tags/TagDetailPanel.vue'
import { dialogs } from '../components/dialogs/dialogs'


const tags = ref([])
const loading = ref(false)
const loadError = ref('')
const filter = ref('')

const selected = ref(null)

function chipStyle(t) {
  if (t.color) {
    return { backgroundColor: t.color + '22', color: t.color, borderColor: t.color + '55' }
  }
  return null
}

async function onCreate() {
  const name = await dialogs.prompt({
    title: '新建标签',
    label: '标签名',
    validate: (v) => (v.trim() ? '' : '标签名不能为空')
  })
  if (name === null) return
  try {
    await http.post('/tags', { name: name.trim() })
    await loadTags()
  } catch (e) {
    await dialogs.alert(errMsg(e, '创建失败'))
  }
}





async function triggerAI() {
  if (aiBusy.value) return
  aiBusy.value = true
  try {
    const { data } = await http.post('/ai/tags', { scope: 'all' })
    await dialogs.alert(`AI 打标完成：处理 ${data?.processed ?? 0} 项，写入 ${data?.tagged ?? 0} 个标签（AI 标签需在媒体详情确认）`)
    await loadTags()
  } catch (e) {
    await dialogs.alert(errMsg(e, 'AI 打标失败（可能未启用 CLIP）'))
  } finally {
    aiBusy.value = false
  }
}

function onSelectionCleared() {
  selected.value = null
}

onMounted(loadTags)
</script>

<style scoped>
.tags-page {
  padding: 20px 24px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
}

.page-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.spacer {
  flex: 1;
}

.filter-input {
  height: 32px;
  width: 200px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  padding: 0 10px;
}

.filter-input:focus {
  outline: none;
  border-color: var(--color-primary);
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn:hover {
  background-color: var(--color-surface-hover);
}

.btn.sm {
  padding: 4px 10px;
  font-size: var(--font-size-sm);
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.danger {
  border-color: var(--color-danger);
  color: var(--color-danger);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
}

.empty {
  padding: 64px 0;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  color: var(--color-text-disabled);
}

.empty-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.cloud {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 22px;
}

.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border-radius: 14px;
  border: 1px solid transparent;
  font-size: var(--font-size-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  cursor: pointer;
}

.tag-chip.ai {
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}

.tag-chip.active {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}

.chip-count {
  font-size: 11px;
  opacity: 0.75;
}

.chip-pending {
  font-style: normal;
  font-size: 10px;
  font-weight: 700;
  padding: 0 4px;
  border-radius: 4px;
  background-color: var(--color-warning-text);
  color: var(--color-warning-bg);
}

</style>
