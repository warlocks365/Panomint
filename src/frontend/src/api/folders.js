import http from './http'

// Job000069 目录管理 API。
export async function createFolder(path) {
  const { data } = await http.post('/folders', { path })
  return data
}

export async function renameFolder(from, to) {
  const { data } = await http.patch('/folders/rename', { from, to })
  return data
}

export async function deleteFolder(path) {
  const { data } = await http.delete('/folders', { params: { path } })
  return data
}

export async function setFolderGrants(path, grants) {
  await http.put('/folders/grants', { path, grants })
}
