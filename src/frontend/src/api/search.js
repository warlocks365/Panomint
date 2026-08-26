import http from './http'

// Stage 3 搜索接口（GET /search，Bearer 鉴权由 http 实例自动注入）
// params: { q, tag, date_after, date_before, place, type, favorites, cursor, limit }
// 响应: { items, next_cursor, total, place_fallback? }
export function searchMedia(params) {
  return http.get('/search', { params })
}
