// 公开分享（免登）API 封装
// 关键约束：公开端点不得携带 Authorization 头 —— 这里统一使用裸 fetch / 直拼 URL，
// 不复用 src/api/http.js（其拦截器会自动注入 Bearer）。

// 同源相对路径（生产 nginx 反代 /public/* → api）；本地 dev 由 vite proxy 转发
const API_BASE = import.meta.env.VITE_API_BASE || ''

// 密码传递约定（与后端「头优先、query 兼容」对应）：
// - 普通 API 请求（fetch 可自定义头）→ X-Share-Password 请求头，不出现在 URL/日志里
// - 媒体流 URL（m3u8/ts 切片、TextureLoader 直贴图等无法带自定义头的场景）→ 保留 ?password= query
function withPassword(url, password) {
  if (!password) return url
  return url + (url.includes('?') ? '&' : '?') + 'password=' + encodeURIComponent(password)
}

function passwordHeaders(password) {
  return password ? { 'X-Share-Password': password } : {}
}

// GET /public/shares/:token（密码走 X-Share-Password 头）
// 成功 → {kind, title, items, require_password}
// 失败 → 抛 {status, code, message}；code ∈ WRONG_PASSWORD / EXPIRED / NOT_FOUND / MAX_VIEWS
export async function fetchPublicShare(token, password) {
  const res = await fetch(`${API_BASE}/public/shares/${token}`, {
    headers: passwordHeaders(password)
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    throw {
      status: res.status,
      code: body?.error?.code || '',
      message: body?.error?.message || ''
    }
  }
  return body
}

// 公开缩略图 URL（媒体流路径：供 TextureLoader 等无法带自定义头的场景直接用，密码保留 query）
export function publicThumbUrl(token, id, size = 'md', password = '') {
  return withPassword(`${API_BASE}/public/shares/${token}/media/${id}/thumb?size=${size}`, password)
}

// 公开 HLS 主播放列表 URL（hls.js / 原生 HLS 直接用，免 token）
// Job000139：HLS 缺失时的原片在线播放（inline 流，后端不受 allow_download 限制）
export function publicStreamUrl(token, id, password = '') {
  return withPassword(`/public/shares/${token}/media/${id}/stream`, password)
}

export function publicHlsUrl(token, id, password = '') {
  return withPassword(`${API_BASE}/public/shares/${token}/media/${id}/hls/master.m3u8`, password)
}

// Job000053：公开侧原文件下载（仅 allow_download=true 的分享可用，服务端 403 同形拦截）。
// fetch + X-Share-Password 头（密码不进 URL/日志）→ blob → ObjectURL 触发浏览器保存。
// 文件名优先取 Content-Disposition（filename* / filename），回落调用方传入名。
// 已知限制：整文件入内存；超大视频待流式方案。
export async function downloadPublicMedia(token, id, password = '', fallbackName = '') {
  const res = await fetch(`${API_BASE}/public/shares/${token}/media/${id}/download`, {
    headers: passwordHeaders(password)
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw { status: res.status, code: body?.error?.code || '', message: body?.error?.message || '' }
  }
  const blob = await res.blob()
  const cd = res.headers.get('Content-Disposition') || ''
  let name = fallbackName || id
  const star = cd.match(/filename\*\s*=\s*UTF-8''([^;]+)/i)
  const plain = cd.match(/filename\s*=\s*"?([^";]+)"?/i)
  if (star) name = decodeURIComponent(star[1])
  else if (plain) name = plain[1]
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = name
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 10000)
  return name
}

// 缩略图 blob → ObjectURL（带缓存与去重，参照 timeline/mediaLoader.js 模式）
const cache = new Map()
const pending = new Map()
const MAX_CACHE = 300

function remember(key, url) {
  if (cache.size >= MAX_CACHE) {
    const oldest = cache.keys().next().value
    URL.revokeObjectURL(cache.get(oldest))
    cache.delete(oldest)
  }
  cache.set(key, url)
  return url
}

export function loadPublicThumb(token, id, size = 'md', password = '') {
  const key = `${token}:${id}:${size}`
  if (cache.has(key)) return Promise.resolve(cache.get(key))
  if (pending.has(key)) return pending.get(key)
  // fetch 可自定义头 → 密码走 X-Share-Password，URL 不带 query
  const p = fetch(publicThumbUrl(token, id, size), { headers: passwordHeaders(password) })
    .then((res) => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.blob()
    })
    .then((blob) => remember(key, URL.createObjectURL(blob)))
    .finally(() => pending.delete(key))
  pending.set(key, p)
  return p
}

