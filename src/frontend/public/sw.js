/* 全景相册 Service Worker（自研轻量，零依赖）
 *
 * 设计裁决（PWA 缓存策略）：
 *   1. 应用外壳（/、/index.html、/manifest.webmanifest、/app-icon.svg）—— 预缓存；
 *   2. 同源静态构建产物（/assets/*.js|css、字体、图标）—— StaleWhileRevalidate；
 *   3. 页面导航 —— NetworkFirst，离线回退到已缓存的 index.html；
 *   4. 所有 API（/auth /media /search … 见 API_PREFIX）—— NetworkOnly，**绝不缓存**：
 *      · 接口响应带 Bearer 鉴权、且按用户/会话变化，缓存会造成跨会话/跨用户串数据；
 *      · /auth/* 涉及 JWT 与刷新令牌，严禁落盘到 Cache Storage；
 *      · /media/:id/download 与 HLS 分片体积大且带 Range，缓存既无意义又易爆配额；
 *   5. 非 GET、跨域、Range 请求一律直接放行（交给网络）。
 *
 * 说明：SW 无法读取 localStorage 中的 JWT，故不做任何"后台带鉴权请求"；
 * 上传类操作全部在前台页面上下文中完成（见 backupManager.js）。
 */

// Job000136：版本常量随构建注入 __APP_VERSION__——每次发版哈希变化 → install 触发 →
// 新 SW 处于 waiting；页面侧探测到 waiting 后提示用户一键刷新。
const VERSION = '__APP_VERSION__'
const SHELL_CACHE = `pano-shell-${VERSION}`
const ASSET_CACHE = `pano-asset-${VERSION}`

// 预缓存的应用外壳（不依赖构建哈希，安全）
const SHELL = ['/', '/index.html', '/manifest.webmanifest', '/app-icon.svg']

// 与 nginx / vite proxy 保持一致的 API 前缀：命中即 NetworkOnly
const API_PREFIX = /^\/(auth|media|search|spaces|folders|transcode|albums|shares|public|admin|health|ready|metrics|geo|tiles|preferences|tags)(\/|$)/

// 可走 SWR 的静态资源（构建产物）
const STATIC_RE = /\.(?:js|mjs|css|woff2?|ttf|otf|png|jpe?g|gif|svg|webp|ico)$/

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting())
  )
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys
          .filter((k) => k.startsWith('pano-') && k !== SHELL_CACHE && k !== ASSET_CACHE)
          .map((k) => caches.delete(k))
      )
    ).then(() => self.clients.claim())
  )
})

self.addEventListener('fetch', (event) => {
  const req = event.request

  // 仅处理同源 GET；非 GET（上传/写操作）、Range（HLS/大文件）、跨域一律放行
  if (req.method !== 'GET') return
  if (req.headers.has('range')) return
  const url = new URL(req.url)
  if (url.origin !== self.location.origin) return

  // API：NetworkOnly —— 绝不缓存（JWT / 用户数据 / 大媒体）
  if (API_PREFIX.test(url.pathname)) return

  // 页面导航：NetworkFirst → 离线回退 index.html
  if (req.mode === 'navigate') {
    event.respondWith(
      fetch(req).catch(() =>
        caches.match('/index.html').then((r) => r || caches.match('/'))
      )
    )
    return
  }

  // 静态资源：StaleWhileRevalidate
  if (STATIC_RE.test(url.pathname) || SHELL.includes(url.pathname)) {
    event.respondWith(
      caches.open(ASSET_CACHE).then(async (cache) => {
        const cached = await cache.match(req)
        const network = fetch(req)
          .then((res) => {
            if (res && res.ok) cache.put(req, res.clone())
            return res
          })
          .catch(() => cached)
        return cached || network
      })
    )
  }
})

// Job000136：页面通过 postMessage({type:'SKIP_WAITING'}) 通知新 SW 立即接管；
// 接管后页面侧监听 controllerchange → reload，完成「新版本一键生效」。
self.addEventListener('message', (event) => {
  if (event.data && event.data.type === 'SKIP_WAITING') {
    self.skipWaiting()
  }
})
