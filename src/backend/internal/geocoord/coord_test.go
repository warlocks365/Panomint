package geocoord

import (
	"math"
	"testing"
)

// TestOutOfChina 国界判断。
func TestOutOfChina(t *testing.T) {
	cases := []struct {
		lng, lat float64
		out      bool
	}{
		{116.39, 39.90, false}, // 北京
		{121.47, 31.23, false}, // 上海
		{139.69, 35.68, true},  // 东京
		{-74.00, 40.71, true},  // 纽约
		{2.35, 48.85, true},    // 巴黎
	}
	for _, c := range cases {
		if got := OutOfChina(c.lng, c.lat); got != c.out {
			t.Errorf("OutOfChina(%v,%v) = %v, want %v", c.lng, c.lat, got, c.out)
		}
	}
}

// TestWGS84ToGCJ02 AC-15：中国境内产生合理偏移（100~1000m 量级），境外不变。
func TestWGS84ToGCJ02(t *testing.T) {
	// 天安门 WGS-84
	gLng, gLat := WGS84ToGCJ02(116.3974, 39.9086)
	if gLng == 116.3974 && gLat == 39.9086 {
		t.Fatal("境内坐标未发生偏移")
	}
	// 偏移距离应在 100m~1000m 量级
	dLatM := (gLat - 39.9086) * 111000
	dLngM := (gLng - 116.3974) * 111000 * math.Cos(39.9086*math.Pi/180)
	dist := math.Hypot(dLatM, dLngM)
	if dist < 100 || dist > 1000 {
		t.Fatalf("偏移量 %v m 超出合理范围（100~1000m）", dist)
	}

	// 境外不变
	lng, lat := WGS84ToGCJ02(139.6917, 35.6895) // 东京
	if lng != 139.6917 || lat != 35.6895 {
		t.Fatal("境外坐标不应偏移")
	}
}
