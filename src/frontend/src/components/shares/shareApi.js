import http from '../../api/http'

// 分享模块（管理端，需登录）API 封装
// 契约：POST /shares → 201 {id, token, url}；GET /shares → 我的分享列表；DELETE /shares/:id 吊销

export async function createShare(payload) {
  const { data } = await http.post('/shares', payload)
  return data
}

export async function listShares() {
  const { data } = await http.get('/shares')
  if (Array.isArray(data)) return data
  return Array.isArray(data.shares) ? data.shares : []
}

export async function revokeShare(id) {
  await http.delete(`/shares/${id}`)
}

export function shareLink(token) {
  return `${window.location.origin}/share/${token}`
}

export function errMsg(e, fallback = '操作失败') {
  return e.response?.data?.error?.message || (e.response ? `HTTP ${e.response.status}` : '网络不可达') || fallback
}
