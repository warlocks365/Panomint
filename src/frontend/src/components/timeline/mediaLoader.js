import http from '../../api/http'

// 媒体图片加载器：
// - 缩略图优先走 GET /media/:id/thumb?size=sm|md|lg（image/webp，Cache-Control 86400）
// - 该媒体尚未生成缩略图（404）时回退 GET /media/:id/download 原文件；
//   其余错误（500/网络中断/429 限流）不回退，直接向上抛出由调用方显示占位，
//   避免一次限流换来几十 MB 原文件下载形成故障正反馈
// - 注意：DB 的 thumbnail_sm/md/lg 字段是文件名而非 URL，不可直接拼接使用
// - 统一走 http 实例（自动注入 Bearer、401 刷新），结果以 ObjectURL 缓存
const cache = new Map() // 缩略图 key -> objectURL
const fullCache = new Map() // 查看器大图/视频（:full）单独小容量池，不挤占缩略图配额
const pending = new Map() // key -> Promise<objectURL>
const MAX_CACHE = 600
const MAX_FULL_CACHE = 4 // 原文件可达数百 MB，只保留最近几条

async function fetchBlobUrl(path, config = {}) {
  const res = await http.get(path, { responseType: 'blob', ...config })
  return URL.createObjectURL(res.data)
}

function remember(map, max, key, url) {
  if (map.size >= max) {
    const oldest = map.keys().next().value
    URL.revokeObjectURL(map.get(oldest))
    map.delete(oldest)
  }
  map.set(key, url)
  return url
}

function dedupe(map, max, key, loader) {
  if (map.has(key)) return Promise.resolve(map.get(key))
  if (pending.has(key)) return pending.get(key)
  const p = loader()
    .then((url) => remember(map, max, key, url))
    .finally(() => pending.delete(key))
  pending.set(key, p)
  return p
}

// 网格缩略图：size = sm | md | lg
export function loadThumbUrl(item, size = 'md') {
  const key = `${item.id}:${size}`
  return dedupe(cache, MAX_CACHE, key, async () => {
    try {
      return await fetchBlobUrl(`/media/${item.id}/thumb?size=${size}`)
    } catch (e) {
      // 仅缩略图未生成（404）回退原文件；500/网络/429 向上抛
      if (e.response?.status === 404) {
        return fetchBlobUrl(`/media/${item.id}/download`)
      }
      throw e
    }
  })
}

// 查看器大图 / 视频源：直接取原文件。
// 显式 timeout:0 —— 原文件可达数百 MB，慢网下 15s 默认超时必然 abort
export function loadFullUrl(item) {
  const key = `${item.id}:full`
  return dedupe(fullCache, MAX_FULL_CACHE, key, () =>
    fetchBlobUrl(`/media/${item.id}/download`, { timeout: 0 })
  )
}
