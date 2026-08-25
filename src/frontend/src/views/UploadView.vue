<template>
  <div class="upload-view">
    <h1 class="page-title">上传</h1>

    <div
      class="dropzone"
      :class="{ 'dropzone--over': dragOver }"
      @dragenter.prevent="dragOver = true"
      @dragover.prevent="dragOver = true"
      @dragleave.prevent="onDragLeave"
      @drop.prevent="onDrop"
      @click="fileInput.click()"
    >
      <span class="drop-icon" v-html="uploadIcon"></span>
      <p class="drop-title">拖拽文件到此处，或点击选择</p>
      <p class="drop-desc">支持照片与视频，可一次选择多个文件；大于 8MB 的文件自动启用分块续传</p>
      <input
        ref="fileInput"
        type="file"
        multiple
        accept="image/*,video/*"
        class="hidden-input"
        @change="onPick"
      />
    </div>

    <div v-if="allDone" class="done-banner">
      <span>全部上传完成</span>
      <router-link to="/timeline" class="done-link">查看时间轴</router-link>
      <button class="clear-btn" @click="onClearFinished">清除记录</button>
    </div>

    <div v-if="queue.items.length > 0" class="queue-section">
      <div class="queue-header">
        <h2 class="section-title">上传队列</h2>
        <span class="muted">{{ doneCount }}/{{ queue.items.length }} 完成</span>
      </div>
      <div class="queue-list">
        <UploadItem v-for="item in queue.items" :key="item.id" :item="item" @retry="retryItem" />
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import UploadItem from '../components/upload/UploadItem.vue'
import { addFiles, clearFinished, queue, retryItem } from '../components/upload/uploadManager'

const uploadIcon =
  '<svg viewBox="0 0 48 48" width="44" height="44" fill="none"><path d="M24 32V14m0 0l-8 8m8-8l8 8" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/><path d="M8 34v4a4 4 0 0 0 4 4h24a4 4 0 0 0 4-4v-4" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"/></svg>'

const fileInput = ref(null)
const dragOver = ref(false)

const doneCount = computed(() => queue.items.filter((it) => it.status === 'done').length)
const allDone = computed(
  () =>
    queue.items.length > 0 &&
    queue.items.every((it) => it.status === 'done')
)

function onDragLeave(e) {
  if (!e.currentTarget.contains(e.relatedTarget)) {
    dragOver.value = false
  }
}

function onDrop(e) {
  dragOver.value = false
  if (e.dataTransfer?.files?.length) {
    addFiles(e.dataTransfer.files)
  }
}

function onPick(e) {
  if (e.target.files?.length) {
    addFiles(e.target.files)
  }
  e.target.value = ''
}

function onClearFinished() {
  clearFinished()
}
</script>

<style scoped>
.upload-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  max-width: 860px;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.dropzone {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 56px 24px;
  background-color: var(--color-surface);
  border: 2px dashed var(--color-border);
  border-radius: var(--radius-lg);
  cursor: pointer;
  text-align: center;
  transition: border-color 0.15s ease, background-color 0.15s ease;
}

.dropzone:hover {
  border-color: var(--color-primary);
}

.dropzone--over {
  border-color: var(--color-primary);
  background-color: var(--color-primary-active-bg);
}

.drop-icon {
  color: var(--color-primary);
  display: inline-flex;
}

.drop-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
  color: var(--color-text-primary);
}

.drop-desc {
  margin: 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.hidden-input {
  display: none;
}

.done-banner {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 12px 16px;
  border-radius: var(--radius-md);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-size: var(--font-size-md);
}

.done-link {
  color: var(--color-primary);
  font-weight: 600;
  text-decoration: underline;
}

.clear-btn {
  margin-left: auto;
  border: none;
  background: transparent;
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
  cursor: pointer;
  font-family: var(--font-family);
}

.clear-btn:hover {
  color: var(--color-text-primary);
}

.queue-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.queue-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
}

.section-title {
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 600;
}

.muted {
  color: var(--color-text-secondary);
}

.queue-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
</style>
