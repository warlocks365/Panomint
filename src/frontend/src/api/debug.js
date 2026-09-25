// 转码远程调试通道 API（Job000121）——全部需 admin:system（服务端强校验；
// 前端卡片可见性与 GET /admin/debug/status 同一把锁：无权限者不渲染入口，不存在只读泄露）。
// 形状与 src/backend/internal/debug/admin.go 一一对应（契约 v1.14 §20）。
import http from './http'

// ---- 通道状态（admin:system）----
// GET /admin/debug/status →
//   未开启: {enabled:false, connected:false}
//   已开启: {enabled:true, connected, channel_id, created_at, expires_at,
//            last_connect_at?, last_connect_ip?}（无明钥——密钥只在 enable/rotate 响应出现一次）
export function getDebugStatus() {
  return http.get('/admin/debug/status').then((r) => r.data)
}

// ---- 开启（admin:system）----
// POST /admin/debug/enable ← {ttl_hours:1|8|24|72}（缺省 24）
// → 201 {url, key, expires_at}（key 唯一出口；已开启 → 409 DEBUG_ALREADY_ENABLED）
export function enableDebug(ttlHours) {
  return http.post('/admin/debug/enable', { ttl_hours: ttlHours }).then((r) => r.data)
}

// ---- 重置密钥（admin:system）----
// POST /admin/debug/rotate ← {ttl_hours?}（缺省沿用原档位 span）
// → 200 {url, key, expires_at}；旧连接被踢（4004）；未开启 → 409 DEBUG_NOT_ENABLED
export function rotateDebug(ttlHours) {
  const body = {}
  if (ttlHours !== undefined && ttlHours !== null) body.ttl_hours = ttlHours
  return http.post('/admin/debug/rotate', body).then((r) => r.data)
}

// ---- 关闭（admin:system）----
// POST /admin/debug/disable → 204（幂等：未开启也 204；活动连接被踢 4003）
export function disableDebug() {
  return http.post('/admin/debug/disable').then((r) => r.data)
}
