<template>
  <div class="upload-view">
    <div class="ph-line">
      <div class="ph-heading">
        <span class="ph-eyebrow">09</span>
        <h1 class="page-title">上传</h1>
      </div>
      <div v-if="targetLabel" class="target-banner" data-testid="upload-target">
        将上传到：<span class="t-mono">{{ targetLabel }}</span>
        <router-link v-if="backTo" :to="backTo" class="done-link">返回</router-link>
      </div>
    </div>

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
import { computed, nextTick, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import gsap from 'gsap'
import UploadItem from '../components/upload/UploadItem.vue'
import { addFiles, clearFinished, queue, retryItem } from '../components/upload/uploadManager'

// Job000066 三入口复用：?folder=<路径>（文件夹页）或 ?album=<id>&albumName=<名>（相册页）
const route = useRoute()
const targetFolder = typeof route.query.folder === 'string' ? route.query.folder : ''
const targetAlbum = typeof route.query.album === 'string' ? route.query.album : ''
const targetAlbumName = typeof route.query.albumName === 'string' ? route.query.albumName : ''
const targetLabel = targetAlbum
  ? '相册「' + (targetAlbumName || targetAlbum) + '」'
  : targetFolder
    ? '目录「' + (targetFolder || '全部') + '」'
    : '个人空间根目录'
const backTo = targetAlbum
  ? '/albums/' + targetAlbum
  : targetFolder
    ? '/folders'
    : ''
const uploadOpts = { folderPath: targetFolder, albumId: targetAlbum }

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
    addFiles(e.dataTransfer.files, uploadOpts)
  }
}

function onPick(e) {
  if (e.target.files?.length) {
    addFiles(e.target.files, uploadOpts)
  }
  e.target.value = ''
}

// DESIGN.md §8 queue-in：队列项首屏一次性浮现（clearProps 还原；reduced-motion 跳过）
let queueDone = false
watch(
  () => queue.items.length,
  async (n) => {
    if (n > 0 && !queueDone) {
      queueDone = true
      await nextTick()
      requestAnimationFrame(() => {
        const mm = gsap.matchMedia()
        mm.add('(prefers-reduced-motion: no-preference)', () => {
          gsap.from('.queue-list .upload-item', {
            y: 16,
            opacity: 0,
            duration: 0.45,
            ease: 'power2.out',
            stagger: 0.05,
            clearProps: 'transform,opacity'
          })
        })
      })
    }
  }
)

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

/* 页头规范（DESIGN.md §5）+ 内联目标徽章（深化稿 VIEW.09） */
.ph-line {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

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
  margin: 0;
  font-size: var(--font-size-lg);
  font-weight: 500;
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}

.target-banner {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 14px;
  border-radius: 10px;
  background-color: rgba(74, 90, 106, 0.08);
  color: var(--color-text-secondary);
  font-size: var(--font-size-sm);
}

.t-mono {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-primary);
}

/* dropzone 深化：米白面 + 雾蓝虚线 + 磨砂圆图标 + hover 雾蓝浅底（软曲线） */
.dropzone {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  padding: 48px 24px;
  background-color: var(--color-surface);
  border: 1.5px dashed rgba(74, 90, 106, 0.4);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  cursor: pointer;
  text-align: center;
  transition: border-color 0.5s cubic-bezier(0.32, 0.72, 0, 1), background-color 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.dropzone:hover {
  border-color: var(--color-primary);
  background-color: rgba(74, 90, 106, 0.04);
}

.dropzone--over {
  border-color: var(--color-primary);
  background-color: rgba(74, 90, 106, 0.08);
}

.drop-icon {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  background-color: rgba(74, 90, 106, 0.1);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}

.drop-title {
  margin: 0;
  font-size: var(--font-size-md);
  font-weight: 500;
  color: var(--color-text-primary);
}

.drop-desc {
  margin: 0;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
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
