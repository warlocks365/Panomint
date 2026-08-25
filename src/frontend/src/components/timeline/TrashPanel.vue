<template>
  <teleport to="body">
    <div v-if="modelValue" class="trash-mask" @click.self="close">
      <aside class="trash-panel">
        <header class="trash-header">
          <h3 class="trash-title">回收站</h3>
          <span v-if="total" class="trash-count">{{ total }} 项</span>
          <button class="close-btn" title="关闭" @click="close">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none">
              <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
            </svg>
          </button>
        </header>

        <div v-if="loading" class="trash-tip">加载中…</div>
        <div v-else-if="error" class="trash-tip error">
          {{ error }}
          <button class="retry-btn" @click="load">重试</button>
        </div>
        <div v-else-if="!items.length" class="trash-tip empty">
          <svg viewBox="0 0 24 24" width="36" height="36" fill="none">
            <path d="M4 7h16M9 7V5a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2M6.5 7l1 13h9l1-13" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <p>回收站为空</p>
        </div>

        <ul v-else class="trash-list">
          <li v-for="item in items" :key="item.id" class="trash-item">
            <div class="trash-thumb">
              <TrashThumb :item="item" />
            </div>
            <div class="trash-meta">
              <p class="trash-name" :title="item.filename">{{ item.filename }}</p>
              <p class="trash-date">{{ formatTime(item.taken_at) }}</p>
            </div>
            <button
              class="restore-btn"
              :disabled="restoringId === item.id"
              @click="restore(item)"
            >
              {{ restoringId === item.id ? '恢复中…' : '恢复' }}
            </button>
          </li>
        </ul>
      </aside>
    </div>
  </teleport>
</template>

<script setup>
import { ref, watch } from 'vue'
import http from '../../api/http'
import TrashThumb from './TrashThumb.vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false }
})
const emit = defineEmits(['update:modelValue', 'restored'])

const items = ref([])
const total = ref(0)
const loading = ref(false)
const error = ref('')
const restoringId = ref('')

watch(
  () => props.modelValue,
  (open) => {
    if (open) load()
  }
)

async function load() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await http.get('/media/trash')
    items.value = Array.isArray(data.items) ? data.items : []
    total.value = data.total ?? items.value.length
  } catch (e) {
    error.value = e.response
      ? `加载失败：HTTP ${e.response.status}`
      : '加载失败：网络不可达'
  } finally {
    loading.value = false
  }
}

async function restore(item) {
  if (restoringId.value) return
  restoringId.value = item.id
  try {
    await http.post(`/media/trash/${item.id}/restore`)
    items.value = items.value.filter((m) => m.id !== item.id)
    total.value = Math.max(0, total.value - 1)
    emit('restored', item)
  } catch (e) {
    window.alert('恢复失败，请稍后重试')
  } finally {
    restoringId.value = ''
  }
}

function close() {
  emit('update:modelValue', false)
}

function formatTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t
  const p = (x) => String(x).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}
</script>

<style scoped>
.trash-mask {
  position: fixed;
  inset: 0;
  background-color: rgba(0, 0, 0, 0.35);
  z-index: 90;
  display: flex;
  justify-content: flex-end;
}

.trash-panel {
  width: 380px;
  max-width: 90vw;
  height: 100%;
  background-color: var(--color-surface);
  border-left: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
}

.trash-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 16px;
  border-bottom: 1px solid var(--color-border);
}

.trash-title {
  margin: 0;
  font-size: var(--font-size-lg);
  flex: 1;
}

.trash-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
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

.trash-tip {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: var(--color-text-disabled);
  font-size: var(--font-size-md);
  padding: 24px;
}

.trash-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  border: 1px solid var(--color-border);
  background-color: var(--color-surface);
  border-radius: var(--radius-sm);
  padding: 5px 14px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
}

.trash-list {
  list-style: none;
  margin: 0;
  padding: 8px 12px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.trash-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: var(--radius-md);
}

.trash-item:hover {
  background-color: var(--color-surface-hover);
}

.trash-thumb {
  width: 56px;
  height: 56px;
  flex-shrink: 0;
}

.trash-meta {
  flex: 1;
  min-width: 0;
}

.trash-name {
  margin: 0 0 2px;
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.trash-date {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.restore-btn {
  flex-shrink: 0;
  border: 1px solid var(--color-primary);
  background-color: var(--color-surface);
  color: var(--color-primary);
  border-radius: var(--radius-sm);
  padding: 5px 14px;
  font-size: var(--font-size-sm);
}

.restore-btn:hover:not(:disabled) {
  background-color: var(--color-primary-active-bg);
}

.restore-btn:disabled {
  color: var(--color-text-disabled);
  border-color: var(--color-border);
}
</style>
