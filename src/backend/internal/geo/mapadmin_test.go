package geo

import (
	"reflect"
	"strings"
	"testing"
)

// 本文件覆盖两处新增端点的**纯逻辑**：用户地图 UI 偏好归一化、系统地图配置归一化、
// 高德 Key 的可用性/来源归因。这三块都不碰数据库，故可以穷举；
// 而它们恰好是"写错了不会立刻报错、只在界面上表现为怪现象"的那类逻辑。

func TestNormalizeUIPrefsDefaults(t *testing.T) {
	got, err := NormalizeUIPrefs(UIPrefs{})
	if err != nil {
		t.Fatalf("空输入不应报错: %v", err)
	}
	// 空串回落默认值（与 DDL 默认一致）：PUT 是整体替换语义，
	// 让调用方每次都必须填满四个字段没有意义。
	if got.MapSliderPos != sliderBottom || got.MapFilterSide != filterLeft || got.MapDefaultProvider != providerAuto {
		t.Fatalf("空字段应回落默认值，实际 %+v", got)
	}
	if got.MapDefaultZoom != nil {
		t.Fatalf("zoom 未提供时应为 nil（= 不指定），实际 %v", *got.MapDefaultZoom)
	}
}

func TestNormalizeUIPrefsAcceptsKnownValues(t *testing.T) {
	z := 12
	got, err := NormalizeUIPrefs(UIPrefs{
		MapSliderPos: "  TOP ", MapFilterSide: "RIGHT", MapDefaultProvider: "OSM", MapDefaultZoom: &z,
	})
	if err != nil {
		t.Fatalf("应接受: %v", err)
	}
	if got.MapSliderPos != sliderTop || got.MapFilterSide != filterRight || got.MapDefaultProvider != providerOSM {
		t.Fatalf("应去空白并转小写，实际 %+v", got)
	}
	if got.MapDefaultZoom == nil || *got.MapDefaultZoom != 12 {
		t.Fatalf("zoom 应原样保留，实际 %v", got.MapDefaultZoom)
	}
}

func TestNormalizeUIPrefsRejectsBadValues(t *testing.T) {
	// 这些值会被直接喂给地图初始化；存脏值的故障现场是"地图打不开"，根因在几百行之外。
	cases := []struct {
		name string
		in   UIPrefs
	}{
		{"滑块位置非法", UIPrefs{MapSliderPos: "middle"}},
		{"筛选栏侧非法", UIPrefs{MapFilterSide: "center"}},
		{"provider 非法", UIPrefs{MapDefaultProvider: "baidu"}},
		{"标记样式非法", UIPrefs{MapMarkerMode: "grid"}},
		{"zoom 过小", UIPrefs{MapDefaultZoom: intPtr(0)}},
		{"zoom 过大", UIPrefs{MapDefaultZoom: intPtr(21)}},
		{"zoom 为负", UIPrefs{MapDefaultZoom: intPtr(-3)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NormalizeUIPrefs(c.in); err == nil {
				t.Fatalf("应被拒绝：%+v", c.in)
			}
		})
	}
}

func TestNormalizeUIPrefsMarkerMode(t *testing.T) {
	// 默认 icon；thumb 合法；collapsed 原样带回
	got, err := NormalizeUIPrefs(UIPrefs{})
	if err != nil || got.MapMarkerMode != "icon" || got.MapFilterCollapsed {
		t.Fatalf("默认值应为 icon+未收起: %+v err=%v", got, err)
	}
	got, err = NormalizeUIPrefs(UIPrefs{MapMarkerMode: "thumb", MapFilterCollapsed: true})
	if err != nil || got.MapMarkerMode != "thumb" || !got.MapFilterCollapsed {
		t.Fatalf("thumb+收起应原样保留: %+v err=%v", got, err)
	}
}

func TestNormalizeUIPrefsZoomBounds(t *testing.T) {
	// 边界值必须被接受（否则合法配置会被误拒）
	for _, z := range []int{minMapZoom, maxMapZoom} {
		if _, err := NormalizeUIPrefs(UIPrefs{MapDefaultZoom: intPtr(z)}); err != nil {
			t.Fatalf("zoom=%d 应被接受: %v", z, err)
		}
	}
}

func intPtr(v int) *int { return &v }

func strPtr(v string) *string { return &v }

func TestSystemMapConfigInputEmpty(t *testing.T) {
	if !(SystemMapConfigInput{}).Empty() {
		t.Fatal("零值应判为 Empty")
	}
	if (SystemMapConfigInput{ChinaProvider: strPtr("amap")}).Empty() {
		t.Fatal("有字段时不应判为 Empty")
	}
	if (SystemMapConfigInput{ChinaTileURL: strPtr("")}).Empty() {
		t.Fatal("提供一个空串（= 清空）也算一次改动")
	}
}

