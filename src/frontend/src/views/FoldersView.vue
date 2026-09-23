<template>
  <div class="folders-view">
    <h1 class="page-title">文件夹</h1>

    <div v-if="page.loadError.value" class="error-banner">{{ page.loadError.value }}</div>

    <div class="folders-body">
      <FolderTree
        :tree="page.tree.value"
        :loading="page.treeLoading.value"
        :selected-path="page.selectedPath.value"
        :total-count="page.totalCount.value"
        @select="page.selectFolder"
      />

      <section class="grid-panel">
        <FolderGridHeader
          :segments="page.pathSegments.value"
          :selected-path="page.selectedPath.value"
          :selected-owner="page.selectedOwner.value"
          :count="page.filteredItems.value.length"
          :media-loading="page.mediaLoading.value"
          @select="page.selectFolder"
          @manage="page.openManage"
        />

        <div v-if="page.mediaLoading.value" class="muted grid-tip">加载中…</div>
        <div v-else-if="page.filteredItems.value.length === 0" class="muted grid-tip">该目录暂无媒体</div>

        <MediaTileGrid :items="page.filteredItems.value" selectable @open="openItem" @changed="page.reloadMedia" />
      </section>
    </div>

    <FolderManageDialog
      v-if="page.manageMode.value"
      :mode="page.manageMode.value"
      :target-path="page.selectedPath.value"
      :initial-grants="page.selectedGrants.value"
      :users="page.users.value"
      @close="page.closeManage"
      @done="page.onManaged"
    />
  </div>
</template>

<script setup>
// 文件夹页面宿主（Job000092：数据编排走 useFoldersPage，网格头部拆 FolderGridHeader）。
// 宿主职责：页级布局、目录树/网格容器装配、openItem 跳转 player（router 属视图职责）。
import { useRouter } from 'vue-router'
import FolderTree from './folders/FolderTree.vue'
import FolderGridHeader from './folders/FolderGridHeader.vue'
import FolderManageDialog from './folders/FolderManageDialog.vue'
import MediaTileGrid from '../components/media/MediaTileGrid.vue'
import { useFoldersPage } from './folders/useFoldersPage'

const router = useRouter()
const page = useFoldersPage()

function openItem(m) {
  router.push({ name: 'player', params: { id: m.id } })
}
</script>

<style scoped>
.folders-view {
  display: flex;
  flex-direction: column;
  gap: 20px;
  height: 100%;
}

.page-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
}

.error-banner {
  padding: 10px 14px;
  border-radius: var(--radius-md);
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
  font-size: var(--font-size-sm);
}

.folders-body {
  display: flex;
  gap: 20px;
  min-height: 0;
  flex: 1;
}

.grid-panel {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.grid-tip {
  padding: 48px 0;
  text-align: center;
}
</style>
