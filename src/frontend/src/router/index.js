import { createRouter, createWebHistory } from 'vue-router'
import { getAccessToken } from '../utils/tokenStore'
import { useAuthStore } from '../stores/auth'
import { getSetupStatus } from '../api/setup'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/LoginView.vue'),
    meta: { public: true }
  },
  {
    // Job000107 首次安装引导（一次性初始化向导）：未初始化时所有导航被守卫重定向到这里
    path: '/setup',
    name: 'setup',
    component: () => import('../views/SetupView.vue'),
    meta: { public: true }
  },
  {
    // Stage 2 公开分享页（免登，token 即凭证；G-Share 实现文件）
    path: '/share/:token',
    name: 'share-public',
    component: () => import('../views/SharePublicView.vue'),
    meta: { public: true }
  },
  {
    path: '/',
    component: () => import('../layout/AppShell.vue'),
    children: [
      { path: '', redirect: '/timeline' },
      {
        path: 'timeline',
        name: 'timeline',
        component: () => import('../views/TimelineView.vue')
      },
      // Job000009 地图模式（全屏地图 + 时间轴双向联动）
      {
        path: 'map',
        name: 'map',
        component: () => import('../views/MapView.vue')
      },
      {
        path: 'places',
        name: 'places',
        component: () => import('../views/PlacesView.vue')
      },
      // Wave 2 路由（G2/G3/G4 视图，由各组实现对应文件）
      {
        path: 'spaces',
        name: 'spaces',
        component: () => import('../views/SpacesView.vue')
      },
      {
        path: 'folders',
        name: 'folders',
        component: () => import('../views/FoldersView.vue')
      },
      {
        path: 'upload',
        name: 'upload',
        component: () => import('../views/UploadView.vue')
      },
      {
        path: 'player/:id',
        name: 'player',
        component: () => import('../views/PlayerView.vue')
      },
      // Stage 3 搜索结果页
      {
        path: 'search',
        name: 'search',
        component: () => import('../views/SearchResultsView.vue')
      },
      // Stage 1 相册路由（G-Frontend 实现对应文件）
      {
        path: 'albums',
        name: 'albums',
        component: () => import('../views/AlbumsView.vue')
      },
      {
        path: 'albums/:id',
        name: 'album-detail',
        component: () => import('../views/AlbumDetailView.vue')
      },
      // Job000010 Phase 4：标签管理 / 人物
      {
        path: 'tags',
        name: 'tags',
        component: () => import('../views/TagsView.vue')
      },
      {
        path: 'people',
        name: 'people',
        component: () => import('../views/PeopleView.vue')
      },
      // PRD §6.16 工具箱（重复项目 / 最近删除 / 已恢复）
      {
        path: 'toolbox',
        name: 'toolbox',
        component: () => import('../views/ToolboxView.vue')
      },
      // Phase 5：账号设置（二次验证 TOTP 的启用/关闭）
      {
        path: 'settings',
        name: 'settings',
        component: () => import('../views/SettingsView.vue')
      },
      // Job000052 管理后台（需 admin:users / admin:system 权限，服务端强校验；入口按角色显示）
      {
        path: 'admin',
        name: 'admin',
        component: () => import('../views/AdminView.vue')
      }
    ]
  },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach(async (to) => {
  // 安装向导闸门（Job000107）：未初始化时全站只放行 /setup；已初始化后 /setup 也不再出现。
  // 查询失败（null）时不拦 —— 可用性优先，SetupView 提交时后端仍会 fail-closed 裁决。
  try {
    const st = await getSetupStatus()
    if (st && !st.initialized && to.name !== 'setup') {
      return { name: 'setup' }
    }
    if (st && st.initialized && to.name === 'setup') {
      return { name: 'login' }
    }
  } catch {
    // 状态不可达：不阻塞导航
  }

  if (!to.meta.public && !getAccessToken()) {
    return { name: 'login' }
  }
  if (to.name === 'login' && getAccessToken()) {
    return { path: '/' }
  }
  // 启动引导：token 仍在（如页面刷新）但用户信息未恢复时补拉一次 /auth/me。
  // 失败不阻塞导航；401 由 http 拦截器统一走 forceLogout。
  if (!to.meta.public) {
    const auth = useAuthStore()
    if (!auth.user) {
      try {
        await auth.fetchMe()
      } catch {
        // 忽略：页面照常渲染，顶栏暂时显示兜底值
      }
    }
  }
  return true
})

export default router
