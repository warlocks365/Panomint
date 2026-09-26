import { createRouter, createWebHistory } from 'vue-router'
import { getAccessToken } from '../utils/tokenStore'
import { useAuthStore } from '../stores/auth'
import { getSetupStatus } from '../api/setup'
import { safeInternalPath } from '../utils/url'

// Job000125：setup 状态的模块级缓存（15s TTL）。
// 之前每次路由导航都发一次 GET /setup/status（时间轴缩放/地图拖动等高频导航场景白耗请求）；
// 初始化完成（SetupView 成功）与登出等低频事件远低于 TTL，行为安全。
// 不可达（网络错）不缓存失败——下次导航立即重试，保持「可用性优先」语义。
let setupCache = null // { initialized: boolean, expires: number }
const SETUP_TTL = 15000

export function invalidateSetupCache() {
  setupCache = null
}

async function setupInitialized() {
  if (setupCache && Date.now() < setupCache.expires) return setupCache.initialized
  const st = await getSetupStatus()
  if (st && typeof st.initialized === 'boolean') {
    setupCache = { initialized: st.initialized, expires: Date.now() + SETUP_TTL }
    return st.initialized
  }
  return null // 状态形状异常：按不可达处理（不拦）
}

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
      // Job000119 应用内操作手册（设置页入口；内容静态打包自 src/manual，无后端依赖）
      {
        path: 'manual',
        name: 'manual',
        component: () => import('../views/ManualView.vue')
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
  // 安装向导闸门（Job000107 首建；Job000125 增 15s 缓存）：未初始化时全站只放行 /setup；
  // 已初始化后 /setup 也不再出现。查询失败（null）时不拦 —— 可用性优先，
  // SetupView 提交时后端仍会 fail-closed 裁决。
  try {
    const initialized = await setupInitialized()
    if (initialized === false && to.name !== 'setup') {
      return { name: 'setup' }
    }
    if (initialized === true && to.name === 'setup') {
      return { name: 'login' }
    }
  } catch {
    // 状态不可达：不阻塞导航
  }

  if (!to.meta.public && !getAccessToken()) {
    // Job000125：携带原始目标路径，登录成功后回跳（safeInternalPath 校验在 LoginView）。
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  if (to.name === 'login' && getAccessToken()) {
    // 已登录访问 /login：尊重 redirect（合法时），否则回首页。
    // safeInternalPath 对空串/非法值一律返回 ''，统一走首页回退。
    const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : ''
    const safe = safeInternalPath(redirect)
    if (safe) return { path: safe }
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
