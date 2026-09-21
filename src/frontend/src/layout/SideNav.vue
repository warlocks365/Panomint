<template>
  <aside class="sidebar" :class="`sidebar--${mode}`">
    <div class="brand">
      <span class="brand-mark">全</span>
      <span class="brand-text">全景相册</span>
    </div>
    <nav class="nav">
      <router-link
        v-for="item in navItems"
        :key="item.label"
        :to="item.ready ? item.to : '#'"
        class="nav-item"
        :class="{ 'nav-item--disabled': !item.ready }"
        :title="item.label"
        @click="!item.ready && $event.preventDefault()"
      >
        <span class="nav-icon" v-html="item.icon"></span>
        <span class="nav-label">{{ item.label }}</span>
        <span v-if="!item.ready" class="badge">开发中</span>
      </router-link>
    </nav>
  </aside>
</template>

<script setup>
// 侧边栏（品牌 + 导航）——从 AppShell 抽出（Job000058-3）。
// 管理入口按 owner/admin 角色显示；服务端仍按 admin:users/admin:system 强校验，这里只是门面。
import { computed } from 'vue'
import { useAuthStore } from '../stores/auth'
import { navIcons as icons } from './navIcons'

defineProps({
  mode: { type: String, default: 'expanded' } // expanded | icon | hidden
})

const auth = useAuthStore()

const navItems = computed(() => {
  const items = [
    { label: '时间轴', to: '/timeline', ready: true, icon: icons.timeline },
    { label: '地图', to: '/map', ready: true, icon: icons.map },
    { label: '相册', to: '/albums', ready: true, icon: icons.album },
    { label: '人物', to: '/people', ready: true, icon: icons.person },
    { label: '地点', ready: false, icon: icons.place },
    { label: '标签', to: '/tags', ready: true, icon: icons.tag },
    { label: '空间', to: '/spaces', ready: true, icon: icons.spaces },
    { label: '文件夹', ready: false, icon: icons.folder },
    { label: '工具箱', to: '/toolbox', ready: true, icon: icons.toolbox },
    { label: '设置', to: '/settings', ready: true, icon: icons.settings }
  ]
  const role = auth.user?.role
  if (role === 'owner' || role === 'admin') {
    items.push({ label: '管理', to: '/admin', ready: true, icon: icons.admin })
  }
  return items
})
</script>

<style scoped>
.sidebar {
  width: var(--sidebar-width);
  flex-shrink: 0;
  background-color: var(--color-surface);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  overflow: hidden;
  transition: width 0.16s ease;
}

.sidebar--icon {
  width: var(--sidebar-width-icon);
}

.sidebar--hidden {
  width: 0;
  border-right: none;
}

/* 图标态：只留图标，隐藏文案与角标 */
.sidebar--icon .brand {
  justify-content: center;
  padding: 0;
}

.sidebar--icon .brand-text,
.sidebar--icon .nav-label,
.sidebar--icon .badge {
  display: none;
}

.sidebar--icon .nav-item {
  justify-content: center;
  gap: 0;
  padding: 9px 0;
}

/* 收起态：宽度 0，内容一并移除（避免残留不可见但可聚焦的元素） */
.sidebar--hidden .brand,
.sidebar--hidden .nav {
  display: none;
}

.brand {
  height: var(--topbar-height);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 20px;
  font-size: var(--font-size-lg);
  font-weight: 600;
  border-bottom: 1px solid var(--color-border);
}

/* 图标态下的品牌方块（展开态不显示，PC 观感不变） */
.brand-mark {
  display: none;
}

.sidebar--icon .brand-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  background-color: var(--color-primary);
  color: #fff;
  font-size: var(--font-size-sm);
}

.nav {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 12px 8px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
  text-decoration: none;
}

.nav-item:hover {
  background-color: var(--color-surface-hover);
}

.nav-item.router-link-active {
  background-color: var(--color-primary-active-bg);
  color: var(--color-primary);
}

.nav-item--disabled {
  color: var(--color-text-disabled);
  cursor: default;
}

.nav-icon {
  display: inline-flex;
  align-items: center;
}

.nav-label {
  flex: 1;
}

.badge {
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 8px;
  background-color: var(--color-warning-bg);
  color: var(--color-warning-text);
}
</style>
