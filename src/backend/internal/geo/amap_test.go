package geo

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 高德返回的典型成功报文（regeocode.formatted_address 为取用字段）。
const amapOKBody = `{
	"status": "1",
	"info": "OK",
	"infocode": "10000",
	"regeocode": {
		"formatted_address": "北京市东城区东华门街道天安门"
	}
}`

// reqRecorder 记录 mock 服务收到的最后一次请求。
type reqRecorder struct {
	req *http.Request
}

// newMockServer 起一个 mock 高德服务，body 为固定响应；返回 server 与请求记录器。
func newMockServer(t *testing.T, body string) (*httptest.Server, *reqRecorder) {
	t.Helper()
	rec := &reqRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.req = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// TestReverseGeocodeSuccess 成功解析：取 formatted_address，且上传坐标为 GCJ-02 而非原始 WGS-84。
func TestReverseGeocodeSuccess(t *testing.T) {
	t.Setenv("AMAP_KEY", "unit-test-key")
	srv, rec := newMockServer(t, amapOKBody)

	g := &AmapGeocoder{BaseURL: srv.URL}
	addr, err := g.ReverseGeocode(context.Background(), 39.908692, 116.397428)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if addr != "北京市东城区东华门街道天安门" {
		t.Fatalf("地址不符: %q", addr)
	}

	if rec.req == nil {
		t.Fatal("mock 服务未收到请求")
	}
	q := rec.req.URL.Query()
	if q.Get("key") != "unit-test-key" {
		t.Errorf("key 参数错误: %q", q.Get("key"))
	}
	if q.Get("extensions") != "base" || q.Get("output") != "JSON" {
		t.Errorf("extensions/output 参数错误: %q / %q", q.Get("extensions"), q.Get("output"))
	}
	// 关键：必须传 GCJ-02（116.403672,39.910095），直接传 WGS-84 会导致地名偏移数百米。
	const wantLoc = "116.403672,39.910095"
	if loc := q.Get("location"); loc != wantLoc {
		t.Errorf("location 应为 GCJ-02 %q，实际 %q", wantLoc, loc)
	}
	if !strings.HasPrefix(rec.req.URL.Path, "/v3/geocode/regeo") {
		t.Errorf("路径错误: %q", rec.req.URL.Path)
	}
}

// TestReverseGeocodeStatusNotOne status != "1" 时返回带 info 的错误。
func TestReverseGeocodeStatusNotOne(t *testing.T) {
	srv, _ := newMockServer(t, `{"status":"0","info":"INVALID_USER_KEY","infocode":"10001"}`)
	g := &AmapGeocoder{BaseURL: srv.URL, Key: "bad-key"}
	_, err := g.ReverseGeocode(context.Background(), 39.9, 116.4)
	if err == nil {
		t.Fatal("status=0 应返回错误")
	}
	if !strings.Contains(err.Error(), "INVALID_USER_KEY") {
		t.Errorf("错误信息应包含 info，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "10001") {
		t.Errorf("错误信息应包含 infocode，实际: %v", err)
	}
	if errors.Is(err, ErrRateLimited) {
		t.Errorf("INVALID_USER_KEY 不应判定为限流错误")
	}
}

// TestReverseGeocodeQuotaExceeded 配额/QPS 超限映射为 ErrRateLimited。
func TestReverseGeocodeQuotaExceeded(t *testing.T) {
	srv, _ := newMockServer(t, `{"status":"0","info":"CUQPS_HAS_EXCEEDED_THE_LIMIT","infocode":"10021"}`)
	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k"}
	_, err := g.ReverseGeocode(context.Background(), 31.230416, 121.473701)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("期望 ErrRateLimited，实际: %v", err)
	}
}

// TestReverseGeocodeEmptyAddress 接口成功但地址为空。
func TestReverseGeocodeEmptyAddress(t *testing.T) {
	srv, _ := newMockServer(t, `{"status":"1","info":"OK","infocode":"10000","regeocode":{"formatted_address":"  "}}`)
	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k"}
	_, err := g.ReverseGeocode(context.Background(), 23.106558, 113.324462)
	if !errors.Is(err, ErrEmptyAddress) {
		t.Fatalf("期望 ErrEmptyAddress，实际: %v", err)
	}
}

// TestReverseGeocodeNoKey Key 与 AMAP_KEY 均为空时返回 ErrNoKey 且不发请求（不阻塞媒体入库）。
func TestReverseGeocodeNoKey(t *testing.T) {
	t.Setenv("AMAP_KEY", "")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
	}))
	defer srv.Close()

	g := &AmapGeocoder{BaseURL: srv.URL}
	_, err := g.ReverseGeocode(context.Background(), 39.908692, 116.397428)
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("期望 ErrNoKey，实际: %v", err)
	}
	if hits != 0 {
		t.Errorf("无 Key 时不应发起请求，实际命中 %d 次", hits)
	}
}

