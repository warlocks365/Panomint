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
    path: '/',
    component: () => import('../layout/AppShell.vue'),
    children: [
      { path: '', redirect: '/timeline' },
      {
        path: 'timeline',
        name: 'timeline',
        component: () => import('../views/TimelineView.vue')
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
