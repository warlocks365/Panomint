<template>
  <div class="shell">
    <aside class="sidebar" :class="`sidebar--${sidebarMode}`">
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

    <div class="main-area">
      <header class="topbar">
        <div class="topbar-left">
          <!-- 侧栏三态开关：放在顶栏左侧（而非侧栏内），保证 hidden 态下仍可呼出 -->
          <button
            class="sidebar-toggle"
            type="button"
            :title="toggleTitle"
            :aria-label="toggleTitle"
            @click="toggleSidebar"
          >
            <svg viewBox="0 0 16 16" width="16" height="16" fill="none">
              <path
                d="M2.5 4h11M2.5 8h11M2.5 12h11"
                stroke="currentColor"
                stroke-width="1.4"
                stroke-linecap="round"
              />
            </svg>
          </button>
          <!-- 地图页有自己的地名搜索（定位到地图），此处隐藏全局搜索框，避免搜地名被跳到 /search -->
          <SearchBar v-if="route.name !== 'map'" />
        </div>

        <div class="user-menu" ref="menuRef">
          <button class="user-btn" @click="menuOpen = !menuOpen">
            <span class="avatar">{{ avatarChar }}</span>
            <span class="user-name">{{ displayName }}</span>
            <svg viewBox="0 0 10 6" width="10" height="6" fill="none">
              <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
            </svg>
          </button>
          <div v-if="menuOpen" class="dropdown">
            <div class="dropdown-email">{{ auth.user?.email }}</div>
            <button class="dropdown-item" @click="onLogout">退出登录</button>
          </div>
        </div>
      </header>

      <main class="content" :class="{ 'content--flush': flushContent }">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useResponsive } from '../composables/useResponsive'
import SearchBar from '../components/search/SearchBar.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { isMobile } = useResponsive()

const menuOpen = ref(false)
const menuRef = ref(null)

// ---- 侧栏三态：expanded(220px) / icon(56px) / hidden(0) ----
const SIDEBAR_MODE_KEY = 'app_sidebar_mode'
const DESKTOP_MODES = ['expanded', 'icon', 'hidden']

function readStoredDesktopMode() {
  try {
    const v = localStorage.getItem(SIDEBAR_MODE_KEY)
    return DESKTOP_MODES.includes(v) ? v : null
  } catch {
    return null
  }
}

// 桌面端（≥1024px）：默认 expanded，与改动前行为完全一致；用户切换后持久化到 localStorage。
// 移动端：默认 icon（56px），把宽度让给内容/地图；移动端的选择**不持久化**，
// 每次进入都回落到 icon —— 否则「手机上把侧栏收到 hidden、之后在 PC 打开」会得到没有导航的 PC 页。
const desktopMode = ref(readStoredDesktopMode() || 'expanded')
const mobileMode = ref('icon')

const sidebarMode = computed(() => (isMobile.value ? mobileMode.value : desktopMode.value))

function toggleSidebar() {
  if (isMobile.value) {
    // 移动端只在「仅图标 / 隐藏」之间切换：220px 展开态正是问题①的根源
    mobileMode.value = mobileMode.value === 'icon' ? 'hidden' : 'icon'
    return
  }
  const i = DESKTOP_MODES.indexOf(desktopMode.value)
  desktopMode.value = DESKTOP_MODES[(i + 1) % DESKTOP_MODES.length]
  try {
    localStorage.setItem(SIDEBAR_MODE_KEY, desktopMode.value)
  } catch {}
}

const toggleTitle = computed(() => {
  if (isMobile.value) {
    return mobileMode.value === 'icon' ? '隐藏侧栏' : '显示侧栏'
  }
  return { expanded: '收起侧栏', icon: '隐藏侧栏', hidden: '展开侧栏' }[desktopMode.value]
})

// 仅移动端 + /map 取消内容区内边距：地图画布需要满屏（390×844 竖屏下 24px 内边距占可视宽 12%；
// 812×375 横屏下 48px 直接吃掉地图高度）。其余路由与桌面端保持原内边距，不改动既有观感。
const flushContent = computed(() => isMobile.value && route.name === 'map')

const displayName = computed(
  () => auth.user?.display_name || auth.user?.email || '用户'
)
const avatarChar = computed(() => displayName.value.charAt(0).toUpperCase())

