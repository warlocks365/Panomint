import http from './http'

// Job000070/000070-2 网络挂载管理 API（后端 internal/storage）。
// 凭据红线：密文永不出端点——列表只回 has_creds；写操作凭据进请求内存即被
// AES-256-GCM 加密，前端不缓存不落盘；更新掩码 ********=不改动（后端约定）。
export async function listMounts() {
  const { data } = await http.get('/storage/mounts')
  return data.mounts || []
}

export async function createMount(payload) {
  const { data } = await http.post('/storage/mounts', payload)
  return data
}

export async function patchMount(id, patch) {
  await http.patch(`/storage/mounts/${id}`, patch)
}

export async function deleteMount(id) {
  await http.delete(`/storage/mounts/${id}`)
}

export async function testMount(id) {
  const { data } = await http.post(`/storage/mounts/${id}/test`)
  return data
}
