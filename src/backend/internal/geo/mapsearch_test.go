package geo

// /map/search 纯逻辑单测：不依赖网络与数据库。
//   - 源选择（CJK 判定 / 无 Key 降级 / 上游失败降级 / 空结果回落）
//   - 高德与 Nominatim 响应解析（构造 JSON：成功 / 空结果 / 字段缺失 / 上游错误码）
//   - 坐标归一化（GCJ-02 ↔ WGS-84，与 /geo/* 同一约定）
//   - 缓存复用（provider 参与 key、重复查询命中缓存）
//   - HTTP 层：q 空白 → 400、无结果 → 200 + candidates: []

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- 测试替身 ----

// fakeAmapForwarder 记录调用并可控返回，用于验证「选源/降级」而不打真实上游。
type fakeAmapForwarder struct {
	gotKey   string
	gotQuery string
	calls    int
	hits     []ForwardHit
	err      error
}

func (f *fakeAmapForwarder) ForwardWithKey(_ context.Context, key, _ /*secret*/, query string, _ int) ([]ForwardHit, error) {
	f.calls++
	f.gotKey = key
	f.gotQuery = query
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

// fakeIntlForwarder 同上，代表 Nominatim。
type fakeIntlForwarder struct {
	gotQuery string
	calls    int
	hits     []ForwardHit
	err      error
}

func (f *fakeIntlForwarder) Forward(_ context.Context, query string, _ int) ([]ForwardHit, error) {
	f.calls++
	f.gotQuery = query
	if f.err != nil {
		return nil, f.err
	}
	return f.hits, nil
}

// memCache 内存缓存（替代 geo_cache 表）。
type memCache struct {
	m    map[string][]ForwardHit
	puts int
}

func newMemCache() *memCache { return &memCache{m: map[string][]ForwardHit{}} }

func (c *memCache) Get(_ context.Context, key string) ([]ForwardHit, bool) {
	v, ok := c.m[key]
	return v, ok
}

func (c *memCache) Put(_ context.Context, key, _ string, hits []ForwardHit) {
	c.puts++
	c.m[key] = hits
}

// newTestService 构造开关齐全的测试服务（QPS<0 关闭高德限流，避免用例等待）。
func newTestService(amap AmapForwarder, intl IntlForwarder, key string) (*MapSearchService, *memCache) {
	c := newMemCache()
	return &MapSearchService{
		Cache:           c,
		Config:          &MapConfigStore{}, // Pool 为 nil → 默认配置
		Amap:            amap,
		Intl:            intl,
		EnvAmapKey:      key,
		Timeout:         time.Second,
		Limit:           5,
		QPS:             -1,
		DisplayProvider: amapProvider,
	}, c
}

// ---- 源选择判定 ----

func TestIsChinaQuery(t *testing.T) {
	cases := []struct {
		q    string
		want bool
	}{
		{"西湖", true},
		{"天安门", true},
		{"北京市朝阳区", true},
		{"西湖 westlake", true}, // 含汉字即视为中国
		{" 西湖 ", true},        // 前后空白不影响判定
		{"West Lake", false},  // 纯拉丁
		{"Paris", false},      //
		{"とうきょう", false},      // 平假名 → 日本
		{"トウキョウ", false},      // 片假名 → 日本
		{"서울", false},         // 谚文 → 韩国
		{"", false},           //
		{"12345", false},      // 无汉字
		{"München", false},    // 变音符号非 CJK
	}
	for _, c := range cases {
		if got := isChinaQuery(c.q); got != c.want {
			t.Errorf("isChinaQuery(%q) = %v, want %v", c.q, got, c.want)
		}
	}
}

func TestResolveAmapKeyPriority(t *testing.T) {
	// 系统配置（解密值）优先于环境变量
	s := &MapSearchService{EnvAmapKey: "env-key"}
	k, st := s.resolveAmapKey(MapConfig{ChinaAPIKey: "db-key", ChinaKeyState: ChinaKeyOK})
	if k != "db-key" || st != ChinaKeyOK {
		t.Fatalf("系统配置应优先，得到 %q/%q", k, st)
	}
	// 系统配置无 key → 环境变量兜底
	k, st = s.resolveAmapKey(MapConfig{ChinaKeyState: ChinaKeyAbsent})
	if k != "env-key" || st != ChinaKeyOK {
		t.Fatalf("环境变量应兜底，得到 %q/%q", k, st)
	}
	// 两者皆无 → 归因状态透出
	s = &MapSearchService{}
	if k, st = s.resolveAmapKey(MapConfig{ChinaKeyState: ChinaKeyUndecryptable}); k != "" || st != ChinaKeyUndecryptable {
		t.Fatalf("解密失败应归因 undecryptable，得到 %q/%q", k, st)
	}
	if k, st = s.resolveAmapKey(MapConfig{ChinaKeyState: ChinaKeyLoadFailed}); k != "" || st != ChinaKeyLoadFailed {
		t.Fatalf("读配置失败应归因 load_failed，得到 %q/%q", k, st)
	}
	if k, st = s.resolveAmapKey(MapConfig{}); k != "" || st != ChinaKeyAbsent {
		t.Fatalf("未配置应归因 absent，得到 %q/%q", k, st)
	}
}

// ---- system_map_config 读取 ----

func TestMapConfigLoadNilPoolDefaults(t *testing.T) {
	cfg := (&MapConfigStore{}).Load(context.Background())
	if cfg.ChinaProvider != defaultChinaProvider || cfg.IntlProvider != defaultIntlProvider {
		t.Fatalf("默认供应商应为 amap/osm，得到 %q/%q", cfg.ChinaProvider, cfg.IntlProvider)
	}
	if cfg.ChinaAPIKey != "" || cfg.ChinaKeyState != ChinaKeyAbsent {
		t.Fatalf("无库时应无 key 且状态 absent，得到 %q/%q", cfg.ChinaAPIKey, cfg.ChinaKeyState)
	}
}

func TestResolveEncryptedKey(t *testing.T) {
	// 未注入解密器：按明文处理（兼容运营直写明文 key）
	k, st := resolveEncryptedKey("plain-key", nil)
	if k != "plain-key" || st != ChinaKeyOK {
		t.Fatalf("无解密器应按明文，得到 %q/%q", k, st)
	}
	// 空密文 → absent
	if k, st = resolveEncryptedKey("  ", nil); k != "" || st != ChinaKeyAbsent {
		t.Fatalf("空值应为 absent，得到 %q/%q", k, st)
	}
	// 注入解密器 · 成功
	dec := func(enc string) (string, error) { return "dec:" + enc, nil }
	if k, st = resolveEncryptedKey("cipher", dec); k != "dec:cipher" || st != ChinaKeyOK {
		t.Fatalf("解密成功应返回明文，得到 %q/%q", k, st)
	}
	// 注入解密器 · 失败 → 不可用（绝不把密文当 key 用）
	failing := func(string) (string, error) { return "", errors.New("bad key") }
	if k, st = resolveEncryptedKey("cipher", failing); k != "" || st != ChinaKeyUndecryptable {
		t.Fatalf("解密失败应归因 undecryptable 且不回退密文，得到 %q/%q", k, st)
	}
	// 注入解密器 · 解出空串同样视为不可用
	empty := func(string) (string, error) { return "   ", nil }
	if k, st = resolveEncryptedKey("cipher", empty); k != "" || st != ChinaKeyUndecryptable {
		t.Fatalf("解出空串应归因 undecryptable，得到 %q/%q", k, st)
	}
}

// ---- 缓存 key ----

func TestMapSearchCacheKey(t *testing.T) {
	k1 := MapSearchCacheKey(amapProvider, "西湖")
	k2 := MapSearchCacheKey(nominatimProvider, "西湖")
	if k1 == k2 {
		t.Fatalf("不同 provider 的 key 必须不同，两者都是 %q", k1)
	}
	// key 含结构版本前缀（Job000143）：加字段后必须 bump 版本，
	// 否则改动前写入的缓存会命中并返回**缺该字段**的旧载荷（address 永远为空）。
	if k1 != "fwd:"+mapSearchCacheVer+":amap:西湖" || k2 != "fwd:"+mapSearchCacheVer+":nominatim:西湖" {
		t.Fatalf("key 格式不符：%q / %q", k1, k2)
	}
	if !strings.Contains(k1, mapSearchCacheVer) {
		t.Fatalf("key 必须含结构版本 %q，否则改字段后旧缓存会串味：%q", mapSearchCacheVer, k1)
	}
	// 与逆地理 rev: 及单结果正向 fwd:<query> 命名空间隔离
	if strings.HasPrefix(k1, "rev:") || k1 == "fwd:西湖" {
		t.Fatalf("key 与既有命名空间冲突：%q", k1)
	}
	// 空白归一化：同一查询的不同空白写法命中同一 key
	if MapSearchCacheKey(amapProvider, "  West   Lake ") != MapSearchCacheKey(amapProvider, "West Lake") {
		t.Error("空白折叠后应命中同一缓存 key")
	}
	// 超长查询退化为 md5，且不超 key 列上限 128 字节
	long := strings.Repeat("西", 500)
	kl := MapSearchCacheKey(amapProvider, long)
	if len(kl) > 128 {
		t.Fatalf("超长 key 应退化为摘要且 ≤128 字节，实际 %d", len(kl))
	}
	if kl == MapSearchCacheKey(amapProvider, strings.Repeat("湖", 500)) {
		t.Error("不同长查询的摘要不应相同")
	}
}

// ---- 高德响应解析 ----

const amapGeoOKBody = `{
	"status":"1","info":"OK","infocode":"10000","count":"2",
	"geocodes":[
		{"formatted_address":"浙江省杭州市西湖区西湖","province":"浙江省","city":"杭州市","district":"西湖区","location":"120.130000,30.250000"},
		{"formatted_address":"北京市海淀区颐和园","province":"北京市","city":"北京市","district":"海淀区","location":"116.270000,39.990000"}
	]}`

func TestAmapForwardParsing(t *testing.T) {
	srv, rec := newMockServer(t, amapGeoOKBody)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}

	hits, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 5)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("期望 2 个候选，得到 %d", len(hits))
	}
	if hits[0].Name != "浙江省杭州市西湖区西湖" || hits[0].Provider != amapProvider {
		t.Errorf("首候选不符: %+v", hits[0])
	}
	if math.Abs(hits[0].Lon-120.130000) > 1e-9 || math.Abs(hits[0].Lat-30.250000) > 1e-9 {
		t.Errorf("首候选坐标不符: %v,%v", hits[0].Lon, hits[0].Lat)
	}
	if math.Abs(hits[1].Lon-116.270000) > 1e-9 || math.Abs(hits[1].Lat-39.990000) > 1e-9 {
		t.Errorf("次候选坐标不符: %v,%v", hits[1].Lon, hits[1].Lat)
	}
	// 请求参数与路径
	if rec.req == nil {
		t.Fatal("mock 服务未收到请求")
	}
	if rec.req.URL.Path != "/v3/geocode/geo" {
		t.Errorf("路径错误: %q", rec.req.URL.Path)
	}
	q := rec.req.URL.Query()
	if q.Get("key") != "k" || q.Get("address") != "西湖" || q.Get("output") != "JSON" {
		t.Errorf("请求参数错误: %v", q)
	}
	// 高德返回 GCJ-02（原生），不做提前转换
	if q.Get("location") != "" {
		t.Errorf("正向编码不应带 location 参数，实际 %q", q.Get("location"))
	}
}

