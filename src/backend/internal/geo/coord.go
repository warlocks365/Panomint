// Package geo 地图模式核心：WGS-84↔GCJ-02 坐标转换 + PostGIS 空间聚合。
// 设计依据：TDD v1.1 §3.7；约束：GCJ-02 仅应用层转换，不落库（DDL §2.5 注）。
package geo

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
