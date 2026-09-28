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

// Job000133：扫描任务控制——action ∈ pause|resume|cancel。
export function controlJob(id, action) {
  return http.post(`/admin/jobs/${id}/${action}`).then((r) => r.data)
}

// GET /admin/jobs/:id → Job
export function getJob(id) {
  return http.get(`/admin/jobs/${id}`).then((r) => r.data)
}

// ---- 扫描导入（admin:system，Job000113 / R1-b）----
// POST /admin/scan ← {dir}（相对 MEDIA_ROOT；空=整个媒体根）→ 202 {job_id,status,root,dir}
// 异步执行：先建 index_jobs 行立即受理，后台 goroutine 扫；进度/终态用上方 getJob(job_id) 轮询。
// 入参越界（路径穿越）/目录不存在 → 400；已有扫描在跑 → 409 SCAN_RUNNING。
export function scanImport(dir = '') {
  return http.post('/admin/scan', { dir }).then((r) => r.data)
}

// ---- 目录树浏览（admin:system，Job000117）----
// GET /admin/fs/tree?dir=<相对 MEDIA_ROOT> → {root,dir,unreadable,items:[{name,rel,readable}]}
// 懒加载单层：只返回目标目录的直接子目录；unreadable=true = 目标目录本身无权限（不报错）；
// 子目录 readable=false = 锁定态（前端展示锁定图标、禁止展开）。
export function listDirTree(dir = '') {
  return http.get('/admin/fs/tree', { params: { dir } }).then((r) => r.data)
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

// ---- 转码设置（admin:system，Job000120-r2：系统级自动 HLS 转码开关；
// Job000124 增 realtime_transcode「播放时自动转码」；
// Job000125 增 HLS 三字段：hls_seg_seconds / hls_cache_profile / stream_base_url，
// 全部部分更新语义：undefined = 不改）----
// GET /admin/transcode-config → {auto_transcode, realtime_transcode, hls_seg_seconds,
//   hls_cache_profile, stream_base_url, updated_at?}（无配置行时缺省=开/关/4s/balanced/空）
export function getTranscodeConfig() {
  return http.get('/admin/transcode-config').then((r) => r.data)
}

// PUT /admin/transcode-config ← {auto_transcode?, realtime_transcode?, hls_seg_seconds?,
//   hls_cache_profile?, stream_base_url?}（至少一项）→ 同 GET 视图（写审计）
// 兼容旧签名（两个布尔位传参）；新代码建议传单个对象。
export function putTranscodeConfig(a, b, c, d, e) {
  const body = {}
  if (a !== null && typeof a === 'object') {
    Object.assign(body, a)
  } else {
    if (a !== undefined) body.auto_transcode = a
    if (b !== undefined) body.realtime_transcode = b
    if (c !== undefined) body.hls_seg_seconds = c
    if (d !== undefined) body.hls_cache_profile = d
    if (e !== undefined) body.stream_base_url = e
  }
  return http.put('/admin/transcode-config', body).then((r) => r.data)
}

// ---- HTTPS/证书设置（admin:system，Job000125）----
// GET /admin/https/config → {force_https, cert_path, cert_key_path, cert_not_after?,
//   updated_at?, cert_dir, request_proto}
export function getHttpsConfig() {
  return http.get('/admin/https/config').then((r) => r.data)
}

// PUT /admin/https/config ← {force_https?, http_port?, https_port?}（缺失不改，Job000128 点 9 扩展端口）
// force_https 保存即热生效（应用层 301 中间件，60s 缓存内收敛）；
// https_port 非 443 时 301 目标携带 `:port`（同样热生效）。
export function putHttpsConfig(forceHTTPS, httpPort, httpsPort) {
  const body = {}
  if (forceHTTPS !== undefined) body.force_https = forceHTTPS
  if (httpPort !== undefined && httpPort !== null) body.http_port = Number(httpPort)
  if (httpsPort !== undefined && httpsPort !== null) body.https_port = Number(httpsPort)
  return http.put('/admin/https/config', body).then((r) => r.data)
}

// POST /admin/https/cert ← multipart {cert: fullchain PEM, key: 私钥 PEM}
// → {config, apply_hint}。服务端校验配对 + 解析到期日 + 落盘 HTTPS_CERT_DIR；
//   **不**自动让反代生效（响应带 apply_hint 指引手动重启 caddy）。
export function uploadHttpsCert(certFile, keyFile) {
  const fd = new FormData()
  fd.append('cert', certFile)
  fd.append('key', keyFile)
  return http.post('/admin/https/cert', fd, {
    headers: { 'Content-Type': 'multipart/form-data' },
    timeout: 60000
  }).then((r) => r.data)
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

// ---- 扫描根目录分配（admin:users，Job000123）----
// PUT /admin/users/:id/scan-root ← {scan_root} → {user}
//   scan_root: 'photos/x' = 分配该子目录；'' = 整个媒体根；null = 取消分配。
//   服务端校验：路径必须位于 MEDIA_ROOT 内（穿越 400）、必须是真实存在的物理目录、
//   不能落在 _imports 保留区（任意层级）。
export function setUserScanRoot(userID, scanRoot) {
  return http.put(`/admin/users/${userID}/scan-root`, { scan_root: scanRoot }).then((r) => r.data)
}

// ---- 账号策略配置（admin:users，Job000128 点 6/7/8）----
// GET /admin/account-config → {policy:{allow_registration, invite_required, pwd_min_length,
//   pwd_require_upper, pwd_require_lower, pwd_require_digit, pwd_require_special,
//   login_max_attempts, login_lock_minutes}}
export function getAccountConfig() {
  return http.get('/admin/account-config').then((r) => r.data)
}

// PUT /admin/account-config ← 全量 policy（全量覆盖式 PUT；范围校验在服务端）
export function putAccountConfig(policy) {
  return http.put('/admin/account-config', policy).then((r) => r.data)
}

// ---- 邀请码管理（admin:users，Job000128 点 6）----
// GET /admin/invites → {invites:[{id, code, creator_email, expires_at, used_by?, used_by_email?, used_at?, created_at}]}
export function listInvites() {
  return http.get('/admin/invites').then((r) => r.data)
}

// POST /admin/invites ← {expires_in_hours?}（默认 72，上限 720）→ {invite}
export function createInvite(expiresInHours) {
  const body = expiresInHours ? { expires_in_hours: expiresInHours } : {}
  return http.post('/admin/invites', body).then((r) => r.data)
}

// ---- 自助注册（公开端点，Job000128 点 6）----
// GET /auth/register/status → {allow_registration, invite_required}
//   （公开：注册页在未登录时也要能查询；读库失败服务端按"关闭"返回）
export function getRegisterStatus() {
  return http.get('/auth/register/status', { skipAuthRefresh: true }).then((r) => r.data)
}

// POST /auth/register ← {email, display_name?, password, invite_code?} → {id}
export function register(email, displayName, password, inviteCode) {
  const body = { email, password }
  if (displayName) body.display_name = displayName
  if (inviteCode) body.invite_code = inviteCode
  return http.post('/auth/register', body, { skipAuthRefresh: true }).then((r) => r.data)
}
