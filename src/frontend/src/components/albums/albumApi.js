import http from '../../api/http'

// 相册模块 API 封装（契约见 Stage 1 任务文本，字段缺失时以实际响应为准做兼容）

export async function listAlbums() {
  const { data } = await http.get('/albums')
  return Array.isArray(data.albums) ? data.albums : []
}

export async function createAlbum(payload) {
  const { data } = await http.post('/albums', payload)
  return data
}

export async function getAlbum(id) {
  const { data } = await http.get(`/albums/${id}`)
  return data
}

export async function updateAlbum(id, payload) {
  const { data } = await http.patch(`/albums/${id}`, payload)
  return data
}

export async function deleteAlbum(id) {
  await http.delete(`/albums/${id}`)
}

export async function addAlbumItems(id, mediaIds) {
  const { data } = await http.post(`/albums/${id}/items`, { media_ids: mediaIds })
  return data
}

export async function removeAlbumItem(id, mediaId) {
  await http.delete(`/albums/${id}/items/${mediaId}`)
}

export async function listComments(id) {
  const { data } = await http.get(`/albums/${id}/comments`)
  return Array.isArray(data.comments) ? data.comments : []
}

export async function createComment(id, content, parentId) {
  const payload = { content }
  if (parentId) payload.parent_id = parentId
  const { data } = await http.post(`/albums/${id}/comments`, payload)
  return data
}

export async function deleteComment(id, commentId) {
  await http.delete(`/albums/${id}/comments/${commentId}`)
}

// 从表单状态构造创建/更新 payload；criteria 中空字段剔除
export function buildCriteria(c) {
  const out = {}
  if (c.type) out.type = c.type
  if (c.date_from) out.date_from = c.date_from
  if (c.date_to) out.date_to = c.date_to
  if (c.place && c.place.trim()) out.place = c.place.trim()
  if (c.favorites) out.favorites = true
  if (c.folder_paths?.length) out.folder_paths = [...c.folder_paths]
  if (c.tag_ids?.length) out.tag_ids = [...c.tag_ids]
  if (c.person_ids?.length) out.person_ids = [...c.person_ids]
  return out
}

export function errMsg(e, fallback = '操作失败') {
  return e.response?.data?.error?.message || (e.response ? `HTTP ${e.response.status}` : '网络不可达') || fallback
}
