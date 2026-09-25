<template>
  <section class="card" data-testid="version-card">
    <h2 class="card-title">版本信息</h2>

    <dl class="info-list">
      <div class="info-row">
        <dt>当前版本</dt>
        <dd data-testid="version-current">
          {{ info.version || '加载中…' }}
          <span v-if="info.version === 'dev'" class="ver-badge" data-testid="version-dev-badge">开发构建</span>
        </dd>
      </div>
      <div v-if="info.commit" class="info-row">
        <dt>构建提交</dt>
        <dd class="ver-mono" data-testid="version-commit">{{ info.commit }}</dd>
      </div>
      <div v-if="info.build_date" class="info-row">
        <dt>构建时间</dt>
        <dd class="ver-mono" data-testid="version-build-date">{{ info.build_date }}</dd>
      </div>
    </dl>

    <button
      class="btn ver-toggle"
      type="button"
      data-testid="version-history-toggle"
      @click="showHistory = !showHistory"
    >
      {{ showHistory ? '收起更新说明' : '查看更新说明' }}
    </button>

    <ol v-if="showHistory" class="ver-history" data-testid="version-history">
      <li v-for="rel in VERSION_HISTORY" :key="rel.version" class="ver-rel">
        <div class="ver-rel-head">
          <span class="ver-rel-version">v{{ rel.version }}</span>
          <span class="ver-rel-date">{{ rel.date }}</span>
          <span v-if="rel.version === info.version" class="ver-badge">当前版本</span>
        </div>
        <ul class="ver-rel-items">
          <li v-for="(item, i) in rel.items" :key="i">{{ item }}</li>
        </ul>
      </li>
    </ol>
  </section>
</template>

<script setup>
// 版本信息卡（Job000117）：当前版本以 GET /version 为唯一真源（发布注入/开发恒 dev）；
// 历史与说明来自 constants/versionHistory.js（发布时人工维护，见该文件头注释）。
import { onMounted, ref } from 'vue'
import { getVersion } from '../../api/version'
import { VERSION_HISTORY } from '../../constants/versionHistory'

const info = ref({ version: '', commit: '', build_date: '' })
const showHistory = ref(false)

onMounted(async () => {
  try {
    info.value = await getVersion()
  } catch {
    info.value = { version: '获取失败', commit: '', build_date: '' }
  }
})
</script>

<style scoped>
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
}
.card-title {
  margin: 0 0 12px;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
.info-list {
  margin: 0;
}
.info-row {
  display: flex;
  gap: 16px;
  padding: 6px 0;
  font-size: var(--font-size-md);
}
.info-row dt {
  flex: none;
  width: 80px;
  color: var(--color-text-secondary);
}
.info-row dd {
  margin: 0;
  color: var(--color-text-primary);
}
.ver-mono {
  font-family: monospace;
  font-size: var(--font-size-sm);
}
.ver-badge {
  display: inline-block;
  margin-left: 8px;
  padding: 1px 8px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-sm);
  color: var(--color-primary);
  border: 1px solid var(--color-primary);
}
.ver-toggle {
  margin-top: 10px;
}
.btn {
  padding: 6px 14px;
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
.ver-history {
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--color-border);
}
.ver-rel {
  padding: 12px 0;
  border-bottom: 1px solid var(--color-border);
}
.ver-rel-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.ver-rel-version {
  font-weight: 600;
  color: var(--color-text-primary);
}
.ver-rel-date {
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.ver-rel-items {
  margin: 0;
  padding-left: 18px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.ver-rel-items li {
  margin: 2px 0;
}
</style>
