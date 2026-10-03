// Package geocoord 坐标系转换（WGS-84 ↔ GCJ-02），**纯函数、零依赖**。
//
// 为什么从 internal/geo 独立出来（Job000143）：
//
//	internal/media 需要调用 GCJ02ToWGS84 做坐标收口，但 internal/geo 反向依赖
//	internal/media（geo → search → media），直接引用会形成
//	media → geo → search → media
//	的**导入循环**（Go 编译期直接拒绝，实测踩过一次）。
//
//	坐标转换是纯数学（克拉索夫斯基椭球 + 国测局偏移公式），本身不含任何
//	地图/存储/网络语义 —— 放进 geo 这样的业务包只是历史归置。拆成独立包后：
//	  - geo 保留薄封装（OutOfChina/GCJ02ToWGS84 等同名转发），既有调用方零改动；
//	  - media 等任何包都可直接依赖 geocoord，不会引入环。
//
// ⚠️ 转换只对**中国大陆境内**有效（OutOfChina 为真时两者等价，无需转换）。
//    这是国测局加密算法本身的适用范围，不是精度限制。
package geocoord

import "math"

const (
	earthA  = 6378245.0              // 克拉索夫斯基椭球长半轴
	earthEE = 0.00669342162296594323 // 偏心率平方
)

// OutOfChina 粗略国界判断（原型矩形框；正式版按 PRD「中外国界可配」接入精确国界数据）。
func OutOfChina(lng, lat float64) bool {
	return lng < 72.004 || lng > 137.8347 || lat < 0.8293 || lat > 55.8271
}

func transformLat(x, y float64) float64 {
	ret := -100.0 + 2.0*x + 3.0*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(y*math.Pi) + 40.0*math.Sin(y/3.0*math.Pi)) * 2.0 / 3.0
	ret += (160.0*math.Sin(y/12.0*math.Pi) + 320*math.Sin(y*math.Pi/30.0)) * 2.0 / 3.0
	return ret
}

func transformLng(x, y float64) float64 {
	ret := 300.0 + x + 2.0*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	ret += (20.0*math.Sin(6.0*x*math.Pi) + 20.0*math.Sin(2.0*x*math.Pi)) * 2.0 / 3.0
	ret += (20.0*math.Sin(x*math.Pi) + 40.0*math.Sin(x/3.0*math.Pi)) * 2.0 / 3.0
	ret += (150.0*math.Sin(x/12.0*math.Pi) + 300.0*math.Sin(x/30.0*math.Pi)) * 2.0 / 3.0
	return ret
}

// WGS84ToGCJ02 WGS-84 → GCJ-02（高德底图火星坐标系）。中国境外坐标原样返回。
func WGS84ToGCJ02(lng, lat float64) (float64, float64) {
	if OutOfChina(lng, lat) {
		return lng, lat
	}
	dLat := transformLat(lng-105.0, lat-35.0)
	dLng := transformLng(lng-105.0, lat-35.0)
	radLat := lat / 180.0 * math.Pi
	magic := math.Sin(radLat)
	magic = 1 - earthEE*magic*magic
	sqrtMagic := math.Sqrt(magic)
	dLat = (dLat * 180.0) / ((earthA * (1 - earthEE)) / (magic * sqrtMagic) * math.Pi)
	dLng = (dLng * 180.0) / (earthA / sqrtMagic * math.Cos(radLat) * math.Pi)
	return lng + dLng, lat + dLat
}

// GCJ02ToWGS84 粗略逆转换（迭代逼近，精度 ~1m，足够展示/搜索回显用）。
func GCJ02ToWGS84(lng, lat float64) (float64, float64) {
	if OutOfChina(lng, lat) {
		return lng, lat
	}
	gLng, gLat := WGS84ToGCJ02(lng, lat)
	return lng - (gLng - lng), lat - (gLat - lat)
}