func TestAmapForwardLimit(t *testing.T) {
	srv, _ := newMockServer(t, amapGeoOKBody)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	hits, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 1)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("limit=1 应截断为 1 个候选，得到 %d", len(hits))
	}
}

func TestAmapForwardEmptyResult(t *testing.T) {
	// status=1 但无候选：无结果不是错误（上层返回 200 + candidates: []）
	srv, _ := newMockServer(t, `{"status":"1","info":"OK","infocode":"10000","count":"0","geocodes":[]}`)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	hits, err := g.ForwardWithKey(context.Background(), "k", "", "不存在的地名xyz", 5)
	if err != nil {
		t.Fatalf("空结果不应报错，得到 %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("空结果应返回 0 条，得到 %d", len(hits))
	}
	// geocodes 字段整体缺失同样视为无结果
	srv2, _ := newMockServer(t, `{"status":"1","info":"OK","infocode":"10000"}`)
	g2 := &AmapForwardGeocoder{BaseURL: srv2.URL}
	if hits, err := g2.ForwardWithKey(context.Background(), "k", "", "q", 5); err != nil || len(hits) != 0 {
		t.Fatalf("geocodes 缺失应视为空结果，得到 %v/%d", err, len(hits))
	}
}

func TestAmapForwardFieldMissing(t *testing.T) {
	// 坐标缺失/非法候选应被跳过；名称缺失回退到省市区拼接
	body := `{"status":"1","info":"OK","infocode":"10000","count":"3","geocodes":[
		{"formatted_address":"无坐标","location":""},
		{"province":"浙江省","city":"杭州市","district":"西湖区","location":"120.13,30.25"},
		{"formatted_address":"非法坐标","location":"abc,def"},
		{"formatted_address":"越界坐标","location":"999,999"}
	]}`
	srv, _ := newMockServer(t, body)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	hits, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 5)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("仅 1 条候选坐标合法，实际 %d 条: %+v", len(hits), hits)
	}
	if hits[0].Name != "浙江省杭州市西湖区" {
		t.Errorf("名称应回退为省市区拼接，实际 %q", hits[0].Name)
	}
}

