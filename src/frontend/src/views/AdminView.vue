<template>
  <div class="admin-page">
    <header class="page-head">
      <h1 class="page-title">管理后台</h1>
      <p class="page-sub">用户、角色、任务、审计与系统配置</p>
    </header>

    <div v-if="forbidden" class="card">
      <p class="msg msg--error" data-testid="admin-forbidden">
        当前账号没有管理权限（需要 admin:users 或 admin:system）。
      </p>
    </div>

    <template v-else>
      <nav class="tabs" role="tablist">
        <button
          v-for="t in tabs"
          :key="t.key"
          type="button"
          role="tab"
          class="tab"
          :class="{ 'tab--active': activeTab === t.key }"
          :aria-selected="activeTab === t.key"
          :data-testid="`tab-${t.key}`"
          @click="activeTab = t.key"
        >
          {{ t.label }}
        </button>
      </nav>

      <!-- 页签组件按需挂载，切换后保留状态（keep-alive 语义 = v-show） -->
      <OverviewTab v-show="activeTab === 'overview'" data-testid="panel-overview" />
      <UsersTab v-show="activeTab === 'users'" data-testid="panel-users" />
      <RolesTab v-show="activeTab === 'roles'" data-testid="panel-roles" />
      <PermTab v-show="activeTab === 'perms'" data-testid="panel-perms" />
      <JobsTab v-show="activeTab === 'jobs'" data-testid="panel-jobs" />
      <AuditTab v-show="activeTab === 'audit'" data-testid="panel-audit" />
      <MapConfigTab v-show="activeTab === 'map'" data-testid="panel-map" />
    </template>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { getStats, listUsers } from '../api/admin'
import OverviewTab from './admin/OverviewTab.vue'
import UsersTab from './admin/UsersTab.vue'
import RolesTab from './admin/RolesTab.vue'
import PermTab from './admin/PermTab.vue'
import JobsTab from './admin/JobsTab.vue'
import AuditTab from './admin/AuditTab.vue'
import MapConfigTab from './admin/MapConfigTab.vue'

const auth = useAuthStore()
const activeTab = ref('overview')
const forbidden = ref(false)

const tabs = [
  { key: 'overview', label: '概览' },
  { key: 'users', label: '用户' },
  { key: 'roles', label: '角色' },
  { key: 'perms', label: '权限' },
  { key: 'jobs', label: '任务' },
  { key: 'audit', label: '审计' },
  { key: 'map', label: '地图配置' }
]

// 权限预检：管理端点分两级（admin:users 管用户/角色/审计，admin:system 管概览/任务/地图配置）。
// 任一级可达即显示页签骨架；两级都 403 才整页提示。服务端仍是唯一裁决者，这里只是门面。
async function probePerm() {
  try {
    await listUsers()
    return
  } catch (e) {
    if (e?.response?.status !== 403) return // 网络错等不由预检裁决，交给页签自身报错
  }
  try {
    await getStats()
  } catch (e) {
    if (e?.response?.status === 403) {
      forbidden.value = true
    }
  }
}

onMounted(async () => {
  if (!auth.user) {
    try {
      await auth.fetchMe()
    } catch {
      /* 401 由拦截器统一处理 */
    }
  }
  await probePerm()
})
</script>

<style scoped>
.admin-page {
  max-width: 1080px;
  margin: 0 auto;
}
.page-head {
  margin-bottom: 16px;
}
.page-title {
  margin: 0;
  font-size: var(--font-size-lg);
  color: var(--color-text-primary);
}
.page-sub {
  margin: 4px 0 0;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
}
.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  margin-bottom: 16px;
  border-bottom: 1px solid var(--color-border);
}
.tab {
  padding: 8px 14px;
  border: none;
  background: none;
  font-size: var(--font-size-md);
  color: var(--color-text-secondary);
  cursor: pointer;
  border-bottom: 2px solid transparent;
  margin-bottom: -1px;
}
.tab:hover {
  color: var(--color-text-primary);
}
.tab--active {
  color: var(--color-primary);
  border-bottom-color: var(--color-primary);
  font-weight: 600;
}
.card {
  background: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  padding: 20px;
}
.msg {
  margin: 0;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}
</style>
