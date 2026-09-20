<template>
  <!-- 根用 span 而非 button：父级 media-cell 本身是 <button>，按钮嵌套按钮属非法 HTML -->
  <span
    v-if="allowed"
    class="dl-btn"
    role="button"
    tabindex="0"
    :title="`下载 ${filename || '原文件'}`"
    :aria-label="`下载 ${filename || '原文件'}`"
    :data-testid="`share-download-${id}`"
    @click.stop="onDownload"
    @keydown.enter.stop.prevent="onDownload"
  >
    <svg v-if="!busy" viewBox="0 0 24 24" width="14" height="14" fill="none">
      <path d="M12 4v11m0 0l-4.5-4.5M12 15l4.5-4.5M5 19.5h14" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
    <span v-else class="dl-spinner" aria-hidden="true"></span>
  </span>
</template>

<script setup>
import { ref } from 'vue'
import { downloadPublicMedia } from './publicApi'

// 公开分享页的逐媒体下载按钮（Job000053）。仅 share.allow_download=true 时由父级渲染。
// 密码走 X-Share-Password 头（不进 URL/日志）；fetch→blob→ObjectURL 触发浏览器保存。
// 已知限制：整文件入内存 —— 相册分享以照片为主可接受；超大视频下载待流式方案（Future）。
const props = defineProps({
  token: { type: String, required: true },
  id: { type: String, required: true },
  filename: { type: String, default: '' },
  password: { type: String, default: '' },
  allowed: { type: Boolean, default: false }
})

const busy = ref(false)

async function onDownload() {
  if (busy.value) return
  busy.value = true
  try {
    await downloadPublicMedia(props.token, props.id, props.password, props.filename)
  } catch {
    /* 失败静默：不打扰浏览；配额/审计在服务端已按实际响应记账 */
  } finally {
    busy.value = false
  }
}
</script>

<style scoped>
.dl-btn {
  position: absolute;
  right: 6px;
  bottom: 6px;
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  background-color: rgba(0, 0, 0, 0.55);
  color: var(--color-surface);
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.15s ease;
}
.media-cell:hover .dl-btn,
.dl-btn:focus-visible {
  opacity: 1;
}
.dl-btn:hover {
  background-color: rgba(0, 0, 0, 0.75);
}
.dl-spinner {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.35);
  border-top-color: var(--color-surface);
  border-radius: 50%;
  animation: dl-spin 0.8s linear infinite;
}
@keyframes dl-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
