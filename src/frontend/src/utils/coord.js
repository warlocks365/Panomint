// WGS-84 ↔ GCJ-02 坐标转换（前端侧）。
//
// 为什么前端也需要一份：地图选点时，高德底图用的是 GCJ-02，
// 而媒体详情里的 gps 是 WGS-84 —— 若直接把 WGS-84 的经纬度喂给
// 高德底图，标记会落在实际位置以西约 500 米处。
//
// 与后端 internal/geocoord 的关系：**同一套公式的两份实现**
// （Go 与 JS 无法共享代码）。公式本身是公开的国测局偏移算法，
// 确定性，无需在线服务。
//
// 精度说明：GCJ02ToWGS84 是**近似逆**（用一次正向偏移的差值反算），
// 实测往返误差最大约 1.7 米。前端不做逆向转换（只在「显示 WGS-84 坐标」时
// 正向转换），所以不承担这个误差；真正的逆向收口在服务端做。

// 克拉索夫斯基椭球参数
const A = 6378245.0
const EE = 0.00669342162296594323

function transformLat(x, y) {
  let ret = -100.0 + 2.0 * x + 3.0 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * Math.PI) + 20.0 * Math.sin(2.0 * x * Math.PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(y * Math.PI) + 40.0 * Math.sin((y / 3.0) * Math.PI)) * 2.0) / 3.0
  ret += ((160.0 * Math.sin((y / 12.0) * Math.PI) + 320 * Math.sin((y * Math.PI) / 30.0)) * 2.0) / 3.0
  return ret
}

function transformLng(x, y) {
  let ret = 300.0 + x + 2.0 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  ret += ((20.0 * Math.sin(6.0 * x * Math.PI) + 20.0 * Math.sin(2.0 * x * Math.PI)) * 2.0) / 3.0
  ret += ((20.0 * Math.sin(x * Math.PI) + 40.0 * Math.sin((x / 3.0) * Math.PI)) * 2.0) / 3.0
  ret += ((150.0 * Math.sin((x / 12.0) * Math.PI) + 300.0 * Math.sin((x / 30.0) * Math.PI)) * 2.0) / 3.0
  return ret
}

// outOfChina 粗略国界框判断（境外不做偏移 —— 这是算法本身的适用范围，不是精度取舍）。
export function outOfChina(lng, lat) {
  return lng < 72.004 || lng > 137.8347 || lat < 0.8293 || lat > 55.8271
}

// wgs84ToGcj02 WGS-84 → GCJ-02。用于「把 WGS-84 坐标显示在高德底图上」。
export function wgs84ToGcj02(lng, lat) {
  if (outOfChina(lng, lat)) return [lng, lat]
  let dLat = transformLat(lng - 105.0, lat - 35.0)
  let dLng = transformLng(lng - 105.0, lat - 35.0)
  const radLat = (lat / 180.0) * Math.PI
  let magic = Math.sin(radLat)
  magic = 1 - EE * magic * magic
  const sqrtMagic = Math.sqrt(magic)
  dLat = (dLat * 180.0) / (((A * (1 - EE)) / (magic * sqrtMagic)) * Math.PI)
  dLng = (dLng * 180.0) / ((A / sqrtMagic) * Math.cos(radLat) * Math.PI)
  return [lng + dLng, lat + dLat]
}

// gcj02ToWgs84 GCJ-02 → WGS-84（近似逆）。
// ⚠️ 前端**不调用**它：逆向收口统一在服务端做，避免两处实现产生精度差异。
//    保留导出仅为单元测试与调试对照使用。
export function gcj02ToWgs84(lng, lat) {
  if (outOfChina(lng, lat)) return [lng, lat]
  const [gLng, gLat] = wgs84ToGcj02(lng, lat)
  return [lng * 2 - gLng, lat * 2 - gLat]
}
