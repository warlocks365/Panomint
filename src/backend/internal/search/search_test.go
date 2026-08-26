package search

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- 参数解析器 ----

func queryGetter(raw string) func(string) string {
	v, _ := url.ParseQuery(raw)
	return v.Get
}

func TestParseParams(t *testing.T) {
	p, err := ParseParams(queryGetter("q=西湖 游船&tag=旅行&place=杭州&type=photo&favorites=true&date_after=2024-01-01&date_before=2024-12-31&limit=30"), "u1")
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if p.Q != "西湖 游船" || p.Tag != "旅行" || p.Place != "杭州" || p.Type != "photo" || !p.Favorites || !p.HasAfter || !p.HasBefore {
		t.Fatalf("解析结果错误: %+v", p)
	}
	if p.Limit != 30 || p.UserID != "u1" {
		t.Fatalf("limit/userID 错误: %+v", p)
	}
	// date_before 纯日期按闭区间顺延一天
	wantBefore := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !p.DateBefore.Equal(wantBefore) {
		t.Fatalf("date_before 应顺延到次日 0 点，实际 %v", p.DateBefore)
	}
	// RFC3339 原样使用
	p2, err := ParseParams(queryGetter("date_before="+url.QueryEscape("2024-06-01T12:00:00Z")), "u1")
	if err != nil || p2.DateBefore.Hour() != 12 {
		t.Fatalf("RFC3339 date_before 解析错误: %v %+v", err, p2)
	}
	// 非法 type / 日期
	if _, err := ParseParams(queryGetter("type=audio"), "u1"); !errors.Is(err, ErrInvalidType) {
		t.Fatalf("非法 type 应报 ErrInvalidType，实际 %v", err)
	}
	if _, err := ParseParams(queryGetter("date_after=2024/01/01"), "u1"); !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("非法 date_after 应报 ErrInvalidDate，实际 %v", err)
	}
}

// ---- buildWhere 参数组合 ----

func TestBuildWhere(t *testing.T) {
	// 基础：仅权限 + 软删
	where, args := buildWhere(SearchParams{UserID: "u1"}, nil, nil)
	if !strings.Contains(where, "m.deleted_at IS NULL") {
		t.Errorf("缺软删过滤: %s", where)
	}
	if !strings.Contains(where, "m.space = 'personal' AND m.owner_id = $1") ||
		!strings.Contains(where, "shared_space_members sm WHERE sm.user_id = $1") {
		t.Errorf("空间可见性条件错误: %s", where)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Errorf("权限参数错误: %v", args)
	}

	// 全组合：q 双 token + tag + 日期 + place + type + favorites
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p := SearchParams{
		UserID: "u1", Q: "西湖 游船", Tag: "旅行", Place: "杭州",
		HasAfter: true, DateAfter: after, HasBefore: true, DateBefore: before,
		Type: "photo", Favorites: true,
	}
	where, args = buildWhere(p, nil, nil)
	checks := []string{
		"m.filename % $", "m.place % $", "m.folder_path ILIKE", "t.name % $", // q trgm
		"media_tags mt JOIN tags t",         // tag EXISTS
		"m.taken_at >= $", "m.taken_at < $", // 日期
		"m.type = $", "m.is_360 = false", // type
		"a.type = 'favorites'", // favorites 相册成员
	}
	for _, c := range checks {
		if !strings.Contains(where, c) {
			t.Errorf("WHERE 缺少 %q: %s", c, where)
		}
	}
	// 参数数：userID + 2 q token + tag + after + before + place + type = 8
	if len(args) != 8 {
		t.Errorf("参数个数应为 8，实际 %d: %v", len(args), args)
	}

	// type=360 仅 is_360
	where, _ = buildWhere(SearchParams{UserID: "u1", Type: "360"}, nil, nil)
	if !strings.Contains(where, "m.is_360 = true") || strings.Contains(where, "m.type =") {
		t.Errorf("360 过滤错误: %s", where)
	}
}

func TestBuildWherePlaceGeoFallback(t *testing.T) {
	p := SearchParams{UserID: "u1", Place: "西湖"}
	// 文本模式：trgm
	where, _ := buildWhere(p, nil, nil)
	if !strings.Contains(where, "m.place % $2") {
		t.Errorf("文本模式应走 place trgm: %s", where)
	}
	if strings.Contains(where, "ST_DWithin") {
		t.Errorf("文本模式不应含 ST_DWithin: %s", where)
	}
	// 地理降级模式：ST_DWithin 5km
	center := &GeoCenter{Lon: 120.15, Lat: 30.27}
	where, args := buildWhere(p, nil, center)
	if !strings.Contains(where, "ST_DWithin(m.gps::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 5000)") {
		t.Errorf("降级模式应走 5km 半径检索: %s", where)
	}
	if strings.Contains(where, "m.place %") {
		t.Errorf("降级模式不应再做 place 文本匹配: %s", where)
	}
	if len(args) != 3 || args[1] != 120.15 || args[2] != 30.27 {
		t.Errorf("降级参数错误: %v", args)
	}
}

