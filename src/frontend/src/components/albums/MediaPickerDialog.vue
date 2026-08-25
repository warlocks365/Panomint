<template>
  <div class="dlg-mask" @click.self="$emit('cancel')">
    <div class="picker" role="dialog" aria-label="添加媒体">
      <div class="picker-head">
        <h3 class="picker-title">添加媒体</h3>
        <span class="picker-count">已选 {{ selected.size }} 项</span>
      </div>

      <div class="picker-body">
        <p v-if="loading && !items.length" class="picker-tip">加载中…</p>
        <p v-else-if="error" class="picker-tip error">
          {{ error }}
          <button class="retry-btn" @click="reset">重试</button>
        </p>
        <p v-else-if="!items.length" class="picker-tip">媒体库暂无内容，请先在时间线上传照片或视频</p>
        <template v-else>
          <div class="picker-grid">
            <MediaThumb
              v-for="m in items"
              :key="m.id"
              :item="m"
              size="sm"
              selectable
              :selected="selected.has(m.id)"
              @click="toggle"
            />
          </div>
          <p v-if="loading" class="picker-tip">加载中…</p>
          <button v-else-if="nextCursor" class="btn load-more" @click="loadMore">加载更多</button>
        </template>
      </div>

      <p v-if="submitError" class="picker-error">{{ submitError }}</p>

      <div class="picker-actions">
        <button class="btn" @click="$emit('cancel')">取消</button>
        <button class="btn primary" :disabled="!selected.size || submitting" @click="submit">
          {{ submitting ? '添加中…' : `添加 ${selected.size} 项` }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import http from '../../api/http'
import MediaThumb from './MediaThumb.vue'
import { addAlbumItems, errMsg } from './albumApi'

const props = defineProps({
  albumId: { type: [String, Number], required: true },
  // 已在相册中的媒体 id，选择时跳过
  existingIds: { type: Array, default: () => [] }
})
const emit = defineEmits(['cancel', 'added'])

const PAGE_SIZE = 60

const items = ref([])
const selected = reactive(new Set())
const loading = ref(false)
const error = ref('')
const submitting = ref(false)
const submitError = ref('')
let nextCursor = ref('')
let finished = false

async function loadMore() {
  if (loading.value || finished) return
  loading.value = true
  error.value = ''
  try {
    const params = { limit: PAGE_SIZE }
    if (nextCursor.value) params.cursor = nextCursor.value
    const { data } = await http.get('/media', { params })
    const list = Array.isArray(data.items) ? data.items : []
    const exist = new Set(props.existingIds)
    for (const m of list) {
      if (!exist.has(m.id) && !items.value.some((x) => x.id === m.id)) {
        items.value.push(m)
      }
    }
    nextCursor.value = data.next_cursor || ''
    if (!nextCursor.value || !list.length) finished = true
  } catch (e) {
    error.value = errMsg(e, '媒体列表加载失败')
  } finally {
    loading.value = false
  }
}

function reset() {
  items.value = []
  nextCursor.value = ''
  finished = false
  loadMore()
}

function toggle(item) {
  if (selected.has(item.id)) selected.delete(item.id)
  else selected.add(item.id)
}

async function submit() {
  if (!selected.size || submitting.value) return
  submitting.value = true
  submitError.value = ''
  try {
    const res = await addAlbumItems(props.albumId, Array.from(selected))
    emit('added', res)
  } catch (e) {
    submitError.value = errMsg(e, '添加失败')
  } finally {
    submitting.value = false
  }
}

onMounted(loadMore)
</script>

<style scoped>
.dlg-mask {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.4);
}

.picker {
  width: 720px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
}

.picker-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  margin-bottom: 12px;
}

.picker-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.picker-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.picker-body {
  flex: 1;
  min-height: 200px;
  overflow-y: auto;
  margin-bottom: 12px;
}

.picker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
  gap: 8px;
}

.picker-tip {
  padding: 32px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.picker-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--font-size-md);
}

.load-more {
  display: block;
  margin: 12px auto 0;
}

.picker-error {
  margin-bottom: 10px;
  font-size: var(--font-size-sm);
  color: var(--color-danger);
}

.picker-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.btn {
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

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}

.btn.primary:hover {
  background-color: var(--color-primary-hover);
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
