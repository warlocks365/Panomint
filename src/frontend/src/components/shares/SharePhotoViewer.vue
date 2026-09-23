<template>
  <div class="viewer-mask" @click.self="$emit('close')">
    <button class="viewer-close" type="button" aria-label="关闭" @click="$emit('close')">
      <svg viewBox="0 0 24 24" width="22" height="22" fill="none">
        <path d="M6 6l12 12M18 6L6 18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
      </svg>
    </button>
    <img v-if="viewerUrl" :src="viewerUrl" :alt="item.filename || ''" class="viewer-img" />
    <div v-else class="spinner"></div>
  </div>
</template>

<script setup>
// SharePublicView 拆解（Job000084）：照片大图查看器。
// 挂载即取 viewer URL（objectURL 由本组件持有，关闭/卸载必须 revoke，避免泄漏）；
// 失败回退到宿主共享缓存里的缩略图 URL（不归本组件所有，不得 revoke）。
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { loadPublicViewerUrl } from './publicApi'

const props = defineProps({
  item: { type: Object, required: true },
  token: { type: String, required: true },
  password: { type: String, default: '' },
  fallbackThumbUrl: { type: String, default: '' } // 共享缓存的缩略图（viewer 加载失败时的回退展示）
})
defineEmits(['close'])

const viewerUrl = ref('')
let ownedUrl = ''
let alive = true

function setUrl(url, owned) {
  if (ownedUrl) URL.revokeObjectURL(ownedUrl)
  ownedUrl = owned ? url : ''
  viewerUrl.value = url
}

onMounted(async () => {
  setUrl('', false)
  try {
    const url = await loadPublicViewerUrl(props.token, props.item.id, props.password)
    if (alive) setUrl(url, true)
    else URL.revokeObjectURL(url) // 已关闭：立即回收，不滞留
  } catch (e) {
    if (alive) setUrl(props.fallbackThumbUrl, false)
  }
})

onBeforeUnmount(() => {
  alive = false
  if (ownedUrl) URL.revokeObjectURL(ownedUrl)
})
</script>

<style scoped>
.viewer-mask {
  position: fixed;
  inset: 0;
  z-index: 200;
  display: flex;
  align-items: center;
  justify-content: center;
  background-color: rgba(0, 0, 0, 0.9);
}

.viewer-close {
  position: absolute;
  top: 12px;
  right: 12px;
  z-index: 210;
  width: 40px;
  height: 40px;
  border: none;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
  background-color: rgba(255, 255, 255, 0.15);
  cursor: pointer;
}

.viewer-img {
  max-width: 100vw;
  max-height: 100vh;
  object-fit: contain;
}

.spinner {
  width: 28px;
  height: 28px;
  border: 3px solid var(--color-border);
  border-top-color: var(--color-primary);
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