func TestAmapForwardUpstreamError(t *testing.T) {
	// status=0 + 非配额错误码：错误信息含 info/infocode，且不误判为限流
	srv, _ := newMockServer(t, `{"status":"0","info":"INVALID_USER_KEY","infocode":"10001"}`)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	_, err := g.ForwardWithKey(context.Background(), "bad", "", "西湖", 5)
	if err == nil {
		t.Fatal("status=0 应返回错误")
	}
	if !strings.Contains(err.Error(), "INVALID_USER_KEY") || !strings.Contains(err.Error(), "10001") {
		t.Errorf("错误应含 info/infocode，实际: %v", err)
	}
	if errors.Is(err, ErrRateLimited) {
		t.Error("INVALID_USER_KEY 不应判定为限流")
	}
}

func TestAmapForwardQuotaError(t *testing.T) {
	srv, _ := newMockServer(t, `{"status":"0","info":"CUQPS_HAS_EXCEEDED_THE_LIMIT","infocode":"10021"}`)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	if _, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 5); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("期望 ErrRateLimited，实际 %v", err)
	}
}

func TestAmapForwardNoKeySkipsRequest(t *testing.T) {
	t.Setenv("AMAP_KEY", "")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
	}))
	defer srv.Close()

	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	_, err := g.ForwardWithKey(context.Background(), "", "", "西湖", 5)
	if !errors.Is(err, ErrNoKey) {
		t.Fatalf("无 Key 应返回 ErrNoKey，实际 %v", err)
	}
	if hits != 0 {
		t.Errorf("无 Key 时不应发起请求，实际命中 %d 次", hits)
	}
}