const icons = {
  timeline:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><rect x="2" y="2" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/><rect x="2" y="7" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/><rect x="2" y="12" width="12" height="3" rx="1" stroke="currentColor" stroke-width="1.4"/></svg>',
  album:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><rect x="2" y="2" width="12" height="12" rx="2" stroke="currentColor" stroke-width="1.4"/><path d="M2 10l3.5-3.5 3 3L11 7l3 3" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></svg>',
  person:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><circle cx="8" cy="5.5" r="2.8" stroke="currentColor" stroke-width="1.4"/><path d="M2.5 14c.8-2.6 2.9-4 5.5-4s4.7 1.4 5.5 4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  place:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M8 14.5S3 10 3 6.5A5 5 0 0 1 13 6.5C13 10 8 14.5 8 14.5z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><circle cx="8" cy="6.5" r="1.8" stroke="currentColor" stroke-width="1.4"/></svg>',
  tag:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 2h5l7 7-5 5-7-7V2z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><circle cx="5.5" cy="5.5" r="1" fill="currentColor"/></svg>',
  folder:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 4a1 1 0 0 1 1-1h3.6l1.6 2H13a1 1 0 0 1 1 1v6a1 1 0 0 1-1 1H3a1 1 0 0 1-1-1V4z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/></svg>',
  settings:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><circle cx="8" cy="8" r="2.2" stroke="currentColor" stroke-width="1.4"/><path d="M8 1.8v2M8 12.2v2M1.8 8h2M12.2 8h2M3.6 3.6l1.4 1.4M11 11l1.4 1.4M12.4 3.6L11 5M5 11l-1.4 1.4" stroke="currentColor" stroke-width="1.4" stroke-linecap="round"/></svg>',
  map:
    '<svg viewBox="0 0 16 16" width="16" height="16" fill="none"><path d="M2 4l4-1.5 4 1.5 4-1.5v10L10 14l-4-1.5L2 14V4z" stroke="currentColor" stroke-width="1.4" stroke-linejoin="round"/><path d="M6 2.5v11M10 4v11" stroke="currentColor" stroke-width="1.4"/></svg>'
}

const navItems = [
  { label: '时间轴', to: '/timeline', ready: true, icon: icons.timeline },
  { label: '地图', to: '/map', ready: true, icon: icons.map },
  { label: '相册', ready: false, icon: icons.album },
  { label: '人物', to: '/people', ready: true, icon: icons.person },
  { label: '地点', ready: false, icon: icons.place },
  { label: '标签', to: '/tags', ready: true, icon: icons.tag },
  { label: '文件夹', ready: false, icon: icons.folder },
  { label: '设置', to: '/settings', ready: true, icon: icons.settings }
]

function onClickOutside(e) {
  if (menuRef.value && !menuRef.value.contains(e.target)) {
    menuOpen.value = false
  }
}

onMounted(() => document.addEventListener('click', onClickOutside))
onBeforeUnmount(() => document.removeEventListener('click', onClickOutside))

async function onLogout() {
  menuOpen.value = false
  await auth.logout()
  router.push('/login')
}
</script>

<style scoped>
.shell {
  display: flex;
  height: 100%;
}

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
  font-size: var(--font-size-md);
}

.nav {
  padding: 12px 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
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

.main-area {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.topbar {
  height: var(--topbar-height);
  flex-shrink: 0;
  background-color: var(--color-surface);
  border-bottom: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 0 20px;
}

.topbar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: 1;
  min-width: 0;
}

.sidebar-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--color-text-secondary);
}

.sidebar-toggle:hover {
  background-color: var(--color-surface-hover);
  color: var(--color-text-primary);
}

.user-menu {
  position: relative;
}

.user-btn {
  display: flex;
  align-items: center;
  gap: 8px;
  border: none;
  background: transparent;
  padding: 6px 8px;
  border-radius: var(--radius-sm);
  color: var(--color-text-primary);
  font-size: var(--font-size-md);
}

.user-btn:hover {
  background-color: var(--color-surface-hover);
}

.avatar {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  background-color: var(--color-primary);
  color: #fff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: var(--font-size-sm);
}

.dropdown {
  position: absolute;
  right: 0;
  top: calc(100% + 6px);
  width: 200px;
  background-color: var(--color-surface);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  box-shadow: var(--shadow-card);
  padding: 8px;
  z-index: 10;
}

.dropdown-email {
  padding: 6px 10px 10px;
  font-size: var(--font-size-sm);
  color: var(--color-text-secondary);
  border-bottom: 1px solid var(--color-border);
  margin-bottom: 6px;
  word-break: break-all;
}

.dropdown-item {
  width: 100%;
  text-align: left;
  border: none;
  background: transparent;
  padding: 8px 10px;
  border-radius: var(--radius-sm);
  font-size: var(--font-size-md);
  color: var(--color-danger);
}

.dropdown-item:hover {
  background-color: var(--color-surface-hover);
}

.content {
  flex: 1;
  overflow: auto;
  padding: var(--content-padding);
}

/* 移动端 /map：内容区满屏（画布需要吃掉全部宽高） */
.content--flush {
  padding: var(--content-padding-mobile);
  overflow: hidden;
}
</style>
