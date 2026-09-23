<template>
  <div class="dlg-mask" @click.self="emit('close')">
    <div class="share-manage-dlg">
      <h3 class="confirm-title">分享管理</h3>
      <ShareManageList ref="listRef" />
      <div class="dlg-actions" style="margin-top: 16px">
        <button class="btn" @click="emit('close')">关闭</button>
      </div>
    </div>
  </div>
</template>

<script setup>
// AlbumDetailView 拆解（Job000087）：分享管理内联对话框。
// ShareManageList 仍由本组件内聚持有；宿主经 expose refresh() 在「新建分享成功」后刷新列表。
import { ref } from 'vue'
import ShareManageList from '../shares/ShareManageList.vue'

const emit = defineEmits(['close'])
const listRef = ref(null)

function refresh() {
  listRef.value?.refresh?.()
}

defineExpose({ refresh })
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

.confirm-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
  margin-bottom: 10px;
}

.dlg-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.share-manage-dlg {
  width: 720px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  overflow: auto;
  background-color: var(--color-surface);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-card);
  padding: 20px;
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
</style>