func TestAmapForwardBadJSON(t *testing.T) {
	srv, _ := newMockServer(t, `not-json`)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	if _, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 5); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}
}

func TestAmapForwardHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	_, err := g.ForwardWithKey(context.Background(), "k", "", "西湖", 5)
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("期望含状态码 502 的错误，实际 %v", err)
	}
}

func TestAmapForwardTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(amapGeoOKBody))
	}))
	defer srv.Close()
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := g.ForwardWithKey(ctx, "k", "", "西湖", 5); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望可回溯 DeadlineExceeded，实际 %v", err)
	}
}

func TestAmapForwardWithSig(t *testing.T) {
	srv, rec := newMockServer(t, amapGeoOKBody)
	g := &AmapForwardGeocoder{BaseURL: srv.URL}
	const secret = "unit-test-secret"
	if _, err := g.ForwardWithKey(context.Background(), "k", secret, "西湖", 5); err != nil {
		t.Fatalf("期望成功，得到 %v", err)
	}
	q := rec.req.URL.Query()
	got := q.Get("sig")
	if got == "" {
		t.Fatal("配置了 Secret 但请求未带 sig")
	}
	q.Del("sig")
	if want := amapSig(q, secret); got != want {
		t.Errorf("sig 校验失败: 请求 %q，复算 %q", got, want)
	}
}

// ---- 坐标归一化 ----

func TestToDisplayCoord(t *testing.T) {
	const (
		wgsLng, wgsLat = 116.397428, 39.908692 // 天安门 WGS-84
		gcjLng, gcjLat = 116.403672, 39.910095 // 同一地点 GCJ-02
	)
	// Nominatim 结果（WGS-84）在 amap 底图上 → 转 GCJ-02
	lon, lat := toDisplayCoord(ForwardHit{Lon: wgsLng, Lat: wgsLat, Provider: nominatimProvider}, amapProvider)
	if math.Abs(lon-gcjLng) > 1e-6 || math.Abs(lat-gcjLat) > 1e-6 {
		t.Errorf("WGS→GCJ 不符: (%.6f,%.6f)，期望 (%.6f,%.6f)", lon, lat, gcjLng, gcjLat)
	}
	// 高德结果（GCJ-02）在 amap 底图上 → 原样返回
	lon, lat = toDisplayCoord(ForwardHit{Lon: gcjLng, Lat: gcjLat, Provider: amapProvider}, amapProvider)
	if lon != gcjLng || lat != gcjLat {
		t.Errorf("同坐标系应原样返回: (%.6f,%.6f)", lon, lat)
	}
	// Nominatim 结果在 osm 底图上 → 原样返回
	lon, lat = toDisplayCoord(ForwardHit{Lon: wgsLng, Lat: wgsLat, Provider: nominatimProvider}, "osm")
	if lon != wgsLng || lat != wgsLat {
		t.Errorf("osm 输出应保持 WGS-84: (%.6f,%.6f)", lon, lat)
	}
	// 高德结果在 osm 底图上 → 反变换回 WGS-84（近似可逆，容差 ~1e-4° ≈ 11m）
	lon, lat = toDisplayCoord(ForwardHit{Lon: gcjLng, Lat: gcjLat, Provider: amapProvider}, "osm")
	if math.Abs(lon-wgsLng) > 1e-4 || math.Abs(lat-wgsLat) > 1e-4 {
		t.Errorf("GCJ→WGS 反变换偏差过大: (%.6f,%.6f)，期望≈(%.6f,%.6f)", lon, lat, wgsLng, wgsLat)
	}
}

