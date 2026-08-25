import http from '../../api/http'

// 媒体图片加载器：
// - 缩略图优先走 GET /media/:id/thumb?size=sm|md|lg（image/webp，Cache-Control 86400）
// - 该媒体尚未生成缩略图（404）时回退 GET /media/:id/download 原文件
// - 注意：DB 的 thumbnail_sm/md/lg 字段是文件名而非 URL，不可直接拼接使用
// - 统一走 http 实例（自动注入 Bearer、401 刷新），结果以 ObjectURL 缓存
const cache = new Map() // key -> objectURL
const pending = new Map() // key -> Promise<objectURL>
const MAX_CACHE = 600

async function fetchBlobUrl(path) {
  const res = await http.get(path, { responseType: 'blob' })
  return URL.createObjectURL(res.data)
}

function remember(key, url) {
  if (cache.size >= MAX_CACHE) {
    const oldest = cache.keys().next().value
    URL.revokeObjectURL(cache.get(oldest))
    cache.delete(oldest)
  }
  cache.set(key, url)
  return url
}

function dedupe(key, loader) {
  if (cache.has(key)) return Promise.resolve(cache.get(key))
  if (pending.has(key)) return pending.get(key)
  const p = loader()
    .then((url) => remember(key, url))
    .finally(() => pending.delete(key))
  pending.set(key, p)
  return p
}

// 网格缩略图：size = sm | md | lg
export function loadThumbUrl(item, size = 'md') {
  const key = `${item.id}:${size}`
  return dedupe(key, async () => {
    try {
      return await fetchBlobUrl(`/media/${item.id}/thumb?size=${size}`)
    } catch (e) {
      // 缩略图未生成（404）等情况：回退原文件
      return fetchBlobUrl(`/media/${item.id}/download`)
    }
  })
}

// 查看器大图 / 视频源：直接取原文件
export function loadFullUrl(item) {
  const key = `${item.id}:full`
  return dedupe(key, () => fetchBlobUrl(`/media/${item.id}/download`))
}
