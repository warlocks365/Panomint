import http from './http'

// 首次安装引导（Job000107）API。
// statusPromise 模块级缓存：路由守卫每次导航都问初始化状态，不能让每个导航都发请求；
// 成功完成初始化（runSetup 201）后必须 invalidateSetupStatus()，否则缓存的旧状态
// 会把用户永远困在向导里。

let statusPromise = null

export function getSetupStatus() {
  if (!statusPromise) {
    statusPromise = http.get('/setup/status').then((r) => r.data)
    // 失败不缓存：下次导航重试（查询失败时守卫选择放行，由 SetupView 容错提示）
    statusPromise.catch(() => {
      statusPromise = null
    })
  }
  return statusPromise
}

export function invalidateSetupStatus() {
  statusPromise = null
}

export function runSetup(payload) {
  return http.post('/setup', payload).then((r) => r.data)
}