// ---- 编排：选源 / 降级 / 回落 ----

func TestCandidatesSourceSelection(t *testing.T) {
	amapHit := ForwardHit{Name: "杭州西湖", Lon: 120.13, Lat: 30.25, Provider: amapProvider}
	intlHit := ForwardHit{Name: "West Lake", Lon: 120.14, Lat: 30.24, Provider: nominatimProvider}

	cases := []struct {
		name         string
		query        string
		envKey       string
		amap         *fakeAmapForwarder
		intl         *fakeIntlForwarder
		wantAmap     int
		wantIntl     int
		wantDegraded string
		wantProvider string
	}{
		{
			name: "中国地名+有 Key → 仅高德", query: "西湖", envKey: "k",
			amap: &fakeAmapForwarder{hits: []ForwardHit{amapHit}}, intl: &fakeIntlForwarder{hits: []ForwardHit{intlHit}},
			wantAmap: 1, wantIntl: 0, wantDegraded: "", wantProvider: amapProvider,
		},
		{
			name: "中国地名+无 Key → 降级回落 Nominatim", query: "西湖", envKey: "",
			amap: &fakeAmapForwarder{hits: []ForwardHit{amapHit}}, intl: &fakeIntlForwarder{hits: []ForwardHit{intlHit}},
			wantAmap: 0, wantIntl: 1, wantDegraded: "amap_absent", wantProvider: nominatimProvider,
		},
		{
			name: "中国地名+高德报错 → 回落", query: "西湖", envKey: "k",
			amap: &fakeAmapForwarder{err: errors.New("上游 500")}, intl: &fakeIntlForwarder{hits: []ForwardHit{intlHit}},
			wantAmap: 1, wantIntl: 1, wantDegraded: "amap_error", wantProvider: nominatimProvider,
		},
		{
			name: "中国地名+高德空结果 → 回落（标记 amap_empty）", query: "東京", envKey: "k",
			amap: &fakeAmapForwarder{hits: nil}, intl: &fakeIntlForwarder{hits: []ForwardHit{intlHit}},
			wantAmap: 1, wantIntl: 1, wantDegraded: "amap_empty", wantProvider: nominatimProvider,
		},
		{
			name: "国际地名 → 仅 Nominatim，不降级", query: "West Lake", envKey: "k",
			amap: &fakeAmapForwarder{hits: []ForwardHit{amapHit}}, intl: &fakeIntlForwarder{hits: []ForwardHit{intlHit}},
			wantAmap: 0, wantIntl: 1, wantDegraded: "", wantProvider: nominatimProvider,
		},
		{
			name: "全部上游无结果 → 空数组且标记降级", query: "西湖", envKey: "",
			amap: &fakeAmapForwarder{hits: nil}, intl: &fakeIntlForwarder{hits: nil},
			wantAmap: 0, wantIntl: 1, wantDegraded: "amap_absent", wantProvider: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _ := newTestService(c.amap, c.intl, c.envKey)
			got, degraded := s.Candidates(context.Background(), c.query, "")
			if c.amap.calls != c.wantAmap {
				t.Errorf("高德调用次数 = %d, want %d", c.amap.calls, c.wantAmap)
			}
			if c.intl.calls != c.wantIntl {
				t.Errorf("Nominatim 调用次数 = %d, want %d", c.intl.calls, c.wantIntl)
			}
			if degraded != c.wantDegraded {
				t.Errorf("degraded = %q, want %q", degraded, c.wantDegraded)
			}
			if c.wantProvider == "" {
				if got == nil || len(got) != 0 {
					t.Fatalf("期望空候选切片，得到 %+v", got)
				}
			} else {
				if len(got) != 1 || got[0].Provider != c.wantProvider {
					t.Fatalf("候选不符: %+v", got)
				}
			}
		})
	}
}

