<template>
  <div class="tags-page">
    <header class="page-toolbar">
      <div class="ph-heading">
        <span class="ph-eyebrow">05</span>
        <h2 class="page-title">标签</h2>
      </div>
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
          <i v-if="t.kind === 'ai' && !t.confirmed" class="ai-dot" aria-hidden="true"></i>
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
import { nextTick, onMounted, ref, watch } from 'vue'
import gsap from 'gsap'
import http from '../api/http'
import TagDetailPanel from './tags/TagDetailPanel.vue'
import { dialogs } from '../components/dialogs/dialogs'


const tags = ref([])
const loading = ref(false)
const loadError = ref('')
const filter = ref('')

const selected = ref(null)
const aiBusy = ref(false)

function errMsg(e, fallback) {
  return e.response?.data?.error?.message || e?.message || fallback
}

async function loadTags() {
  loading.value = true
  loadError.value = ''
  try {
    const { data } = await http.get('/tags', { params: filter.value ? { q: filter.value } : {} })
    tags.value = Array.isArray(data) ? data : Array.isArray(data?.tags) ? data.tags : []
    if (selected.value) {
      const cur = tags.value.find((t) => t.id === selected.value.id)
      selected.value = cur || null
    }
  } catch (e) {
    loadError.value = errMsg(e, '标签加载失败')
  } finally {
    loading.value = false
  }
}

function selectTag(t) {
  selected.value = t
}

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

// DESIGN.md §8 cloud-in：标签云 chip 首屏一次性浮现（scale 0.92→1 + 透明度；clearProps 还原）
let cloudDone = false
watch(
  () => loading.value,
  async (isLoading) => {
    if (!isLoading && !cloudDone) {
      cloudDone = true
      await nextTick()
      requestAnimationFrame(() => {
        const mm = gsap.matchMedia()
        mm.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.from('.cloud .tag-chip', {
            scale: 0.92,
            opacity: 0,
            duration: 0.45,
            ease: 'power2.out',
            stagger: 0.03,
            clearProps: 'transform,opacity'
          })
        })
      })
    }
  }
)
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

/* 页头规范（DESIGN.md §5） */
.ph-heading {
  display: flex;
  align-items: baseline;
  gap: 10px;
}

.ph-eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--color-text-disabled);
}

.page-title {
  font-size: var(--font-size-lg);
  letter-spacing: 0.12em;
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
  align-items: center;
  gap: 12px 14px;
  padding: 20px 22px;
  margin-bottom: 22px;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
}

/* 磨砂胶囊 chip（DESIGN.md 深化稿 VIEW.05）；自定义色标签的内联底色优先保留其色彩语义 */
.tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 999px;
  border: 1px solid transparent;
  font-size: var(--font-size-sm);
  background-color: rgba(244, 241, 237, 0.82);
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.08);
  color: var(--color-text-primary);
  cursor: pointer;
  transition: box-shadow 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.tag-chip:hover {
  box-shadow: var(--shadow-lift);
}

.tag-chip.ai {
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}

.tag-chip.active {
  border-color: var(--color-primary);
  background-color: var(--color-primary);
  color: #eef1f4;
  box-shadow: var(--shadow-card);
}

.ai-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: var(--stat-pano-photo);
  flex-shrink: 0;
}

.chip-count {
  font-family: var(--font-mono);
  font-size: 10px;
  opacity: 0.75;
}

.chip-pending {
  font-style: normal;
  font-size: 10px;
  font-weight: 700;
  padding: 0 4px;
  border-radius: 4px;
  border: 1px solid var(--color-warning-text);
  color: var(--color-warning-text);
  background-color: transparent;
}

</style>