// TestReverseGeocodeTimeout 上游超时：错误可回溯到 context.DeadlineExceeded。
func TestReverseGeocodeTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(amapOKBody))
	}))
	defer srv.Close()

	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k"}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := g.ReverseGeocode(ctx, 39.908692, 116.397428)
	if err == nil {
		t.Fatal("超时场景应返回错误")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("期望可回溯 context.DeadlineExceeded，实际: %v", err)
	}
}

// TestReverseGeocodeHTTPError 非 200 响应。
func TestReverseGeocodeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k"}
	_, err := g.ReverseGeocode(context.Background(), 39.9, 116.4)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("期望包含状态码 502 的错误，实际: %v", err)
	}
}

// TestReverseGeocodeBadJSON 非法 JSON 响应。
func TestReverseGeocodeBadJSON(t *testing.T) {
	srv, _ := newMockServer(t, `not-json`)
	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k"}
	if _, err := g.ReverseGeocode(context.Background(), 39.9, 116.4); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

// TestReverseCacheKey 缓存 key 格式 "rev:<lat4>,<lng4>"，与正向 "fwd:" 隔离。
func TestReverseCacheKey(t *testing.T) {
	cases := []struct {
		lat, lng float64
		want     string
	}{
		{39.908692, 116.397428, "rev:39.9087,116.3974"},
		{31.230416, 121.473701, "rev:31.2304,121.4737"},
		{39.9, 116.4, "rev:39.9000,116.4000"},
		{-33.868820, 151.209295, "rev:-33.8688,151.2093"},
	}
	for _, c := range cases {
		if got := ReverseCacheKey(c.lat, c.lng); got != c.want {
			t.Errorf("ReverseCacheKey(%v,%v) = %q, want %q", c.lat, c.lng, got, c.want)
		}
	}
	if !strings.HasPrefix(ReverseCacheKey(1, 2), "rev:") {
		t.Error("逆地理 key 必须以 rev: 开头，避免与 fwd: 冲突")
	}
}

// TestWGS84ToGCJ02KnownValues 已知值校验（境内偏移 400~800m 且发生偏移，境外原样返回）。
func TestWGS84ToGCJ02KnownValues(t *testing.T) {
	cases := []struct {
		name       string
		lng, lat   float64
		gLng, gLat float64
	}{
		{"天安门", 116.397428, 39.908692, 116.403672, 39.910095},
		{"上海人民广场", 121.473701, 31.230416, 121.478224, 31.228474},
		{"广州塔", 113.324462, 23.106558, 113.329883, 23.103956},
	}
	const eps = 1e-6
	for _, c := range cases {
		lng, lat := WGS84ToGCJ02(c.lng, c.lat)
		if math.Abs(lng-c.gLng) > eps || math.Abs(lat-c.gLat) > eps {
			t.Errorf("%s: WGS84ToGCJ02(%v,%v) = (%.6f,%.6f), want (%.6f,%.6f)",
				c.name, c.lng, c.lat, lng, lat, c.gLng, c.gLat)
		}
		// 偏移量级独立校验：中国境内典型 GCJ-02 偏移为数百米
		dLngM := (lng - c.lng) * 111320 * math.Cos(c.lat*math.Pi/180)
		dLatM := (lat - c.lat) * 110574
		if d := math.Hypot(dLngM, dLatM); d < 400 || d > 800 {
			t.Errorf("%s: 偏移 %.0fm 超出合理区间 400~800m", c.name, d)
		}
	}
	// 境外（东京）原样返回
	lng, lat := WGS84ToGCJ02(139.691706, 35.689487)
	if lng != 139.691706 || lat != 35.689487 {
		t.Errorf("境外坐标应原样返回，实际 (%.6f,%.6f)", lng, lat)
	}
}

// TestQPSLimiter 限流器按最小间隔放行，且 ctx 取消时立即返回。
func TestQPSLimiter(t *testing.T) {
	l := newQPSLimiter(20) // 50ms 间隔
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.Wait(context.Background()); err != nil {
			t.Fatalf("第 %d 次 Wait 出错: %v", i, err)
		}
	}
	if el := time.Since(start); el < 100*time.Millisecond {
		t.Errorf("3 次调用应至少耗时 100ms（间隔 50ms），实际 %v", el)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	start = time.Now()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("ctx 已超时，Wait 应返回错误")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("ctx 取消后应立即返回，实际耗时 %v", time.Since(start))
	}
}

// TestAmapGeocoderNoPoolSkipsCache Pool 为 nil 时不访问数据库（测试环境无 DB 也能跑通）。
func TestAmapGeocoderNoPoolSkipsCache(t *testing.T) {
	srv, _ := newMockServer(t, amapOKBody)
	g := &AmapGeocoder{BaseURL: srv.URL, Key: "k", QPS: -1}
	addr, err := g.ReverseGeocode(context.Background(), 39.908692, 116.397428)
	if err != nil || addr == "" {
		t.Fatalf("Pool=nil 时应跳过缓存直接请求，实际 addr=%q err=%v", addr, err)
	}
}
