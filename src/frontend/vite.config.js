import fs from 'node:fs'
import path from 'node:path'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Job000009 地图模式：maplibre-gl v6 的 GeoJSON worker 依赖 import.meta.url 相对路径，
// vite 打包后 worker 文件缺失 → GeoJSON 图层静默不渲染。
// 构建前把 worker 与 shared 模块复制到 public/（dist 根路径可被 SPA nginx 直接 serve），
// 配合 MapView.vue 中 setWorkerUrl('/maplibre-gl-worker.mjs') 使用。
function copyMaplibreWorker() {
  return {
    name: 'copy-maplibre-worker',
    buildStart() {
      const from = path.resolve('node_modules/maplibre-gl/dist')
      const to = path.resolve('public')
      fs.mkdirSync(to, { recursive: true })
      for (const f of ['maplibre-gl-worker.mjs', 'maplibre-gl-shared.mjs']) {
        fs.copyFileSync(path.join(from, f), path.join(to, f))
      }
    }
  }
}

export default defineConfig({
  plugins: [vue(), copyMaplibreWorker()],
  server: {
    port: 5173,
    // 前端默认同源相对路径调 API；dev 下把 API 路由代理到本机 8080 后端
    proxy: {
      '^/(auth|media|search|spaces|folders|transcode|albums|shares|public|admin|health|ready|metrics|geo|tiles)(/|$)': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
