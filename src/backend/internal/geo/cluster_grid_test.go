package geo

import "testing"

// GridSize 的测试归本包（函数在 cluster.go，与坐标转换无关）。
// 曾随 coord_test.go 一起被误搬到 internal/geocoord —— 那次搬迁只该带走
// 坐标转换的测试；本文件是对那次误搬的纠正锚点。
// TestGridSize 网格尺寸随 zoom 减半。
func TestGridSize(t *testing.T) {
	if GridSize(0) != 180.0 {
		t.Fatalf("z0 网格 = %v", GridSize(0))
	}
	if GridSize(1) != 90.0 {
		t.Fatalf("z1 网格 = %v", GridSize(1))
	}
	if GridSize(20) != 0.0005 {
		t.Fatalf("z20 应触底 0.0005，= %v", GridSize(20))
	}
}
