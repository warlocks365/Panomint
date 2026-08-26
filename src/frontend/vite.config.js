import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    // 前端默认同源相对路径调 API；dev 下把 API 路由代理到本机 8080 后端
    proxy: {
      '^/(auth|media|search|spaces|folders|transcode|albums|shares|public|admin|health|ready|metrics)(/|$)': {
        target: 'http://localhost:8080',
        changeOrigin: true
      }
    }
  }
})
