package geo

// /map/search 国际源适配器（nominatimForwarder）单测。
//
// 该适配器是 internal/search（Nominatim 客户端，返回 WGS-84 的 search.Place）
// 与 /map/search 编排层（ForwardHit，需标注 provider 供坐标归一化分支判断）之间的胶水：
//   - 字段映射（Name/Lon/Lat）必须逐一对应；
//   - provider 必须标注为 nominatim —— 若标错，toDisplayCoord 会把 WGS-84 当 GCJ-02
//     原样下发，前端地图上偏数百米。
//
// 全程 httptest 假上游，不依赖外网与数据库。注意 internal/search 的包级限速器（≥1s/请求）
// 未导出开关，故本用例最多等待 1s（条款合规优先于测试速度）。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"panoalbum/internal/search"
)

func TestNominatimForwarderMapsPlacesToHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"display_name":"西湖, 杭州, 浙江","lat":"30.2500","lon":"120.1500"},
			{"display_name":"坐标缺失"}
		]`))
	}))
	defer srv.Close()

	f := nominatimForwarder{G: search.NominatimGeocoder{BaseURL: srv.URL}}
	hits, err := f.Forward(context.Background(), "西湖", 5)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("坐标缺失的候选应被跳过，期望 1 条，实际 %d: %+v", len(hits), hits)
	}
	h := hits[0]
	if h.Name != "西湖, 杭州, 浙江" || h.Lon != 120.15 || h.Lat != 30.25 {
		t.Errorf("字段映射不符: %+v", h)
	}
	if h.Provider != nominatimProvider {
		t.Errorf("provider 必须标注为 %q（否则坐标归一化会走错分支），实际 %q", nominatimProvider, h.Provider)
	}

	// 空结果透传为空切片 + nil 错误（不是错误），上层据此继续降级到 200 + []
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srvEmpty.Close()
	if hits, err := (nominatimForwarder{G: search.NominatimGeocoder{BaseURL: srvEmpty.URL}}).
		Forward(context.Background(), "不存在的地名xyz", 5); err != nil || len(hits) != 0 {
		t.Fatalf("空结果应为 0 条 + nil 错误，得到 err=%v / %d 条", err, len(hits))
	}
}