func TestCandidatesNoIntlDegradesToEmpty(t *testing.T) {
	// intl 缺失（如装配失败）+ 无高德 Key：不得 panic，返回空候选 + 降级标记
	s, _ := newTestService(&fakeAmapForwarder{}, nil, "")
	got, degraded := s.Candidates(context.Background(), "西湖", "")
	if len(got) != 0 || degraded != "amap_absent" {
		t.Fatalf("期望空候选 + amap_absent，得到 %+v / %q", got, degraded)
	}
}

func TestCandidatesChinaProviderNullDisablesAmap(t *testing.T) {
	// system_map_config.china_provider='null' → 系统显式关闭中国底图，高德源停用
	amap := &fakeAmapForwarder{hits: []ForwardHit{{Name: "x", Provider: amapProvider}}}
	intl := &fakeIntlForwarder{hits: []ForwardHit{{Name: "y", Provider: nominatimProvider}}}
	s, _ := newTestService(amap, intl, "k")
	s.Config = stubConfigLoader{cfg: MapConfig{ChinaProvider: "null", IntlProvider: "osm"}}

	got, degraded := s.Candidates(context.Background(), "西湖", "")
	if amap.calls != 0 {
		t.Errorf("china_provider=null 时不应调用高德，实际 %d 次", amap.calls)
	}
	if len(got) != 1 || got[0].Provider != nominatimProvider {
		t.Fatalf("应回落 Nominatim，得到 %+v", got)
	}
	// Key 本身可用但 provider 被关闭：归因必须指向 provider，而非误报 amap_ok
	if degraded != "amap_provider_null" {
		t.Errorf("降级归因应为 amap_provider_null，实际 %q", degraded)
	}
}

func TestAmapUnavailableReason(t *testing.T) {
	cases := []struct {
		providerOff, clientMissing bool
		keyState                   string
		want                       string
	}{
		{false, false, ChinaKeyAbsent, "amap_absent"},
		{false, false, ChinaKeyUndecryptable, "amap_undecryptable"},
		{false, false, ChinaKeyLoadFailed, "amap_load_failed"},
		{false, false, ChinaKeyOK, "amap_unavailable"}, // key 可用却不可达：兜底表述
		{false, true, ChinaKeyOK, "amap_client_unavailable"},
		{true, false, ChinaKeyOK, "amap_provider_null"},
		{true, true, ChinaKeyAbsent, "amap_provider_null"}, // provider 关闭优先级最高
	}
	for _, c := range cases {
		if got := amapUnavailableReason(c.providerOff, c.clientMissing, c.keyState); got != c.want {
			t.Errorf("amapUnavailableReason(%v,%v,%q) = %q, want %q",
				c.providerOff, c.clientMissing, c.keyState, got, c.want)
		}
	}
}

func TestCandidatesCacheHit(t *testing.T) {
	amapHit := ForwardHit{Name: "杭州西湖", Lon: 120.13, Lat: 30.25, Provider: amapProvider}
	amap := &fakeAmapForwarder{hits: []ForwardHit{amapHit}}
	intl := &fakeIntlForwarder{}
	s, cache := newTestService(amap, intl, "k")

	ctx := context.Background()
	first, _ := s.Candidates(ctx, "西湖", "")
	if len(first) != 1 || amap.calls != 1 || cache.puts != 1 {
		t.Fatalf("首次查询应打上游并回填缓存: hits=%d calls=%d puts=%d", len(first), amap.calls, cache.puts)
	}
	// 上游此后故障，但缓存命中：仍应返回结果且不再调用上游
	amap.err = errors.New("上游宕机")
	second, _ := s.Candidates(ctx, "西湖", "")
	if amap.calls != 1 {
		t.Errorf("重复查询应命中缓存，高德调用次数仍应为 1，实际 %d", amap.calls)
	}
	if len(second) != 1 || second[0].Name != "杭州西湖" {
		t.Fatalf("缓存命中应返回同一结果，得到 %+v", second)
	}
	if cache.puts != 1 {
		t.Errorf("缓存命中不应重复写缓存，实际 writes=%d", cache.puts)
	}
}

