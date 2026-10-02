<template>
  <div class="toolbox-page">
    <header class="tb-head">
      <div class="ph-heading">
        <span class="ph-eyebrow">08</span>
        <h2 class="tb-title">工具箱</h2>
      </div>
      <p class="tb-sub">查找重复媒体 · 找回误删内容</p>
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

    <!-- tabs-fade：面板切换 y12 淡入（CSS 过渡；prefers-reduced-motion 由 media 禁用） -->
    <Transition name="tf" mode="out-in">
      <div :key="tab" class="tb-panel-zone">
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
    </Transition>
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

/* 页头规范（DESIGN.md §5） */
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

.tb-title {
  font-size: var(--font-size-lg);
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}

.tb-sub {
  margin-top: 4px;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}

/* 胶囊分段 tabs（对齐空间页分段切换语言，DESIGN.md 深化稿 VIEW.08） */
.tb-tabs {
  display: inline-flex;
  gap: 2px;
  background-color: var(--color-surface);
  border-radius: 999px;
  padding: 3px;
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.06);
  margin-bottom: 20px;
}

.tb-tab {
  border: none;
  background: transparent;
  padding: 7px 18px;
  border-radius: 999px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  cursor: pointer;
  font-family: var(--font-family);
  transition: background-color 0.5s cubic-bezier(0.32, 0.72, 0, 1), color 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}

.tb-tab:hover {
  color: var(--color-text-primary);
}

.tb-tab.active {
  background-color: var(--color-primary);
  color: #eef1f4;
  border-bottom-color: transparent;
  font-weight: 500;
}

/* tabs-fade 面板切换过渡（CSS；reduced-motion 禁用） */
.tf-enter-active,
.tf-leave-active {
  transition: opacity 0.4s ease, transform 0.4s ease;
}

.tf-enter-from {
  opacity: 0;
  transform: translateY(12px);
}

.tf-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}

@media (prefers-reduced-motion: reduce) {
  .tf-enter-active,
  .tf-leave-active {
    transition: none;
  }
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
