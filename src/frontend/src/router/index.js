import { createRouter, createWebHistory } from 'vue-router'
import { getAccessToken } from '../utils/tokenStore'

const routes = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/LoginView.vue'),
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
      }
    ]
  },
  { path: '/:pathMatch(.*)*', redirect: '/' }
]

const router = createRouter({
  history: createWebHistory(),
  routes
})

router.beforeEach((to) => {
  if (!to.meta.public && !getAccessToken()) {
    return { name: 'login' }
  }
  if (to.name === 'login' && getAccessToken()) {
    return { path: '/' }
  }
  return true
})

export default router
