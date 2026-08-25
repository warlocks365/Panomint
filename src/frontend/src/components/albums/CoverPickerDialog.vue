<template>
  <div class="dlg-mask" @click.self="$emit('cancel')">
    <div class="picker" role="dialog" aria-label="设置封面">
      <div class="picker-head">
        <h3 class="picker-title">设置封面</h3>
        <span class="picker-count">从相册内容中选择一张作为封面</span>
      </div>

      <div class="picker-body">
        <p v-if="!items.length" class="picker-tip">相册内暂无媒体，无法设置封面</p>
        <div v-else class="picker-grid">
          <MediaThumb
            v-for="m in items"
            :key="m.id"
            :item="m"
            size="sm"
            selectable
            :selected="selectedId === m.id"
            @click="selectedId = $event.id"
          />
        </div>
      </div>

      <p v-if="error" class="picker-error">{{ error }}</p>

      <div class="picker-actions">
        <button class="btn" @click="$emit('cancel')">取消</button>
        <button class="btn primary" :disabled="!selectedId || submitting" @click="submit">
          {{ submitting ? '保存中…' : '设为封面' }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import MediaThumb from './MediaThumb.vue'
import { errMsg, updateAlbum } from './albumApi'

const props = defineProps({
  albumId: { type: [String, Number], required: true },
  items: { type: Array, default: () => [] },
  currentCoverId: { type: [String, Number], default: '' }
})
const emit = defineEmits(['cancel', 'saved'])

const selectedId = ref(props.currentCoverId || '')
const submitting = ref(false)
const error = ref('')

async function submit() {
  if (!selectedId.value || submitting.value) return
  submitting.value = true
  error.value = ''
  try {
    const saved = await updateAlbum(props.albumId, { cover_media_id: selectedId.value })
    emit('saved', saved)
  } catch (e) {
    error.value = errMsg(e, '封面设置失败')
  } finally {
    submitting.value = false
  }
}
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
  width: 640px;
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
  gap: 12px;
  margin-bottom: 12px;
}

.picker-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  flex-shrink: 0;
}

.picker-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.picker-body {
  flex: 1;
  min-height: 160px;
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