func TestNormalizeSystemMapConfig(t *testing.T) {
	t.Run("provider 白名单", func(t *testing.T) {
		if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{ChinaProvider: strPtr(" AMAP ")}); err != nil {
			t.Fatalf("amap 应被接受（并去空白转小写）: %v", err)
		}
		if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{IntlProvider: strPtr("maptiler")}); err != nil {
			t.Fatalf("maptiler 应被接受: %v", err)
		}
		// 新增一家 provider 需要同时改代码里的选源分支，故这里是白名单而不是自由文本
		for _, bad := range []string{"baidu", "google", "AMAP2", ""} {
			if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{ChinaProvider: strPtr(bad)}); err == nil {
				t.Fatalf("china_provider=%q 应被拒绝", bad)
			}
		}
		if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{IntlProvider: strPtr("amap")}); err == nil {
			t.Fatal("intl_provider=amap 应被拒绝（不在国际白名单内）")
		}
	})

	t.Run("tile URL 必须 http(s)，空串表示清空", func(t *testing.T) {
		for _, ok := range []string{"https://tile.example.com/{z}/{x}/{y}.png", "http://a.b/c", ""} {
			if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{ChinaTileURL: strPtr(ok)}); err != nil {
				t.Fatalf("%q 应被接受: %v", ok, err)
			}
		}
		for _, bad := range []string{"ftp://a.b/c", "javascript:alert(1)", "tile.example.com/{z}", "//a.b/c"} {
			if _, err := NormalizeSystemMapConfig(SystemMapConfigInput{ChinaTileURL: strPtr(bad)}); err == nil {
				t.Fatalf("%q 应被拒绝（必须是带 host 的 http/https 地址）", bad)
			}
		}
	})

	t.Run("未提供的字段不进入归一化结果", func(t *testing.T) {
		got, err := NormalizeSystemMapConfig(SystemMapConfigInput{ChinaProvider: strPtr("amap")})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if got.IntlProvider != nil || got.ChinaTileURL != nil || got.IntlTileURL != nil {
			t.Fatalf("未提供字段应保持 nil（部分更新语义），实际 %+v", got)
		}
	})
}

func TestMapKeyState(t *testing.T) {
	cases := []struct {
		name    string
		envKey  string
		dbKey   string
		dbState string
		wantSt  string
		wantSrc string
	}{
		{"库里有可解密 Key → db 优先", "ENVKEY", "DBKEY", ChinaKeyOK, ChinaKeyOK, "db"},
		{"库里没有、env 有 → env", "ENVKEY", "", ChinaKeyAbsent, ChinaKeyOK, "env"},
		{"两边都没有 → absent/none", "", "", ChinaKeyAbsent, ChinaKeyAbsent, "none"},
		// ⚠️ 库里配了密文但解不开时**不回落 env**：那说明配置本身坏了，
		// 静默用 env 顶上会让"界面显示可用、实际走的是另一个 Key"永远查不出来。
		{"密文解不开 → undecryptable（不回落 env）", "ENVKEY", "CIPHER", ChinaKeyUndecryptable, ChinaKeyUndecryptable, "db"},
		{"读库失败且无 env → load_failed", "", "", ChinaKeyLoadFailed, ChinaKeyLoadFailed, "none"},
		{"读库失败但有 env → env 顶上（此时确实可用）", "ENVKEY", "", ChinaKeyLoadFailed, ChinaKeyOK, "env"},
		{"库状态 ok 但 Key 为空 → 视为没有", "", "", ChinaKeyOK, ChinaKeyAbsent, "none"},
		{"env 只有空白 → 视为没有", "   ", "", ChinaKeyAbsent, ChinaKeyAbsent, "none"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, src := mapKeyState(c.envKey, c.dbKey, c.dbState)
			if st != c.wantSt || src != c.wantSrc {
				t.Fatalf("mapKeyState(%q,%q,%q) = (%s,%s)，期望 (%s,%s)",
					c.envKey, c.dbKey, c.dbState, st, src, c.wantSt, c.wantSrc)
			}
		})
	}
}

// TestSystemMapConfigViewNeverCarriesSecret 管理端视图的 JSON tag 不允许出现密钥字段。
//
// 这是一条**结构性**断言：只要有人日后往 SystemMapConfigView 里加 `ChinaAPIKey string
// json:"china_api_key"`，它就会随 GET /admin/map-config 回显给浏览器。
// 用「精确 tag 匹配」而不是"字段名含 key 就报错"——后者会把合法的
// `china_api_key_state`（只报可用性、不含内容）也误判掉。
func TestSystemMapConfigViewNeverCarriesSecret(t *testing.T) {
	forbidden := map[string]bool{
		"china_api_key": true, "china_api_key_enc": true,
		"api_key": true, "key": true, "secret": true, "password": true,
	}
	rt := reflect.TypeOf(SystemMapConfigView{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")[0]
		if forbidden[tag] {
			t.Fatalf("SystemMapConfigView 字段 %s 的 json tag 是 %q —— 密钥会被 GET /admin/map-config 回显",
				f.Name, tag)
		}
	}
	// 但"可用性状态"必须存在：界面需要知道中国地名源能不能用。
	var hasState bool
	for i := 0; i < rt.NumField(); i++ {
		if strings.Split(rt.Field(i).Tag.Get("json"), ",")[0] == "china_api_key_state" {
			hasState = true
		}
	}
	if !hasState {
		t.Fatal("视图必须提供 china_api_key_state（否则界面无法判断 Key 是否可用）")
	}
}
