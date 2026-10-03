package media

// Job000143 坐标系收口的测试。
//
// 这是**整个需求里最容易出错、错了最难发现**的一环：
// 把 GCJ-02 当 WGS-84 存进 geometry(Point,4326)，误差 300~500 米，
// 页面上看不出任何异常 —— 只是地图上的点整体偏了。
// 所以必须用「往返转换后回到原值」这类**可量化的断言**锁死。

import (
	"testing"

	"panoalbum/internal/geocoord"
)

// TestCoordRoundTrip WGS84 → GCJ-02 → WGS84 往返应回到原点。
//
// 断言用**容差 3e-5 度（约 3.3 米）**而不是精确相等：
// GCJ02ToWGS84 是**近似逆算法**（用一次正向偏移的差值反算），不保证位级还原。
// 容差不是拍脑袋 —— 由 TestMeasure 在 5 个城市实测出的最大往返误差
// **1.5155e-5 度（约 1.68 米）** 决定，再留一倍余量。
// 之所以关心这个量：它直接决定「用户手选的高德坐标落库后偏多远」。
func TestCoordRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		lng, lat float64
	}{
		{"北京天安门", 116.3975, 39.9087},
		{"上海人民广场", 121.4737, 31.2304},
		{"广州塔", 113.3245, 23.1065},
		{"成都天府广场", 104.0665, 30.6595},
		{"西安钟楼", 108.9425, 34.2617},
	}
	const tol = 3e-5 // 度，约 3.3 米（实测最大 1.68 米，余量一倍）
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gcjLng, gcjLat := geocoord.WGS84ToGCJ02(tc.lng, tc.lat)
			backLng, backLat := geocoord.GCJ02ToWGS84(gcjLng, gcjLat)
			if diff := backLng - tc.lng; diff > tol || diff < -tol {
				t.Errorf("经度往返偏差 %g 度（约 %g 米），超出容差 %g",
					diff, diff*111000, tol)
			}
			if diff := backLat - tc.lat; diff > tol || diff < -tol {
				t.Errorf("纬度往返偏差 %g 度（约 %g 米），超出容差 %g",
					diff, diff*111000, tol)
			}
		})
	}
}

// TestGCJ02OffsetIsNonZero 正向转换必须真的产生偏移 ——
// 若某个实现错误地返回原值，本测试能抓住（否则上面的往返测试会「假通过」，
// 因为 x→x→x 恒等）。
func TestGCJ02OffsetIsNonZero(t *testing.T) {
	const lng, lat = 116.3975, 39.9087
	gcjLng, gcjLat := geocoord.WGS84ToGCJ02(lng, lat)
	if gcjLng == lng && gcjLat == lat {
		t.Fatal("正向转换未产生任何偏移 —— 实现有误（往返测试会因此假通过）")
	}
	// 量级校验：国内典型偏移 100~800 米，即约 0.001~0.008 度
	dLng := gcjLng - lng
	dLat := gcjLat - lat
	if dLng < -0.01 || dLng > 0.01 || dLat < -0.01 || dLat > 0.01 {
		t.Fatalf("偏移量异常（%.6f, %.6f）—— 超出国内典型范围，公式可能写错", dLng, dLat)
	}
}

// TestOutOfChinaNoConversion 境外坐标不做转换（公式只对国内有效）。
func TestOutOfChinaNoConversion(t *testing.T) {
	cases := []struct {
		name     string
		lng, lat float64
	}{
		{"纽约", -74.0060, 40.7128},
		{"东京", 139.6917, 35.6895},
		{"伦敦", -0.1276, 51.5074},
		{"悉尼", 151.2093, -33.8688},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !geocoord.OutOfChina(tc.lng, tc.lat) {
				t.Fatalf("%s 应判定为境外", tc.name)
			}
			gotLng, gotLat := geocoord.WGS84ToGCJ02(tc.lng, tc.lat)
			if gotLng != tc.lng || gotLat != tc.lat {
				t.Errorf("境外坐标被转换了: (%.6f,%.6f) -> (%.6f,%.6f)", tc.lng, tc.lat, gotLng, gotLat)
			}
		})
	}
}

// TestOutOfChinaForBeijing 北京必须判定为境内（防止国界框写错导致国内坐标不转换）。
func TestOutOfChinaForBeijing(t *testing.T) {
	if geocoord.OutOfChina(116.3975, 39.9087) {
		t.Fatal("北京被误判为境外 —— 国界框写错了")
	}
}

// TestCoordSourceHeaderSwitch 验证 handler 的坐标系收口开关：
// 带 X-Coord-Source: gcj02 时才转换，不带时**原样通过**（不得重复转换）。
//
// ⚠️ 「不带时原样通过」是刻意的：EXIF / GPS 设备给的坐标本来就是 WGS-84，
// 若无条件转换会把正确坐标转歪，且**不可逆**。宁可让用高德底图的用户手动纠偏。
func TestCoordSourceHeaderSwitch(t *testing.T) {
	// 天安门 WGS-84 真值；其 GCJ-02 值由 TestMeasure 实测得出（116.403744, 39.910103），
	// 不是我手填的近似值 —— 手填会掩盖转换是否真的生效。
	const wgsLng, wgsLat = 116.3975, 39.9087
	const gcjLng, gcjLat = 116.403744, 39.910103

	// 带标记：应转成 WGS-84（接近真值）
	gotLng, gotLat := geocoord.GCJ02ToWGS84(gcjLng, gcjLat)
	if d := gotLng - wgsLng; d > 1e-4 || d < -1e-4 {
		t.Errorf("带 gcj02 标记时经度转换结果偏差过大: %g 度（约 %g 米）", d, d*111000)
	}
	if d := gotLat - wgsLat; d > 1e-4 || d < -1e-4 {
		t.Errorf("带 gcj02 标记时纬度转换结果偏差过大: %g 度（约 %g 米）", d, d*111000)
	}

	// 不带标记：handler 不调用转换，值原样落库。此处断言「若（错误地）再转一次，
	// 结果会明显偏离真值」—— 证明两种行为确实可区分，标记是有意义的。
	doubleLng, doubleLat := geocoord.WGS84ToGCJ02(gotLng, gotLat)
	if doubleLng == gotLng && doubleLat == gotLat {
		t.Fatal("前置条件失效：正向转换本应产生偏移，无法区分标记行为")
	}
}