func TestCandidatesEmptyResultIsCached(t *testing.T) {
	// 无结果同样入缓存：避免对无意义查询反复打上游（上游配额保护）
	amap := &fakeAmapForwarder{hits: nil}
	intl := &fakeIntlForwarder{hits: nil}
	s, cache := newTestService(amap, intl, "k")
	ctx := context.Background()
	_, _ = s.Candidates(ctx, "不存在的地名xyz", "")
	firstIntl := intl.calls
	_, _ = s.Candidates(ctx, "不存在的地名xyz", "")
	if intl.calls != firstIntl {
		t.Errorf("空结果应被缓存，Nominatim 不应二次调用：%d → %d", firstIntl, intl.calls)
	}
	if cache.puts < 2 {
		t.Errorf("高德与 Nominatim 各应回填一次缓存，实际 %d", cache.puts)
	}
}

// stubConfigLoader 返回固定配置（替代 system_map_config 读库）。
type stubConfigLoader struct{ cfg MapConfig }

func (s stubConfigLoader) Load(context.Context) MapConfig { return s.cfg }

// ---- HTTP 层 ----

func newMapSearchRouter(s *MapSearchService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/map/search", s.Search)
	r.GET("/map/providers", s.Providers)
	return r
}

func doGet(r *gin.Engine, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func TestSearchHandlerRejectsBlankQuery(t *testing.T) {
	s, _ := newTestService(&fakeAmapForwarder{}, &fakeIntlForwarder{}, "k")
	r := newMapSearchRouter(s)
	for _, target := range []string{
		"/map/search",
		"/map/search?q=",
		"/map/search?q=%20%20%20", // 全空白（半角）
		"/map/search?q=%E3%80%80", // 全空白（全角 U+3000）
		"/map/search?q=" + strings.Repeat("%E8%A5%BF", 201), // 超长（201 汉字）
	} {
		w := doGet(r, target)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s 应 400，实际 %d，body=%s", target, w.Code, w.Body.String())
			continue
		}
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Errorf("%s 响应非 JSON: %v", target, err)
			continue
		}
		if body.Error.Code != "INVALID_PARAMS" {
			t.Errorf("%s 错误码应为 INVALID_PARAMS，实际 %q", target, body.Error.Code)
		}
	}
}

func TestSearchHandlerCandidatesShape(t *testing.T) {
	amap := &fakeAmapForwarder{hits: []ForwardHit{{Name: "杭州西湖", Lon: 120.13, Lat: 30.25, Provider: amapProvider}}}
	s, _ := newTestService(amap, &fakeIntlForwarder{}, "k")
	r := newMapSearchRouter(s)

	w := doGet(r, "/map/search?q=%E8%A5%BF%E6%B9%96")
	if w.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Candidates []MapCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解析失败: %v (%s)", err, w.Body.String())
	}
	if len(body.Candidates) != 1 {
		t.Fatalf("期望 1 个候选，实际 %+v", body.Candidates)
	}
	c := body.Candidates[0]
	if c.Name != "杭州西湖" || c.Provider != amapProvider {
		t.Errorf("候选字段不符: %+v", c)
	}
	if math.Abs(c.Lon-120.13) > 1e-9 || math.Abs(c.Lat-30.25) > 1e-9 {
		t.Errorf("候选坐标不符: %+v", c)
	}
	if w.Header().Get("X-Map-Search-Degraded") != "" {
		t.Errorf("正常路径不应带降级头，实际 %q", w.Header().Get("X-Map-Search-Degraded"))
	}
}

