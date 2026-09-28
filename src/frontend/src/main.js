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
// Job000136：SW 更新提示——检测到新版本立即提示刷新，根治「发版后界面不更新」。
if (import.meta.env.PROD && 'serviceWorker' in navigator) {
  window.addEventListener('load', () => {
    let refreshing = false
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      if (refreshing) return // 防止多标签页连锁刷新
      refreshing = true
      window.location.reload()
    })
    const pingWaiting = (reg) => {
      if (!reg || !reg.waiting) return
      const bar = document.createElement('div')
      bar.style.cssText =
        'position:fixed;left:0;right:0;bottom:0;z-index:9999;display:flex;align-items:center;' +
        'justify-content:center;gap:12px;padding:12px 16px;background:#1f2937;color:#fff;' +
        'font:14px/1.4 system-ui,-apple-system,sans-serif;box-shadow:0 -2px 12px rgba(0,0,0,.25)'
      bar.innerHTML =
        '<span>检测到新版本，刷新后生效</span>' +
        '<button style="border:none;border-radius:6px;padding:6px 16px;background:#2563eb;color:#fff;' +
        'cursor:pointer;font:inherit">立即刷新</button>'
      bar.querySelector('button').addEventListener('click', () => {
        reg.waiting.postMessage({ type: 'SKIP_WAITING' })
      })
      document.body.appendChild(bar)
    }
    navigator.serviceWorker.register('/sw.js').then((reg) => {
      pingWaiting(reg)
      reg.addEventListener('updatefound', () => {
        const nw = reg.installing
        if (nw) nw.addEventListener('statechange', () => { if (nw.state === 'installed') pingWaiting(reg) })
      })
      // 每 60s 主动探新（SW 静默更新周期默认依赖导航时机，主动探更及时）
      setInterval(() => reg.update().catch(() => {}), 60000)
    }).catch(() => {})
    navigator.serviceWorker.getRegistration().then(pingWaiting).catch(() => {})
  })
}

