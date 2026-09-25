// 成员扫描导入 API（Job000123）——普通账号在管理员分配 scan_root 后使用。
// 全部需登录；权限 media:write（POST /scan）/ media:read（其余）。
// 形状与 src/backend/internal/index/member_scan.go 一一对应（契约 v1.12 §成员扫描）。
import http from './http'

// GET /user/scan-root → {assigned, scan_root}
//   assigned=false（scan_root=null）= 未分配：成员侧扫描入口一律 403，前端直接展示引导；
//   scan_root='' = 已分配、根目录为整个媒体根；其余 = 相对媒体根的子目录。
export function getMyScanRoot() {
  return http.get('/user/scan-root').then((r) => r.data)
}

// POST /scan ← {dir}（相对自己的扫描根；空=整个扫描根）→ 202 {job_id,status,root,dir}
// 越界（路径穿越）/命中系统保留区/目录不存在 → 400；未分配 → 403 SCAN_ROOT_REQUIRED；
// 已有扫描在跑 → 409 SCAN_RUNNING（与管理端共用互斥）。
export function scanMine(dir = '') {
  return http.post('/scan', { dir }).then((r) => r.data)
}

// GET /fs/tree?dir=<相对扫描根> → {root,dir,unreadable,items:[{name,rel,readable}]}
// 与管理端目录树同构（懒加载单层、无权限=锁定态）；差别：边界=扫描根、_imports 不下发。
export function listMyDirTree(dir = '') {
  return http.get('/fs/tree', { params: { dir } }).then((r) => r.data)
}

// GET /jobs/:id → {id,kind,status,total,processed,progress,started_at,finished_at,created_at}
// 只返回**自己触发**的扫描任务；不属于自己的 id → 404（与不存在同形，无权不可见）。
export function getMyJob(id) {
  return http.get(`/jobs/${id}`).then((r) => r.data)
}