func TestBuildWhereSemanticRecallSlot(t *testing.T) {
	p := SearchParams{UserID: "u1", Q: "猫"}
	// 无召回：纯结构化
	where, _ := buildWhere(p, nil, nil)
	if strings.Contains(where, "ANY(") {
		t.Errorf("无召回时不应拼 OR 并集: %s", where)
	}
	// 有召回 ID：OR 并集包装
	where, args := buildWhere(p, []string{"id-a", "id-b"}, nil)
	if !strings.Contains(where, "OR m.id = ANY($3::uuid[])") {
		t.Errorf("召回应以 OR 并集拼接: %s", where)
	}
	if ids, ok := args[len(args)-1].([]string); !ok || len(ids) != 2 {
		t.Errorf("召回 ID 参数错误: %v", args)
	}
}

// ---- 降级判定 + resolver（mock）----

func TestNeedGeoFallback(t *testing.T) {
	cases := []struct {
		place  string
		total  int
		hasGPS bool
		want   bool
	}{
		{"西湖", 0, true, true},   // 触发降级
		{"西湖", 5, true, false},  // 文本有结果不降级
		{"西湖", 0, false, false}, // 库内无 GPS 媒体不降级
		{"", 0, true, false},    // 无 place 不降级
	}
	for _, c := range cases {
		if got := needGeoFallback(c.place, c.total, c.hasGPS); got != c.want {
			t.Errorf("needGeoFallback(%q,%d,%v) = %v，应 %v", c.place, c.total, c.hasGPS, got, c.want)
		}
	}
}

type fakeGeocoder struct {
	lon, lat float64
	ok       bool
	err      error
	calls    int
}

func (f *fakeGeocoder) Geocode(context.Context, string) (float64, float64, bool, error) {
	f.calls++
	return f.lon, f.lat, f.ok, f.err
}

func TestCachedResolverWithMockProvider(t *testing.T) {
	// 命中：透传坐标（nil Pool 跳过缓存）
	g := &fakeGeocoder{lon: 120.15, lat: 30.27, ok: true}
	r := &CachedResolver{Provider: g}
	lon, lat, ok, err := r.Resolve(context.Background(), "杭州")
	if err != nil || !ok || lon != 120.15 || lat != 30.27 {
		t.Fatalf("resolver 命中错误: %v %v %v %v", lon, lat, ok, err)
	}
	// 未命中：ok=false（不降级，按零结果返回）
	g2 := &fakeGeocoder{ok: false}
	r2 := &CachedResolver{Provider: g2}
	if _, _, ok, err := r2.Resolve(context.Background(), "不存在地名xyz"); err != nil || ok {
		t.Fatalf("未命中应 ok=false,nil err，实际 %v %v", ok, err)
	}
	// 提供者错误：向上传递
	g3 := &fakeGeocoder{err: errors.New("上游超时")}
	r3 := &CachedResolver{Provider: g3}
	if _, _, _, err := r3.Resolve(context.Background(), "x"); err == nil {
		t.Fatal("提供者错误应向上传递")
	}
	// 无提供者：静默不降级
	r4 := &CachedResolver{}
	if _, _, ok, err := r4.Resolve(context.Background(), "x"); err != nil || ok {
		t.Fatalf("无提供者应 ok=false,nil err，实际 %v %v", ok, err)
	}
}

func TestNominatimGeocoder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") == "空" {
			w.Write([]byte(`[]`))
			return
		}
		w.Write([]byte(`[{"lon":"120.155","lat":"30.274","display_name":"杭州"}]`))
	}))
	defer srv.Close()
	g := NominatimGeocoder{BaseURL: srv.URL}
	lon, lat, ok, err := g.Geocode(context.Background(), "杭州")
	if err != nil || !ok || lon != 120.155 || lat != 30.274 {
		t.Fatalf("nominatim 解析错误: %v %v %v %v", lon, lat, ok, err)
	}
	if _, _, ok, err := g.Geocode(context.Background(), "空"); err != nil || ok {
		t.Fatalf("空结果应 ok=false,nil err，实际 %v %v", ok, err)
	}
}

// ---- 游标 ----

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 26, 10, 20, 30, 123, time.UTC)
	cur := encodeCursor(ts, "abc-123")
	gotT, gotID, err := decodeCursor(cur)
	if err != nil || gotID != "abc-123" || gotT.UnixNano() != ts.UnixNano() {
		t.Fatalf("游标往返失败: %v %q %v", gotT, gotID, err)
	}
	if _, _, err := decodeCursor("!!!bad!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
}

// ---- Handler（httptest + gin 假上下文；仅覆盖不触库的参数校验分支）----

func doSearch(h *Handler, rawQuery string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1") })
	r.GET("/search", h.Search)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/search?"+rawQuery, nil)
	r.ServeHTTP(rec, req)
	return rec
}

func TestHandlerInvalidParams(t *testing.T) {
	h := &Handler{} // Store 为 nil：参数校验失败应在触库前 400
	if rec := doSearch(h, "type=audio"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 type 应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doSearch(h, "date_after=not-a-date"); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法日期应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
}
