<template>
  <div class="toolbox-page">
    <header class="tb-head">
      <h2 class="tb-title">工具箱</h2>
      <p class="tb-sub">查找重复媒体、找回误删内容</p>
    </header>

    <div class="tb-tabs" data-testid="toolbox-tabs" role="tablist">
      <button
        class="tb-tab"
        :class="{ active: tab === 'dup' }"
        data-testid="toolbox-tab-dup"
        @click="tab = 'dup'"
      >
        重复项目
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'trash' }"
        data-testid="toolbox-tab-trash"
        @click="tab = 'trash'"
      >
        最近删除
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'restored' }"
        data-testid="toolbox-tab-restored"
        @click="tab = 'restored'"
      >
        已恢复
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'scan' }"
        data-testid="toolbox-tab-scan"
        @click="tab = 'scan'"
      >
        扫描导入
      </button>
      <button
        class="tb-tab"
        :class="{ active: tab === 'shares' }"
        data-testid="toolbox-tab-shares"
        @click="tab = 'shares'"
      >
        我的分享
      </button>
    </div>

    <ToolboxDupPanel v-if="tab === 'dup'" />

    <!-- 最近删除 / 已恢复：只做引导，回收站本体唯一实现在时间轴面板里 -->
    <section v-else-if="tab === 'trash'" class="tb-panel tb-guide">
      <h3 class="guide-title">最近删除</h3>
      <p class="guide-text">
        删除是软删：媒体会先进入回收站，随时可以恢复。回收站的列表与恢复操作统一在
        时间轴的「回收站」面板里，这里只提供入口 —— 同一套逻辑放两处实现，迟早会各改各的。
      </p>
      <button class="btn primary" @click="goTrash">打开时间轴回收站</button>
    </section>

    <ToolboxRestoredPanel v-else-if="tab === 'restored'" :active="tab === 'restored'" />

    <!-- 扫描导入（Job000123）：管理员分配扫描根后，成员自助导入挂载目录里的媒体 -->
    <ToolboxScanPanel v-else-if="tab === 'scan'" />

    <!-- Job000134：我的分享——列表/状态/观看统计/吊销/二维码（ShareManageList 内聚） -->
    <div v-if="tab === 'shares'" class="tb-shares" data-testid="toolbox-my-shares">
      <ShareManageList />
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import ToolboxDupPanel from './toolbox/DupPanel.vue'
import ToolboxRestoredPanel from './toolbox/RestoredPanel.vue'
import ToolboxScanPanel from './toolbox/ScanPanel.vue'
import ShareManageList from '../components/shares/ShareManageList.vue'

const router = useRouter()

const tab = ref('dup')

function goTrash() {
  router.push({ path: '/timeline', query: { trash: '1' } })
}

</script>

<style scoped>
.toolbox-page {
  padding: 20px 24px;
}

.tb-head {
  margin-bottom: 14px;
}

.tb-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.tb-sub {
  margin-top: 4px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.tb-tabs {
  display: flex;
  gap: 4px;
  border-bottom: 1px solid var(--color-border);
  margin-bottom: 16px;
}

.tb-tab {
  border: none;
  background: transparent;
  padding: 9px 14px;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}

.tb-tab:hover {
  color: var(--color-text-primary);
}

.tb-tab.active {
  color: var(--color-primary);
  border-bottom-color: var(--color-primary);
}

.tb-guide {
  max-width: 620px;
}

.guide-title {
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  margin-bottom: 8px;
}

.guide-text {
  font-size: var(--font-size-md);
  line-height: 1.8;
  color: var(--color-text-secondary);
  margin-bottom: 14px;
}

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  background-color: var(--color-surface);
  cursor: pointer;
}

.btn.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}
</style>
