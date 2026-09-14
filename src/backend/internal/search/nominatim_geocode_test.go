package search

// 补充单测（与 nominatim_search_test.go 互补，不重复）：
//   - 既有单结果接口 Geocode 在「传输层重构 + 条款限速」后行为不回归（place 地理降级依赖它）；
//   - 限速发生在发请求之前：ctx 已取消时不占用上游配额。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newGeoStubServer 起一个假 Nominatim（本地，无外网依赖），记录命中次数。
func newGeoStubServer(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestNominatimGeocodeRegression 单结果接口回归：成功取值、空结果为 (ok=false, err=nil)。
// 后者是搜索 place 降级的前提语义（store.go 据此按文本零结果继续，而非 500）。
func TestNominatimGeocodeRegression(t *testing.T) {
	defer disableNominatimRateLimit()()

	srv, _ := newGeoStubServer(t, `[{"lon":"120.1551","lat":"30.2741","display_name":"杭州"}]`)
	g := NominatimGeocoder{BaseURL: srv.URL}
	lon, lat, ok, err := g.Geocode(context.Background(), "杭州")
	if err != nil || !ok || lon != 120.1551 || lat != 30.2741 {
		t.Fatalf("Geocode 成功路径回归: lon=%v lat=%v ok=%v err=%v", lon, lat, ok, err)
	}

	srvEmpty, _ := newGeoStubServer(t, `[]`)
	g2 := NominatimGeocoder{BaseURL: srvEmpty.URL}
	if _, _, ok, err := g2.Geocode(context.Background(), "空"); err != nil || ok {
		t.Fatalf("空结果应为 ok=false,nil err，实际 ok=%v err=%v", ok, err)
	}
}

// TestNominatimSearchCanceledContextSkipsUpstream ctx 已取消时不应发出请求
// （限速在前、请求在后，避免把必然失败的调用计入上游配额）。
func TestNominatimSearchCanceledContextSkipsUpstream(t *testing.T) {
	defer disableNominatimRateLimit()()

	srv, hits := newGeoStubServer(t, `[]`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	g := NominatimGeocoder{BaseURL: srv.URL}
	if _, err := g.Search(ctx, "西湖", 5); err == nil {
		t.Fatal("ctx 已取消应返回错误")
	}
	if *hits != 0 {
		t.Errorf("ctx 已取消不应发出请求，实际命中 %d 次", *hits)
	}
}
