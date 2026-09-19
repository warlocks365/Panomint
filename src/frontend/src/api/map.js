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

// 媒体类型过滤：缺省与 'all' 等价（后端已核对），故 'all' 不下发，保持 URL 干净
function setKind(q, kind) {
  if (kind && kind !== 'all') q.set('kind', kind)
}

// fetchClusters bbox + zoom 网格聚合（时间轴 → 地图 过滤）
export function fetchClusters(bbox, zoom, from, to, kind) {
  const q = bboxQuery(bbox)
  q.set('zoom', String(Math.round(zoom)))
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  setKind(q, kind)
  return http.get(`/geo/clusters?${q.toString()}`).then((r) => r.data.clusters || [])
}

// fetchItems bbox 内媒体条目（点击簇展开）
export function fetchItems(bbox, from, to, limit = 60, kind) {
  const q = bboxQuery(bbox)
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  q.set('limit', String(limit))
  setKind(q, kind)
  return http.get(`/geo/items?${q.toString()}`).then((r) => r.data.items || [])
}

// fetchHistogram bbox 内时间分布（地图 viewport → 时间轴）
// granularity: year|month|day（Job000009 新增 day）；每桶含四类计数
export function fetchHistogram(bbox, granularity = 'month') {
  const q = bboxQuery(bbox)
  q.set('granularity', granularity)
  return http.get(`/geo/histogram?${q.toString()}`).then((r) => r.data.buckets || [])
}

// fetchPlaces bbox 内去重地名列表（底部地理位置罗列）
export function fetchPlaces(bbox, from, to, limit = 50, kind) {
  const q = bboxQuery(bbox)
  if (from) q.set('from', from)
  if (to) q.set('to', to)
  q.set('limit', String(limit))
  setKind(q, kind)
  return http.get(`/geo/places?${q.toString()}`).then((r) => r.data.places || [])
}

// searchPlaces 地名正向检索（GET /map/search）
// 后端已按 display provider（默认 amap）把候选归一化为 GCJ-02，与地图高德栅格底图同一坐标系，
// 返回值可直接喂 map.flyTo；q 为空由调用方拦截（后端返回 400 INVALID_PARAMS）。
export function searchPlaces(q) {
  return http.get('/map/search', { params: { q } }).then((r) => r.data.candidates || [])
}

// getMapIconPref 读取账户级地图图标偏好
export function getMapIconPref() {
  return http.get('/preferences/map').then((r) => r.data.pref || null)
}

// putMapIconPref 写入账户级地图图标偏好
export function putMapIconPref(pref) {
  return http.put('/preferences/map', pref).then((r) => r.data.pref || null)
}

// getUiPrefs 读取账户级地图界面偏好（布局 / 底图 / 默认缩放），后端未设置时返回默认值而非 404
export function getUiPrefs() {
  return http.get('/user/ui-prefs')
}

// putUiPrefs 写入账户级地图界面偏好（非法值后端返回 400 INVALID_PARAMS）
export function putUiPrefs(prefs) {
  return http.put('/user/ui-prefs', prefs)
}
