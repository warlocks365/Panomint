<template>
  <div class="people-page">
    <header class="page-toolbar">
      <h2 class="page-title">人物</h2>
      <span v-if="!page.loading.value && !page.loadError.value" class="page-sub">
        {{ page.named.value.length }} 位已命名 · {{ page.unnamed.value.length }} 个待命名
      </span>
      <div class="spacer"></div>
      <button class="btn" :disabled="page.scanning.value" @click="page.triggerScan">
        {{ page.scanning.value ? '已提交…' : '重新扫描人脸' }}
      </button>
    </header>

    <p v-if="page.loading.value" class="page-tip">加载中…</p>
    <p v-else-if="page.loadError.value" class="page-tip error">
      {{ page.loadError.value }}
      <button class="retry-btn" @click="page.load">重试</button>
    </p>

    <template v-else>
      <p v-if="page.scanNotice.value" class="notice">{{ page.scanNotice.value }}</p>

      <section v-if="page.named.value.length" class="section">
        <h3 class="section-title">
          已命名
          <span v-if="page.namedSelected.value.length" class="batch-bar-entity" data-testid="people-batch-bar">
            <span class="pb-count">已选 {{ page.namedSelected.value.length }} 项</span>
            <button class="mini" data-testid="pb-select-all" @click="page.toggleAllNamed">
              {{ page.allNamedPicked() ? '取消全选' : '全选' }}
            </button>
            <button class="mini" data-testid="pb-invert" @click="page.invertNamed">反选</button>
            <button class="mini" data-testid="pb-hide" @click="page.batchHide(true)">隐藏</button>
            <button class="mini" data-testid="pb-unhide" @click="page.batchHide(false)">取消隐藏</button>
            <button class="mini" data-testid="pb-clear" @click="page.namedSelected.value = []">清除</button>
          </span>
        </h3>
        <div class="grid">
          <PersonCard
            v-for="p in page.named.value"
            :key="p.id"
            :person="p"
            :cover="page.covers[p.id]"
            :selected="page.namedSelected.value.includes(p.id)"
            variant="named"
            @pick="page.toggleNamedPick"
            @open="openPerson"
            @rename="page.startRename"
            @toggle-hidden="page.toggleHidden"
          />
        </div>
      </section>

      <section class="section">
        <h3 class="section-title">
          未命名聚类
          <span class="hint">勾选多个可合并为同一人</span>
          <template v-if="page.unnamed.value.length">
            <button class="mini" data-testid="pc-select-all" @click="page.toggleAllClusters">
              {{ page.allClustersPicked() ? '取消全选' : '全选' }}
            </button>
            <button class="mini" data-testid="pc-invert" @click="page.invertClusters">反选</button>
          </template>
          <button v-if="page.selected.value.length" class="mini primary" data-testid="people-merge-open" @click="page.openMerge">
            合并命名（已选 {{ page.selected.value.length }}）
          </button>
        </h3>
        <p v-if="!page.unnamed.value.length" class="page-tip">没有待命名的人脸聚类</p>
        <div v-else class="grid">
          <PersonCard
            v-for="c in page.unnamed.value"
            :key="c.cluster_id"
            :person="c"
            :cover="page.covers[c.cluster_id]"
            :selected="page.selected.value.includes(c.cluster_id)"
            variant="cluster"
            @pick="page.togglePick"
          />
        </div>
      </section>
    </template>

    <!-- 合并 / 改名自 Job000081 起走统一对话框宿主（dialogs.form / dialogs.prompt），内联对话框已清 -->
  </div>
</template>

<script setup>
// Job000090 拆解（页面级视图：数据操作编排走 composable，卡片展示拆双形态组件）：
// - usePeoplePage：加载/双勾选集/批量隐藏/合并命名/改名/单个隐藏/扫描（dialogs 统一宿主编排）
// - PersonCard：named/cluster 双形态卡片（结构同源，条件渲染两处差异；卡片样式全自持）
// 宿主留：页级工具栏/网格容器/batchbar/notice 与页面跳转（router 属视图职责）。
import { useRouter } from 'vue-router'
import PersonCard from '../components/people/PersonCard.vue'
import { usePeoplePage } from './people/usePeoplePage'

const router = useRouter()
const page = usePeoplePage()

function openPerson(p) {
  // 复用搜索页的人物筛选（契约 §8 GET /search?person=），无需新路由
  router.push({ name: 'search', query: { person: p.id } })
}
</script>

<style scoped>
.people-page {
  padding: 20px 24px;
}

.page-toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}

.page-title {
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}

.page-sub {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.spacer {
  flex: 1;
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

.page-tip {
  padding: 40px 0;
  text-align: center;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
}

.page-tip.error {
  color: var(--color-danger);
}

.retry-btn {
  margin-left: 8px;
  border: none;
  background: none;
  color: var(--color-primary);
  cursor: pointer;
  font-size: var(--font-size-md);
}

.notice {
  margin-bottom: 12px;
  padding: 8px 12px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
  font-size: var(--font-size-sm);
}

.section {
  margin-bottom: 28px;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--font-size-md);
  color: var(--color-text-primary);
  margin-bottom: 12px;
}

.hint {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  font-weight: 400;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(148px, 1fr));
  gap: 16px;
}

.batch-bar-entity {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 400;
}

.pb-count {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}

.mini {
  flex: 1;
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-sm);
  background-color: var(--color-surface);
  color: var(--color-text-primary);
  font-size: var(--font-size-sm);
  cursor: pointer;
}

.mini:hover {
  background-color: var(--color-surface-hover);
}

.mini.primary {
  border-color: var(--color-primary);
  color: #fff;
  background-color: var(--color-primary);
}
</style>
