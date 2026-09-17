import http from './http'

// 工具箱（PRD §6.16）用到的媒体接口：重复检测 + 软删。
// 命名导出 + `.then(r => r.data)` 与 api/map.js 保持一致。

// getDuplicates 拉取互为重复的分组（后端按 pHash 聚类，组内首位即建议保留者）。
// 候选宇宙超过后端 O(N²) 上限时返回 400 TOO_MANY_MEDIA，message 里带真实规模，
// 调用方必须自己处理该分支（不是网络故障，重试无用）。
export function getDuplicates({ threshold = 10, limit = 50 } = {}) {
  return http
    .get('/media/duplicates', { params: { threshold, limit } })
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
