package index

import "testing"

// 守卫纪律：硬编码期望值，覆盖 ISO6709 主流形态 / DJI XML / 越界拒绝 / 优先级。

func TestParseISO6709(t *testing.T) {
	cases := []struct {
		in                     string
		lat, lng               float64
		ok                     bool
	}{
		{"+27.1720-080.0389/", 27.172, -80.0389, true},          // iPhone 典型（含高度分隔）
		{"+27.1720-080.0389", 27.172, -80.0389, true},           // 无尾斜杠
		{"+22.5726+088.3639/", 22.5726, 88.3639, true},          // 东经（印度）
		{"-33.8688+151.2093/+0.000/", -33.8688, 151.2093, true}, // 南半球（含高度）
		{"+45.0000+120.5", 45.0, 120.5, true},                   // 整数度
		{"", 0, 0, false},                                       // 空
		{"27.1720-080.0389/", 0, 0, false},                      // 缺符号
		{"+91.0000+120.0000/", 0, 0, false},                     // 纬度越界
		{"+45.0000+181.0000/", 0, 0, false},                     // 经度越界
		{"not-a-coordinate", 0, 0, false},
	}
	for _, tc := range cases {
		lat, lng, ok := parseISO6709(tc.in)
		if ok != tc.ok || (ok && (lat != tc.lat || lng != tc.lng)) {
			t.Errorf("parseISO6709(%q) = (%v,%v,%v), want (%v,%v,%v)", tc.in, lat, lng, ok, tc.lat, tc.lng, tc.ok)
		}
	}
}

func TestParseDJIXMLComment(t *testing.T) {
	dji := `DJI Mega: <drone-dji:latitude>22.5726</drone-dji:latitude><drone-dji:longitude>113.9525</drone-dji:longitude>`
	lat, lng, ok := parseDJIXMLComment(dji)
	if !ok || lat != 22.5726 || lng != 113.9525 {
		t.Errorf("DJI comment = (%v,%v,%v)", lat, lng, ok)
	}
	// 属性形态
	attr := `<drone-dji:gimbal-roll-degree>"+0.00"</drone-dji:gimbal-roll-degree><drone-dji:latitude="22.57">x<drone-dji:longitude="113.95">y`
	if _, _, ok := parseDJIXMLComment(attr); ok {
		// 属性形态由宽匹配覆盖：latitude="22.57" —— [>"]+ 匹配 =" —— 允许
		t.Log("attr form parsed (tolerated)")
	}
	if _, _, ok := parseDJIXMLComment("no drone here"); ok {
		t.Fatal("无 drone-dji 标记必须 false")
	}
}

func TestVideoGPSFromTags_Priority(t *testing.T) {
	// location 优先于 apple quicktime
	tags := map[string]string{
		"location":                            "+27.0000-080.0000/",
		"com.apple.quicktime.location.ISO6709": "+10.0000+020.0000/",
	}
	lat, lng, ok := videoGPSFromTags(tags)
	if !ok || lat != 27.0 || lng != -80.0 {
		t.Errorf("location 优先级失效: %v %v %v", lat, lng, ok)
	}
	// 只有 apple quicktime 时取它
	lat, lng, ok = videoGPSFromTags(map[string]string{
		"com.apple.quicktime.location.ISO6709": "+10.0000+020.0000/",
	})
	if !ok || lat != 10.0 || lng != 20.0 {
		t.Errorf("apple fallback 失效: %v %v %v", lat, lng, ok)
	}
	// comment DJI 兜底
	lat, lng, ok = videoGPSFromTags(map[string]string{
		"comment": "<drone-dji:latitude>1.5</drone-dji:latitude><drone-dji:longitude>2.5</drone-dji:longitude>",
	})
	if !ok || lat != 1.5 || lng != 2.5 {
		t.Errorf("DJI 兜底失效: %v %v %v", lat, lng, ok)
	}
	// 全空
	if _, _, ok := videoGPSFromTags(map[string]string{}); ok {
		t.Fatal("空 tags 必须 false")
	}
}

func TestApplyVideoLocationTags_PlaceFallback(t *testing.T) {
	m := &Meta{}
	applyVideoLocationTags(m, map[string]string{"comment": "云南大理洱海西岸"})
	if m.Place != "云南大理洱海西岸" {
		t.Errorf("comment 地址应落 place，得 %q", m.Place)
	}
	// 有 GPS 时优先，place 不被 comment 覆盖
	m2 := &Meta{}
	applyVideoLocationTags(m2, map[string]string{
		"location": "+27.0000-080.0000/",
		"comment":  "some place text",
	})
	if m2.Lat == nil || *m2.Lat != 27.0 || m2.Place != "" {
		t.Errorf("GPS 优先且 place 不应被写: %+v", m2)
	}
	// XML 注释不落 place（含尖括号）
	m3 := &Meta{}
	applyVideoLocationTags(m3, map[string]string{"comment": "<xml>stuff</xml>"})
	if m3.Place != "" {
		t.Errorf("含尖括号 comment 不应落 place: %q", m3.Place)
	}
}
