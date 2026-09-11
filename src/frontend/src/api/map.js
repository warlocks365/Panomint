import http from './http'

// 地图模式 API（Job000009）
// 后端坐标系：请求 bbox 一律 WGS-84；响应坐标按 provider=amap 返回 GCJ-02（与高德底图对齐）。
// 前端拿到的 clusters/items 坐标可直接喂给 MapLibre（底图为高德栅格瓦片）。

function bboxQuery({ minLng, minLat, maxLng, maxLat }) {
  const q = new URLSearchParams({
    min_lng: String(minLng),
    min_lat: String(minLat),
    max_lng: String(maxLng),
    max_lat: String(maxLat)
  })
  return q
}

// fetchClusters bbox + zoom 网格聚合（时间轴 → 地图 过滤）
export function fetchClusters(bbox, zoom, from, to) {
  const q = bboxQuery(bbox)
  q.set('zoom', String(Math.round(zoom)))
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  return http.get(`/geo/clusters?${q.toString()}`).then((r) => r.data.clusters || [])
}

// fetchItems bbox 内媒体条目（点击簇展开）
export function fetchItems(bbox, from, to, limit = 60) {
  const q = bboxQuery(bbox)
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  q.set('limit', String(limit))
  return http.get(`/geo/items?${q.toString()}`).then((r) => r.data.items || [])
}

// fetchHistogram bbox 内时间分布（地图 viewport → 时间轴）
// granularity: year|month|day（Job000009 新增 day）；每桶含四类计数
export function fetchHistogram(bbox, granularity = 'month') {
  const q = bboxQuery(bbox)
  q.set('granularity', granularity)
  return http.get(`/geo/histogram?${q.toString()}`).then((r) => r.data.buckets || [])
}

// getMapIconPref 读取账户级地图图标偏好
export function getMapIconPref() {
  return http.get('/preferences/map').then((r) => r.data.pref || null)
}

// putMapIconPref 写入账户级地图图标偏好
export function putMapIconPref(pref) {
  return http.put('/preferences/map', pref).then((r) => r.data.pref || null)
}

// thumbBlobUrl 缩略图需 Bearer 鉴权，img src 无法带 header → 取 blob 后转本地 URL
export async function thumbBlobUrl(id) {
  const r = await http.get(`/media/${id}/thumb?size=sm`, { responseType: 'blob' })
  return URL.createObjectURL(r.data)
}
