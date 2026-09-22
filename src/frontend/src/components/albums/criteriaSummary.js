// 智能相册条件的中文摘要（Job000068）。
// 目录/标签/人物以"名称 ×N"数量形态呈现——详情页不解析名称（需另拉三个列表），
// 名称映射留给编辑对话框的 CriteriaDimensions。
export function summarizeCriteria(c = {}) {
  const parts = []
  const typeMap = { photo: '照片', video: '视频', '360': '360' }
  parts.push(c.type ? `类型：${typeMap[c.type] || c.type}` : '全部类型')
  if (c.date_from || c.date_to) {
    parts.push(`日期：${c.date_from || '最早'} ~ ${c.date_to || '至今'}`)
  }
  if (c.place) parts.push(`地点：${c.place}`)
  if (c.favorites) parts.push('仅收藏')
  for (const [k, label] of [
    ['folder_paths', '目录'],
    ['tag_ids', '标签'],
    ['person_ids', '人物']
  ]) {
    const n = Array.isArray(c[k]) ? c[k].length : 0
    if (n) parts.push(`${label} ×${n}`)
  }
  return parts.join(' · ')
}
