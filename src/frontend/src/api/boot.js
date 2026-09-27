// boot.js —— 启动状态探测（Job000131 T3-B BootGate）。
// 原生 fetch 不走 axios：不经 401 刷新逻辑、不被业务拦截器修饰，探测语义纯粹。
// /ready 无鉴权（liveness/readiness 家族），失败不缓存、成功缓存 10s（高频导航免重探）。
const READY_CACHE_MS = 10000
let readyCache = { ready: null, checks: null, expires: 0 }

export function invalidateBootCache() {
  readyCache = { ready: null, checks: null, expires: 0 }
}

// 返回 { ready: boolean, checks?: {postgres,valkey,disk} }；网络失败按未就绪处理（启动窗口语义）。
export async function probeReady() {
  if (readyCache.ready !== null && Date.now() < readyCache.expires) {
    return { ready: readyCache.ready, checks: readyCache.checks }
  }
  let ready = false
  let checks = null
  try {
    const res = await fetch('/ready', { cache: 'no-store' })
    ready = res.ok
    if (res.ok || res.status === 503) {
      try {
        const body = await res.json()
        checks = body.checks || null
      } catch { /* 形状异常：无分项 */ }
    }
  } catch {
    ready = false // 连接失败 = 服务未起/未就绪
  }
  readyCache = { ready, checks, expires: Date.now() + READY_CACHE_MS }
  return { ready, checks }
}
