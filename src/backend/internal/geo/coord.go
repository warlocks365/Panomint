// Package geo 地图模式核心：PostGIS 空间聚合 + 地图搜索 / 瓦片 / 配置端点。
//
// 坐标系转换（WGS-84 ↔ GCJ-02）已迁至 internal/geocoord（Job000143）：
// 它是纯数学函数，而本包**反向依赖** internal/media（geo → search → media），
// 坐标转换若留在这里，会让需要它的 media 包形成
// media → geo → search → media 的导入循环（Go 编译期直接拒绝，实测踩过）。
//
// 下方保留同名薄封装，既有调用方（tile.go / mapsearch.go 等）零改动。
// 设计依据：TDD v1.1 §3.7；约束：GCJ-02 仅应用层转换，不落库（DDL §2.5 注）。
package geo

import "panoalbum/internal/geocoord"

// 坐标系转换的转发封装（实现见 internal/geocoord）。保留别名是为了不动
// 本包内既有调用点；新代码请直接用 geocoord（少一层间接）。
var (
	// OutOfChina 粗略国界判断（境外坐标不做转换）。
	OutOfChina = geocoord.OutOfChina
	// WGS84ToGCJ02 正向加密（WGS-84 → GCJ-02），用于「按 WGS-84 坐标取高德瓦片」。
	WGS84ToGCJ02 = geocoord.WGS84ToGCJ02
	// GCJ02ToWGS84 逆向解密（GCJ-02 → WGS-84），用于「高德坐标落库前收口」。
	GCJ02ToWGS84 = geocoord.GCJ02ToWGS84
)
