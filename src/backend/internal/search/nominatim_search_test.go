package search

// Nominatim 正向多候选（Search）解析与限速合规单测 —— 对应契约 §7 GET /map/search 的国际源。
//
// 全部用 httptest 本地假上游，不依赖外网与数据库：测试服直连外网受限，
// 真实 Nominatim 调用留待部署后验证；本文件只锁「响应解析 + 请求构造 + 条款限速」这些纯逻辑。
//
// 覆盖：成功解析 / 空结果 / 字段缺失 / 上游错误码 / 非法 JSON / limit 钳制 /
//      UA 与 accept-language / ctx 取消 / 包级限速器（≥1s/请求）行为。

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// nsRecorder 记录假 Nominatim 收到的一次请求。
type nsRecorder struct {
	req *http.Request
}

// nsServer 起假 Nominatim：固定状态码与响应体，并记录请求。
func nsServer(t *testing.T, status int, body string) (*httptest.Server, *nsRecorder) {
	t.Helper()
	rec := &nsRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.req = r.Clone(context.Background())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// ---- 响应解析 ----

// TestNominatimSearchParsesCandidates 成功响应：display_name 优先、name 兜底、坐标按字符串解析。
func TestNominatimSearchParsesCandidates(t *testing.T) {
	defer disableNominatimRateLimit()() // 单测不等 1s

	body := `[
		{"display_name":"西湖, 杭州, 浙江","name":"西湖","lat":"30.2500","lon":"120.1500"},
		{"name":"Fallback Name","lat":"20.0","lon":"100.0"}
	]`
	srv, rec := nsServer(t, http.StatusOK, body)
	got, err := (&NominatimGeocoder{BaseURL: srv.URL}).Search(context.Background(), "西湖", 5)
	if err != nil {
		t.Fatalf("期望成功，得到 err=%v", err)
	}
	if len(got) != 2 {
		t.Fatalf("期望 2 个候选，得到 %d: %+v", len(got), got)
	}
	if got[0].Name != "西湖, 杭州, 浙江" {
		t.Errorf("候选名应取 display_name，实际 %q", got[0].Name)
	}
	if got[0].Lon != 120.15 || got[0].Lat != 30.25 {
		t.Errorf("首候选坐标不符: %v,%v", got[0].Lon, got[0].Lat)
	}
	if got[1].Name != "Fallback Name" {
		t.Errorf("display_name 缺失应回退 name 字段，实际 %q", got[1].Name)
	}

	// 请求构造：路径 + 查询参数 + 条款要求的 UA 与 accept-language
	if rec.req == nil {
		t.Fatal("假上游未收到请求")
	}
	if rec.req.URL.Path != "/search" {
		t.Errorf("请求路径错误: %q", rec.req.URL.Path)
	}
	q := rec.req.URL.Query()
	if q.Get("q") != "西湖" || q.Get("format") != "json" || q.Get("limit") != "5" {
		t.Errorf("请求参数不符: %v", q)
	}
	if q.Get("accept-language") == "" {
		t.Error("应携带 accept-language 以提升中文/多语命中率")
	}
	if ua := rec.req.Header.Get("User-Agent"); ua == "" {
		t.Error("Nominatim 使用条款要求携带可识别的 User-Agent")
	}
}

// TestNominatimSearchEmptyResult 上游 200 + 空数组：无结果不是错误（上层返回 200 + candidates: []）。
func TestNominatimSearchEmptyResult(t *testing.T) {
	defer disableNominatimRateLimit()()

	srv, _ := nsServer(t, http.StatusOK, `[]`)
	got, err := (&NominatimGeocoder{BaseURL: srv.URL}).Search(context.Background(), "不存在的地名xyz", 5)
	if err != nil {
		t.Fatalf("空结果不应报错，得到 %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("空结果应为 0 条，得到 %d: %+v", len(got), got)
	}
}

// TestNominatimSearchSkipsMalformedEntries 字段缺失/非法坐标：跳过单条，不影响其余候选。
func TestNominatimSearchSkipsMalformedEntries(t *testing.T) {
	defer disableNominatimRateLimit()()

	body := `[
		{"display_name":"坐标字段整体缺失"},
		{"display_name":"坐标为空串","lat":"","lon":""},
		{"display_name":"坐标非法","lat":"abc","lon":"def"},
		{"display_name":"正常候选","lat":"30.2741","lon":"120.1551"}
	]`
	srv, _ := nsServer(t, http.StatusOK, body)
	got, err := (&NominatimGeocoder{BaseURL: srv.URL}).Search(context.Background(), "西湖", 5)
	if err != nil {
		t.Fatalf("单条脏数据不应让整体失败，得到 %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("仅 1 条坐标合法，实际 %d: %+v", len(got), got)
	}
	if got[0].Name != "正常候选" {
		t.Errorf("保留的候选不符: %+v", got[0])
	}

	// 坐标全非法 → 空结果 + nil 错误（而非报错）
	srv2, _ := nsServer(t, http.StatusOK, `[{"display_name":"x","lat":"","lon":""}]`)
	if got, err := (&NominatimGeocoder{BaseURL: srv2.URL}).Search(context.Background(), "q", 5); err != nil || len(got) != 0 {
		t.Fatalf("全非法坐标应视为空结果，得到 err=%v / %d 条", err, len(got))
	}
}

// TestNominatimSearchNameFallbackToQuery 名称字段全缺时回退为查询词，保证候选 name 非空。
func TestNominatimSearchNameFallbackToQuery(t *testing.T) {
	defer disableNominatimRateLimit()()

	srv, _ := nsServer(t, http.StatusOK, `[{"lat":"30.2741","lon":"120.1551"}]`)
	got, err := (&NominatimGeocoder{BaseURL: srv.URL}).Search(context.Background(), "某地", 5)
	if err != nil {
		t.Fatalf("期望成功，得到 %v", err)
	}
	if len(got) != 1 || got[0].Name != "某地" {
		t.Fatalf("名称缺失应回退查询词，得到 %+v", got)
	}
}

// TestNominatimSearchUpstreamErrors 上游错误码 / 非法 JSON：返回错误由调用方降级。
func TestNominatimSearchUpstreamErrors(t *testing.T) {
	defer disableNominatimRateLimit()()

	srv503, _ := nsServer(t, http.StatusServiceUnavailable, `upstream down`)
	if _, err := (&NominatimGeocoder{BaseURL: srv503.URL}).Search(context.Background(), "西湖", 5); err == nil ||
		!strings.Contains(err.Error(), "503") {
		t.Fatalf("非 200 应返回含状态码的错误，实际 %v", err)
	}

	srvBad, _ := nsServer(t, http.StatusOK, `not-json`)
	if _, err := (&NominatimGeocoder{BaseURL: srvBad.URL}).Search(context.Background(), "西湖", 5); err == nil {
		t.Fatal("非法 JSON 应返回错误")
	}

	// 单点接口（place 降级复用）在非 200 时不得返回 ok=true
	srv500, _ := nsServer(t, http.StatusInternalServerError, ``)
	if _, _, ok, err := (&NominatimGeocoder{BaseURL: srv500.URL}).Geocode(context.Background(), "西湖"); err == nil || ok {
		t.Fatalf("非 200 应 err != nil 且 ok=false，实际 ok=%v err=%v", ok, err)
	}
}

// TestNominatimSearchLimitClamped limit 缺省与钳制（防滥用、避免上游拒绝）。
func TestNominatimSearchLimitClamped(t *testing.T) {
	defer disableNominatimRateLimit()()

	cases := []struct {
		in   int
		want string
	}{
		{0, "5"},  // 未指定 → 默认
		{-3, "5"}, // 非法 → 默认
		{3, "3"},  // 正常透传
		{1000, strconv.Itoa(maxPlaceSearchLimit)}, // 超上限 → 钳制
	}
	for _, c := range cases {
		srv, rec := nsServer(t, http.StatusOK, `[]`)
		if _, err := (&NominatimGeocoder{BaseURL: srv.URL}).Search(context.Background(), "q", c.in); err != nil {
			t.Fatalf("limit=%d 期望成功，得到 %v", c.in, err)
		}
		if got := rec.req.URL.Query().Get("limit"); got != c.want {
			t.Errorf("limit=%d 应请求上游 limit=%s，实际 %q", c.in, c.want, got)
		}
	}
}

// ---- 条款合规：限速器（Nominatim 要求 ≥1 req/s）----

// TestNominatimMinIntervalMeetsTerms 最小间隔常量不得低于条款下限（1s）。
func TestNominatimMinIntervalMeetsTerms(t *testing.T) {
	if nominatimMinInterval < time.Second {
		t.Fatalf("Nominatim 使用条款要求 ≥1s/请求，实际间隔 %v", nominatimMinInterval)
	}
	if nominatimUserAgent == "" {
		t.Fatal("必须声明可识别的 User-Agent")
	}
}

// TestMinIntervalLimiterPacesRequests 最小间隔限流：连续 3 次需跨越 2 个间隔。
func TestMinIntervalLimiterPacesRequests(t *testing.T) {
	l := newMinIntervalLimiter(60 * time.Millisecond)
	ctx := context.Background()
	start := time.Now()
	for i := 0; i < 3; i++ {
		if err := l.wait(ctx); err != nil {
			t.Fatalf("第 %d 次 wait 失败: %v", i+1, err)
		}
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("3 次请求应至少跨越 2 个 60ms 间隔，实际仅 %v", elapsed)
	}
}

// TestMinIntervalLimiterCtxCancel 排队期间 ctx 超时应立即返回，不占用上游配额。
func TestMinIntervalLimiterCtxCancel(t *testing.T) {
	l := newMinIntervalLimiter(time.Second)
	if err := l.wait(context.Background()); err != nil { // 占住第一个时间槽
		t.Fatalf("首次 wait 失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := l.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("期望可回溯 context.DeadlineExceeded，实际 %v", err)
	}
}

// TestMinIntervalLimiterNilAllowsAll nil 限速器 / 非正间隔视为不限流（便于单测与关闭开关）。
func TestMinIntervalLimiterNilAllowsAll(t *testing.T) {
	var l *minIntervalLimiter
	if err := l.wait(context.Background()); err != nil {
		t.Fatalf("nil 限速器应直接放行，实际 %v", err)
	}
	if err := newMinIntervalLimiter(0).wait(context.Background()); err != nil {
		t.Fatalf("零间隔应直接放行，实际 %v", err)
	}
}

// TestDisableNominatimRateLimitRestoresHook 关闭钩子必须还原调用前的限速器
// （否则单测会污染同包其他用例的生产语义）。
func TestDisableNominatimRateLimitRestoresHook(t *testing.T) {
	old := nominatimLimiter
	l := newMinIntervalLimiter(time.Second)
	nominatimLimiter = l
	defer func() { nominatimLimiter = old }()

	restore := disableNominatimRateLimit()
	if nominatimLimiter != nil {
		t.Fatal("关闭后限速器应为 nil")
	}
	restore()
	if nominatimLimiter != l {
		t.Fatal("restore 应还原调用前的限速器")
	}
}
