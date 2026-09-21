// 管理后台 API（Job000052）——全部需登录，服务端按 admin:users / admin:system 两级鉴权。
// 形状与 src/backend/internal/{auth,audit,geo} 的 handler 一一对应（契约 v1.2 §2/§7/§12/§14）。
import http from './http'

// ---- 系统概览（admin:system）----
// GET /admin/stats → {media_total, users, storage_used, index_status:{state,running,last_job}}
export function getStats() {
  return http.get('/admin/stats').then((r) => r.data)
}

// ---- 用户（admin:users）----
// GET /admin/users → {users:[{id,email,display_name,role,status,mfa_enabled,mfa_pending}], total}
export function listUsers() {
  return http.get('/admin/users').then((r) => r.data)
}

// POST /admin/users ← {email, display_name?, password, role}（role 传角色名）
export function createUser(payload) {
  return http.post('/admin/users', payload).then((r) => r.data)
}

// PATCH /admin/users/:id ← {display_name?, role?, status?, password?}（部分更新）
export function updateUser(id, patch) {
  return http.patch(`/admin/users/${id}`, patch).then((r) => r.data)
}

// DELETE /admin/users/:id?disable=1 → {mode:'disable'|'purge'}
export function deleteUser(id, { disable = true } = {}) {
  return http
    .delete(`/admin/users/${id}`, { params: disable ? { disable: '1' } : {} })
    .then((r) => r.data)
}

// ---- 角色（admin:users）----
// GET /admin/roles → {roles:[{name,description,permissions,users}], total}
export function listRoles() {
  return http.get('/admin/roles').then((r) => r.data)
}

// POST /admin/roles ← {name, description?, permissions[]}（服务端有提权守卫）
export function createRole(payload) {
  return http.post('/admin/roles', payload).then((r) => r.data)
}

// ---- 审计（admin:users）----
// GET /admin/audit → {items:[Entry], total, limit, next_cursor?}
// 查询参数：action / actor / target_type / target_id / from / to / limit / cursor
export function listAudit(params = {}) {
  return http.get('/admin/audit', { params }).then((r) => r.data)
}

// ---- 任务（admin:system）----
// GET /admin/jobs?type=all|index|transcode&status=&limit= → {items:[Job], limit}
export function listJobs(params = {}) {
  return http.get('/admin/jobs', { params }).then((r) => r.data)
}

// GET /admin/jobs/:id → Job
export function getJob(id) {
  return http.get(`/admin/jobs/${id}`).then((r) => r.data)
}

// ---- 地图配置（admin:system）----
// GET /admin/map-config → {china_provider,china_tile_url,intl_provider,intl_tile_url,
//                          china_api_key_state,china_api_key_source,updated_at}
export function getMapConfig() {
  return http.get('/admin/map-config').then((r) => r.data)
}

// PUT /admin/map-config ← {china_provider?,china_tile_url?,intl_provider?,intl_tile_url?}
// ⚠️ 不接受 china_api_key（服务端显式 400，须走 AMAP_KEY 环境变量）
export function putMapConfig(patch) {
  return http.put('/admin/map-config', patch).then((r) => r.data)
}

// ---- 权限体系增强（Job000067 / F1）----
// GET /admin/perms → {perms:[{perm,label,category,description,orphan}]}
export function listPermMeta() {
  return http.get('/admin/perms').then((r) => r.data)
}

// PUT /admin/perms/:perm ← {label, category, description}（自定义中文名/分类）
export function putPermMeta(perm, payload) {
  return http.put('/admin/perms/' + encodeURIComponent(perm), payload).then((r) => r.data)
}

// GET /admin/users/:id/perms → {user_id, perms[]}
export function listUserPerms(userID) {
  return http.get('/admin/users/' + userID + '/perms').then((r) => r.data)
}

// PUT /admin/users/:id/perms ← {perm, granted}（增量授予/收回，服务端 PermCovered 守卫）
export function putUserPerm(userID, perm, granted) {
  return http.put('/admin/users/' + userID + '/perms', { perm, granted }).then((r) => r.data)
}
