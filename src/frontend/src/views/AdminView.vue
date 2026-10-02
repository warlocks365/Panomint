<template>
  <div class="admin-page">
    <header class="page-head">
      <div class="ph-heading">
        <span class="ph-eyebrow">11</span>
        <h1 class="page-title">管理后台</h1>
      </div>
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
      <AccountsTab v-show="activeTab === 'accounts'" data-testid="panel-accounts" />
      <RolesTab v-show="activeTab === 'roles'" data-testid="panel-roles" />
      <PermTab v-show="activeTab === 'perms'" data-testid="panel-perms" />
      <JobsTab v-show="activeTab === 'jobs'" data-testid="panel-jobs" />
      <TranscodeTab v-show="activeTab === 'transcode'" data-testid="panel-transcode" />
      <NetworkTab v-show="activeTab === 'network'" data-testid="panel-network" />
      <AuditTab v-show="activeTab === 'audit'" data-testid="panel-audit" />
      <MapConfigTab v-show="activeTab === 'map'" data-testid="panel-map" />
      <StorageTab v-show="activeTab === 'storage'" data-testid="panel-storage" />
    </template>
  </div>
</template>

<script setup>
import { nextTick, onMounted, ref, watch } from 'vue'
import { useAuthStore } from '../stores/auth'
import gsap from 'gsap'
import { getStats, listUsers } from '../api/admin'
import OverviewTab from './admin/OverviewTab.vue'
import UsersTab from './admin/UsersTab.vue'
import AccountsTab from './admin/AccountsTab.vue'
import RolesTab from './admin/RolesTab.vue'
import PermTab from './admin/PermTab.vue'
import JobsTab from './admin/JobsTab.vue'
import TranscodeTab from './admin/TranscodeTab.vue'
import NetworkTab from './admin/NetworkTab.vue'
import AuditTab from './admin/AuditTab.vue'
import MapConfigTab from './admin/MapConfigTab.vue'
import StorageTab from './admin/StorageTab.vue'

const auth = useAuthStore()
const activeTab = ref('overview')
const forbidden = ref(false)

const tabs = [
  { key: 'overview', label: '概览' },
  { key: 'users', label: '用户' },
  { key: 'accounts', label: '账号策略' },
  { key: 'roles', label: '角色' },
  { key: 'perms', label: '权限' },
  { key: 'jobs', label: '任务' },
  { key: 'transcode', label: '转码' },
  { key: 'network', label: '网络' },
  { key: 'audit', label: '审计' },
  { key: 'map', label: '地图配置' },
  { key: 'storage', label: '存储' }
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

// DESIGN.md §8 panel-fade：页签切换 y12 淡入（v-show 面板 keep-alive 语义保留；
// 对全部面板 fromTo——display:none 的无视觉副作用；clearProps 还原；reduced-motion 跳过）
watch(activeTab, async () => {
  await nextTick()
  requestAnimationFrame(() => {
    const mm = gsap.matchMedia()
    mm.add('(prefers-reduced-motion: no-preference)', () => {
      gsap.fromTo(
        '.admin-page [data-testid^="panel-"]',
        { y: 12, opacity: 0 },
        { y: 0, opacity: 1, duration: 0.35, ease: 'power2.out', clearProps: 'transform,opacity' }
      )
    })
  })
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
.page-title {
  margin: 0;
  font-size: var(--font-size-lg);
  letter-spacing: 0.12em;
  color: var(--color-text-primary);
}
.page-sub {
  margin: 4px 0 0;
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.02em;
  color: var(--color-text-secondary);
}
/* 胶囊分段 tabs（对齐工具箱/空间页语言；11 页签 wrap） */
.tabs {
  display: inline-flex;
  flex-wrap: wrap;
  gap: 2px;
  padding: 3px;
  background-color: var(--color-surface);
  border-radius: 999px;
  box-shadow: 0 1px 3px rgba(65, 64, 60, 0.06);
  margin-bottom: 18px;
}
.tab {
  padding: 6px 15px;
  border: none;
  border-radius: 999px;
  background: transparent;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  cursor: pointer;
  font-family: var(--font-family);
  transition: background-color 0.5s cubic-bezier(0.32, 0.72, 0, 1), color 0.5s cubic-bezier(0.32, 0.72, 0, 1);
}
.tab:hover {
  color: var(--color-text-primary);
}
.tab--active {
  background-color: var(--color-primary);
  color: #eef1f4;
  font-weight: 500;
}
.card {
  background: var(--color-surface);
  border: none;
  border-radius: var(--radius-md);
  padding: 20px;
  box-shadow: var(--shadow-card);
}
.msg {
  margin: 0;
  font-size: var(--font-size-md);
}
.msg--error {
  color: var(--color-danger);
}
</style>
