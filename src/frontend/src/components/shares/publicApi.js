// 公开分享（免登）API 封装
// 关键约束：公开端点不得携带 Authorization 头 —— 这里统一使用裸 fetch / 直拼 URL，
// 不复用 src/api/http.js（其拦截器会自动注入 Bearer）。

const API_BASE = 'http://localhost:8080'

function withPassword(url, password) {
  if (!password) return url
  return url + (url.includes('?') ? '&' : '?') + 'password=' + encodeURIComponent(password)
}

// GET /public/shares/:token?password=
// 成功 → {kind, title, items, require_password}
// 失败 → 抛 {status, code, message}；code ∈ WRONG_PASSWORD / EXPIRED / NOT_FOUND / MAX_VIEWS
export async function fetchPublicShare(token, password) {
  const res = await fetch(withPassword(`${API_BASE}/public/shares/${token}`, password))
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

// 公开缩略图 URL（blob 加载用）
export function publicThumbUrl(token, id, size = 'md', password = '') {
  return withPassword(`${API_BASE}/public/shares/${token}/media/${id}/thumb?size=${size}`, password)
}

// 公开 HLS 主播放列表 URL（hls.js / 原生 HLS 直接用，免 token）
export function publicHlsUrl(token, id, password = '') {
  return withPassword(`${API_BASE}/public/shares/${token}/media/${id}/hls/master.m3u8`, password)
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
  const p = fetch(publicThumbUrl(token, id, size, password))
    .then((res) => {
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      return res.blob()
    })
    .then((blob) => remember(key, URL.createObjectURL(blob)))
    .finally(() => pending.delete(key))
  pending.set(key, p)
  return p
}
