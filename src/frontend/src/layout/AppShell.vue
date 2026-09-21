<template>
  <div class="shell">
    <SideNav :mode="sidebarMode" />

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

        <UserMenu />
      </header>

      <main class="content" :class="{ 'content--flush': flushContent }">
        <router-view />
      </main>
    </div>
  </div>
</template>

<script setup>
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '../stores/auth'
import { useResponsive } from '../composables/useResponsive'
import SideNav from './SideNav.vue'
import UserMenu from './UserMenu.vue'
import SearchBar from '../components/search/SearchBar.vue'

const route = useRoute()
const auth = useAuthStore()
const { isMobile } = useResponsive()


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


</script>

<style scoped>
.shell {
  display: flex;
  height: 100%;
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