func TestSearchHandlerEmptyResultIsEmptyArray(t *testing.T) {
	// 无结果必须是 200 + candidates: []（不是 null，也不是错误）
	s, _ := newTestService(&fakeAmapForwarder{hits: nil}, &fakeIntlForwarder{hits: nil}, "k")
	w := doGet(newMapSearchRouter(s), "/map/search?q=West%20Lake")
	if w.Code != http.StatusOK {
		t.Fatalf("无结果应 200，实际 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"candidates":[]`) {
		t.Fatalf("无结果应为空数组，实际 body=%s", w.Body.String())
	}
}

func TestSearchHandlerDegradedHeaderOnNoKey(t *testing.T) {
	// 当前部署实况：无高德 Key + 中国地名 → 降级头可见，且仍返回 200
	intl := &fakeIntlForwarder{hits: []ForwardHit{{Name: "West Lake", Lon: 120.14, Lat: 30.24, Provider: nominatimProvider}}}
	s, _ := newTestService(&fakeAmapForwarder{}, intl, "")
	w := doGet(newMapSearchRouter(s), "/map/search?q=%E8%A5%BF%E6%B9%96")
	if w.Code != http.StatusOK {
		t.Fatalf("无 Key 也必须 200（绝不 500），实际 %d", w.Code)
	}
	if got := w.Header().Get("X-Map-Search-Degraded"); got != "amap_absent" {
		t.Errorf("降级头应为 amap_absent，实际 %q", got)
	}
}

func TestSearchHandlerDisplayProviderConversion(t *testing.T) {
	// provider=osm 时，高德（GCJ-02）结果需反变换为 WGS-84
	const gcjLng, gcjLat = 116.403672, 39.910095
	amap := &fakeAmapForwarder{hits: []ForwardHit{{Name: "天安门", Lon: gcjLng, Lat: gcjLat, Provider: amapProvider}}}
	s, _ := newTestService(amap, &fakeIntlForwarder{}, "k")
	w := doGet(newMapSearchRouter(s), "/map/search?q=%E5%A4%A9%E5%AE%89%E9%97%A8&provider=osm")
	var body struct {
		Candidates []MapCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if len(body.Candidates) != 1 {
		t.Fatalf("期望 1 个候选，实际 %+v", body.Candidates)
	}
	if math.Abs(body.Candidates[0].Lon-116.397428) > 1e-4 || math.Abs(body.Candidates[0].Lat-39.908692) > 1e-4 {
		t.Errorf("provider=osm 应返回 WGS-84，实际 %+v", body.Candidates[0])
	}
}

func TestProvidersHandler(t *testing.T) {
	s, _ := newTestService(&fakeAmapForwarder{}, &fakeIntlForwarder{}, "")
	w := doGet(newMapSearchRouter(s), "/map/providers")
	if w.Code != http.StatusOK {
		t.Fatalf("应 200，实际 %d", w.Code)
	}
	var body struct {
		China   string `json:"china"`
		Intl    string `json:"intl"`
		Default string `json:"default"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if body.China != defaultChinaProvider || body.Intl != defaultIntlProvider || body.Default != "auto" {
		t.Fatalf("响应不符: %+v", body)
	}
	// 契约要求不含密钥
	if strings.Contains(w.Body.String(), "key") {
		t.Errorf("/map/providers 不得包含密钥字段: %s", w.Body.String())
	}
}

// TestCandidatesExposeShortPlace 锁死 ShortPlace 的**接线**（真跑 Candidates）。
//
// 🔴 这条测试是被真实缺陷逼出来的：ShortPlaceName 写了 195 行 + 161 行测试，
// 但**全仓零调用方** —— /map/search 的候选只给 name(=完整地址) 与
// address(=同一个完整地址)，于是前端保存时 place === address，
// 短地名功能等于没上线（且退化成迁移 00047 之前的形态）。
//
// ⚠️ 断言方式的关键：必须**真调用 MapSearchService.Candidates**。
// 自己重一遍字段映射的写法锁不住回归 —— 漏掉 Candidates 里那行赋值时它照样通过。
// 只测 ShortPlaceName 本身同样不够（那是纯函数，与接线无关）。
func TestCandidatesExposeShortPlace(t *testing.T) {
	amap := &fakeAmapForwarder{hits: []ForwardHit{{
		Name:     "北京市东城区景山前街 4 号",
		Lon:      116.403744,
		Lat:      39.910103,
		Provider: amapProvider,
		Address:  "北京市东城区景山前街 4 号",
	}}}
	svc, _ := newTestService(amap, nil, "k")

	got, _ := svc.Candidates(context.Background(), "景山前街", amapProvider)
	if len(got) == 0 {
		t.Fatal("候选为空：fakeAmapForwarder 未能返回结果（测试装配有问题，不是产品问题）")
	}
	c := got[0]
	if c.ShortPlace != "景山前街" {
		t.Fatalf("Candidates 未填充 ShortPlace 或取值不对:\n  got  %q\n  want %q",
			c.ShortPlace, "景山前街")
	}
	// 关键回归点：place(ShortPlace) 与 address(完整地址) **必须不同** ——
	// 两者相同时说明又退回了「place 存完整地址」的老形态。
	if c.ShortPlace == c.Address {
		t.Fatalf("ShortPlace 与 Address 相同（都是 %q）—— 短地名未生效，"+
			"媒体 place 会被写成完整地址", c.ShortPlace)
	}
}