// 查看器大图（lg）：不进共享缓存，objectURL 由调用方持有并负责 revoke
// （若复用 loadPublicThumb 的缓存 URL，调用方 revoke 会使缓存持有死引用）
export async function loadPublicViewerUrl(token, id, password = '') {
  const res = await fetch(publicThumbUrl(token, id, 'lg'), { headers: passwordHeaders(password) })
  if (!res.ok) throw new Error(`HTTP ${res.status}`)
  return URL.createObjectURL(await res.blob())
}

/* ---------------- Phase 4 P1：分享带宽自测（360 播放页 ABR 初档依据） ---------------- */

// 探针尺寸：下行 256KiB / 上行 128KiB —— 足够测出量级，又不会在移动网络上白烧流量。
const BW_DOWN_BYTES = 256 * 1024
const BW_UP_BYTES = 128 * 1024
// 与后端 maxReportedKbps 一致：防前端计时异常产出离谱数值
const BW_MAX_KBPS = 5000000

function clampKbps(v) {
  if (!Number.isFinite(v) || v <= 0) return 0
  return Math.min(BW_MAX_KBPS, Math.round(v))
}

function randomBytes(n) {
  const b = new Uint8Array(n)
  for (let off = 0; off < n; off += 65536) {
    // crypto.getRandomValues 单次上限 65536 字节
    crypto.getRandomValues(b.subarray(off, Math.min(off + 65536, n)))
  }
  return b
}

// 探针端点 URL（下行 GET / 上行 POST 同一路径，token 鉴权 + 密码约束）
export function publicProbeUrl(token, bytes, password = '') {
  return withPassword(`${API_BASE}/public/shares/${token}/bandwidth-probe?bytes=${bytes}`, password)
}

// measureShareBandwidth 分享页带宽自测（3 个请求，与后端 internal/shares/bandwidth.go 对应）：
//   1. GET  探针：服务端连写 N 字节随机数据，浏览器读完计时 → 下行 + TTFB 延迟
//   2. POST 探针：浏览器上传 M 字节，服务端读请求体计时 → 上行
//   3. POST /bandwidth-test（无 body）：服务端读探针缓存、落库并回 {up_kbps,down_kbps,latency_ms}
//
// 任何一步失败都抛错，调用方**必须回落 hls.js 默认 ABR**，不得阻塞播放。
// 上行探针失败不致命（ABR 只依赖下行），但下行探针失败即视为测速失败。
export async function measureShareBandwidth(token, password = '') {
  const headers = passwordHeaders(password) // 探针为普通 API 请求：密码走头不走 query
  // 1) 下行：边收边读，TTFB ≈ RTT（后端返回 X-Accel-Buffering: no，故 nginx 不会缓冲掉时间信息）
  const t0 = performance.now()
  const dl = await fetch(publicProbeUrl(token, BW_DOWN_BYTES), { cache: 'no-store', headers })
  if (!dl.ok) throw new Error(`bandwidth probe HTTP ${dl.status}`)

  let received = 0
  let firstByteMs = 0
  if (dl.body && typeof dl.body.getReader === 'function') {
    const reader = dl.body.getReader()
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      if (!firstByteMs) firstByteMs = performance.now() - t0
      received += value.byteLength
    }
  } else {
    // 极老浏览器无流式 body：退化为整体计时，延迟按 0 处理
    received = (await dl.arrayBuffer()).byteLength
  }
  const elapsedMs = Math.max(1, performance.now() - t0)
  const downKbps = clampKbps((received * 8) / (elapsedMs / 1000) / 1000)
  const latencyMs = Math.round(firstByteMs)
  if (downKbps <= 0) throw new Error('bandwidth probe 无有效载荷')

  // 2) 上行：失败只丢上行值，不影响选档
  let upKbps = 0
  try {
    const up = await fetch(publicProbeUrl(token, BW_UP_BYTES), {
      method: 'POST',
      body: randomBytes(BW_UP_BYTES),
      cache: 'no-store',
      headers
    })
    if (up.ok) upKbps = clampKbps(Number((await up.json())?.up_kbps) || 0)
  } catch {
    upKbps = 0
  }

  const local = { up_kbps: upKbps, down_kbps: downKbps, latency_ms: latencyMs }

  // 3) 汇总落库。写库失败也要把已测到的带宽交回调用方（否则白白浪费一次测速）
  try {
    const url = `${API_BASE}/public/shares/${token}/bandwidth-test?down_kbps=${downKbps}&latency_ms=${latencyMs}`
    const res = await fetch(url, { method: 'POST', cache: 'no-store', headers })
    if (res.ok) return await res.json()
  } catch {
    /* 回落本地测量值 */
  }
  return local
}
