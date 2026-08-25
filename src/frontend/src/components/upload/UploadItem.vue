<template>
  <div class="upload-item" :class="'upload-item--' + item.status">
    <span class="file-icon" v-html="fileIcon"></span>

    <div class="file-main">
      <div class="file-top">
        <span class="file-name" :title="item.name">{{ item.name }}</span>
        <span class="file-status" :class="statusClass">{{ statusText }}</span>
      </div>

      <div class="progress-track">
        <div class="progress-bar" :class="barClass" :style="{ width: percent + '%' }"></div>
      </div>

      <div class="file-bottom">
        <span class="muted">{{ formatBytes(item.uploaded) }} / {{ formatBytes(item.size) }}</span>
        <span v-if="item.status === 'uploading' && speedText" class="muted">{{ speedText }}</span>
        <span v-if="item.chunked" class="tag">分块续传</span>
        <span v-if="item.status === 'error'" class="error-text">{{ item.error }}</span>
      </div>
    </div>

    <button v-if="item.status === 'error'" class="retry-btn" @click="$emit('retry', item)">重试</button>
    <span v-else-if="item.status === 'done'" class="done-icon" v-html="doneIcon"></span>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { formatBytes, formatSpeed } from './uploadManager'

const props = defineProps({
  item: { type: Object, required: true }
})
defineEmits(['retry'])

const doneIcon =
  '<svg viewBox="0 0 20 20" width="20" height="20" fill="none"><circle cx="10" cy="10" r="8.5" stroke="currentColor" stroke-width="1.6"/><path d="M6.5 10.2l2.4 2.4 4.6-5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>'

const fileIcon = computed(() => {
  const isVideo = /\.(mp4|mov|avi|mkv|webm|m4v|3gp|insv)$/i.test(props.item.name)
  return isVideo
    ? '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><rect x="3" y="5" width="18" height="14" rx="2" stroke="currentColor" stroke-width="1.5"/><path d="M10.5 9.5l5 2.5-5 2.5v-5z" fill="currentColor"/></svg>'
    : '<svg viewBox="0 0 24 24" width="22" height="22" fill="none"><rect x="3" y="4" width="18" height="16" rx="2" stroke="currentColor" stroke-width="1.5"/><circle cx="9" cy="10" r="2" stroke="currentColor" stroke-width="1.5"/><path d="M4 18l5-5 3.5 3.5L16 13l4 4" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round"/></svg>'
})

const percent = computed(() => {
  if (!props.item.size) return 0
  const loaded = Math.min(props.item.uploaded + (props.item.chunkLoaded || 0), props.item.size)
  return Math.round((loaded / props.item.size) * 100)
})

const speedText = computed(() => formatSpeed(props.item.speed))

const statusText = computed(() => {
  switch (props.item.status) {
    case 'waiting':
      return '等待'
    case 'uploading':
      return percent.value + '%'
    case 'done':
      return '完成'
    case 'error':
      return '失败'
    default:
      return ''
  }
})

const statusClass = computed(() => ({
  'status--done': props.item.status === 'done',
  'status--error': props.item.status === 'error',
  'status--uploading': props.item.status === 'uploading'
}))

const barClass = computed(() => ({
  'bar--done': props.item.status === 'done',
  'bar--error': props.item.status === 'error'
}))
</script>

<style scoped>
.upload-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
}

.upload-item--error {
  border-color: var(--color-danger);
}

.file-icon {
  color: var(--color-text-disabled);
  display: inline-flex;
  flex-shrink: 0;
}

.file-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.file-top {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.file-name {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.file-status {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  flex-shrink: 0;
}

.status--uploading {
  color: var(--color-primary);
}

.status--done {
  color: var(--color-success);
}

.status--error {
  color: var(--color-danger);
}

.progress-track {
  height: 4px;
  border-radius: 2px;
  background-color: var(--color-bg);
  overflow: hidden;
}

.progress-bar {
  height: 100%;
  background-color: var(--color-primary);
  border-radius: 2px;
  transition: width 0.2s ease;
}

.bar--done {
  background-color: var(--color-success);
}

.bar--error {
  background-color: var(--color-danger);
}

.file-bottom {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
}

.muted {
  color: var(--color-text-secondary);
}

.tag {
  padding: 0 6px;
  border-radius: 4px;
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
}

.error-text {
  color: var(--color-danger);
}

.retry-btn {
  padding: 6px 14px;
  border: 1px solid var(--color-danger);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-danger);
  font-size: var(--font-size-sm);
  cursor: pointer;
  font-family: var(--font-family);
  flex-shrink: 0;
}

.retry-btn:hover {
  background-color: var(--color-surface-hover);
}

.done-icon {
  color: var(--color-success);
  display: inline-flex;
  flex-shrink: 0;
}
</style>
