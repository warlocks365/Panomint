import http from './http'

// 工具箱（PRD §6.16）用到的媒体接口：重复检测 + 软删。
// 命名导出 + `.then(r => r.data)` 与 api/map.js 保持一致。

// getDuplicates 拉取互为重复的分组（后端按 pHash 聚类，组内首位即建议保留者）。
// 候选宇宙超过后端 O(N²) 上限时返回 400 TOO_MANY_MEDIA，message 里带真实规模，
// 调用方必须自己处理该分支（不是网络故障，重试无用）。
export function getDuplicates({ threshold = 10, limit = 50 } = {}) {
  return http
    // 后端 pHash O(N²) 聚类天然慢，显式取消 15s 全局默认超时
    .get('/media/duplicates', { params: { threshold, limit }, timeout: 0 })
    .then((r) => r.data)
}

// deleteMedia 逐条软删（后端没有批量删除接口），删除后入回收站仍可恢复
export function deleteMedia(id) {
  return http.delete(`/media/${id}`).then((r) => r.data)
}

// getRestoreHistory 本人执行过的「从回收站恢复」历史（审计行原样透传）。
// 时间列是 `at`（不是 created_at）；detail 里只有 path / owner_id，没有 filename。
// 后端只按 actor 过滤 ⇒ 看不到"管理员代我恢复"的记录（可见性缺口，非越权）。
export function getRestoreHistory({ limit = 50, cursor = '' } = {}) {
  const params = { limit }
  if (cursor) params.cursor = cursor
  return http.get('/media/restore-history', { params }).then((r) => r.data)
}

// updateMetadata 编辑媒体元数据（Job000143：拍摄时间 / 拍摄地 / 详细地址 / GPS）。
//
// 三态语义（与后端 media.MetadataUpdate 一致）：
//   - 字段不在 payload 里 → **不动**
//   - 值为 ''（或 lat/lng 为 0）→ **清空**该字段
//   - 其余 → 更新
// 故「只改拍摄地」时只传 { place }，不要把其它字段一起带上（带上空串会清空它们）。
//
// coordSource：坐标系来源标记。'gcj02' 表示坐标来自高德底图（地图选点 / 高德搜索候选），
// 后端会转成 WGS-84 再落库（库内 geometry(Point,4326) 是 WGS-84，**必须转换**，
// 否则地图页聚合点整体偏 300~500 米）。
// **不确定就传 'wgs84'**（或不传 = 默认 WGS-84）：把已经是 WGS-84 的坐标
// 再转一次是不可逆的精度损失；反过来顶多让用户手动纠偏。
export function updateMetadata(id, payload, { coordSource = 'wgs84' } = {}) {
  return http
    .patch(`/media/${id}`, payload, { headers: { 'X-Coord-Source': coordSource } })
    .then((r) => r.data)
}

// fetchMediaDetail 拉单个媒体详情（元数据编辑的初始值来源）。
export function fetchMediaDetail(id) {
  return http.get(`/media/${id}`).then((r) => r.data)
}

// setFavorite 收藏/取消收藏（Job000143：媒体卡右键菜单用）。
// 后端是 POST /media/:id/favorite { favorite: bool }，语义为**设为**该状态
// （不是 toggle），故调用方需自己算好 next。
export function setFavorite(id, favorite) {
  return http.post(`/media/${id}/favorite`, { favorite }).then((r) => r.data)
}
