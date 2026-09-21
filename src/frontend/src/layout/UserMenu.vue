<template>
  <div class="user-menu" ref="menuRef">
    <button class="user-btn" data-testid="user-menu-btn" @click="menuOpen = !menuOpen">
      <span class="avatar">{{ avatarChar }}</span>
      <span class="user-name">{{ displayName }}</span>
      <svg viewBox="0 0 10 6" width="10" height="6" fill="none">
        <path d="M1 1l4 4 4-4" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
      </svg>
    </button>
    <div v-if="menuOpen" class="dropdown" data-testid="user-dropdown">
      <div class="dropdown-email">{{ auth.user?.email }}</div>
      <button class="dropdown-item" data-testid="user-logout" @click="onLogout">退出登录</button>
    </div>
  </div>
</template>

<script setup>
// 顶栏用户菜单（头像/下拉/登出）——从 AppShell 抽出（Job000058-3 超大文件拆分）。
// 点击外部关闭的监听自包含；登出走 auth store 后跳登录页。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '../stores/auth'

const auth = useAuthStore()
const router = useRouter()

const menuOpen = ref(false)
const menuRef = ref(null)

const displayName = computed(
  () => auth.user?.display_name || auth.user?.email || '用户'
)
const avatarChar = computed(() => displayName.value.charAt(0).toUpperCase())

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
  cursor: pointer;
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
  cursor: pointer;
}

.dropdown-item:hover {
  background-color: var(--color-surface-hover);
}
</style>
