import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import './styles/tokens.css'
import './styles/base.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
app.mount('#app')

// PWA：注入 Web App Manifest（幂等，避免改动 index.html）
if (typeof document !== 'undefined' && !document.querySelector('link[rel="manifest"]')) {
  const link = document.createElement('link')
  link.rel = 'manifest'
  link.href = '/manifest.webmanifest'
  document.head.appendChild(link)
}

// PWA：注册自研 Service Worker（仅生产；幂等，注册失败不影响主流程）。
// 缓存策略见 public/sw.js —— 应用外壳预缓存+SWR，所有 API 一律 NetworkOnly（绝不缓存 JWT）。
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    navigator.serviceWorker.register('/sw.js').catch(() => {})
  })
}

