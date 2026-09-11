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

// ---- Job000005：q 多 token OR + 相关度评分 ----

func TestBuildWhereOrAndScore(t *testing.T) {
	// 双 token：OR 召回（任一命中），不再逐 token AND
	where, score, args, _ := buildWhere(SearchParams{UserID: "u1", Q: "西湖 游船"}, nil, nil)
	if len(args) != 3 { // userID + 2 token（WHERE 与 score 复用同一占位符）
		t.Fatalf("参数应为 3（userID+2token），实际 %d: %v", len(args), args)
	}
	// 两个 token 条件组之间应是 OR 连接
	if !strings.Contains(where, ")\n\tOR (m.filename ILIKE") {
		t.Errorf("多 token 应 OR 连接: %s", where)
	}
	// trgm 补充召回阈值
	if !strings.Contains(where, "similarity(m.filename, $2) >= 0.15") {
		t.Errorf("应含 trgm 补充召回条件: %s", where)
	}
	// 评分表达式：ILIKE 权重（::int）+ similarity 连续分。
	// 注意 place / folder_path 必须 COALESCE 后再 ILIKE——两者可空，
	// 否则 NULL::int 会让整个 score 变 NULL，在 ORDER BY score DESC 下
	// 因 Postgres 默认 NULLS FIRST 被排到最前（Job000010 实测踩到）。
	for _, want := range []string{
		"10*(m.filename ILIKE",
		"6*(COALESCE(m.place,'') ILIKE",
		"4*(COALESCE(m.folder_path,'') ILIKE",
		"2*similarity(m.filename, $2)",
		"max(similarity(t.name, $2))",
	} {
		if !strings.Contains(score, want) {
			t.Errorf("score 表达式缺少 %q: %s", want, score)
		}
	}
	if strings.Contains(score, "6*(m.place ILIKE") || strings.Contains(score, "4*(m.folder_path ILIKE") {
		t.Errorf("评分表达式对可空列必须先 COALESCE 再 ILIKE（否则 score 可能为 NULL）: %s", score)
	}
	// 无 q：scoreExpr 为空（调用方保持 taken_at 排序原行为）
	if _, scoreEmpty, _, _ := buildWhere(SearchParams{UserID: "u1"}, nil, nil); scoreEmpty != "" {
		t.Errorf("无 q 时 scoreExpr 应为空，实际 %q", scoreEmpty)
	}
	// 单 token 不拼 OR
	where1, _, _, _ := buildWhere(SearchParams{UserID: "u1", Q: "西湖"}, nil, nil)
	if strings.Count(where1, "m.filename ILIKE") != 1 {
		t.Errorf("单 token 应仅一组命中条件: %s", where1)
	}
}

func TestScoredCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 27, 8, 30, 0, 456, time.UTC)
	cur := encodeScoredCursor(12.5, ts, "abc-123")
	sc, gotT, gotID, err := decodeScoredCursor(cur)
	if err != nil || sc != 12.5 || gotID != "abc-123" || gotT.UnixNano() != ts.UnixNano() {
		t.Fatalf("v2 游标往返失败: %v %v %q %v", sc, gotT, gotID, err)
	}
	// 旧版（无 v2 前缀）游标在评分模式下应拒绝（版本化隔离）
	legacy := encodeCursor(ts, "abc-123")
	if _, _, _, err := decodeScoredCursor(legacy); err == nil {
		t.Fatal("旧版游标在评分模式应报版本错误")
	}
	if _, _, _, err := decodeScoredCursor("!!!bad!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
}

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
	where, _, args, _ := buildWhere(SearchParams{UserID: "u1"}, nil, nil)
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
	where, _, args, _ = buildWhere(p, nil, nil)
	checks := []string{
		"m.filename ILIKE", "m.place ILIKE", "m.folder_path ILIKE", "t.name ILIKE", // q 子串匹配
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
	where, _, _, _ = buildWhere(SearchParams{UserID: "u1", Type: "360"}, nil, nil)
	if !strings.Contains(where, "m.is_360 = true") || strings.Contains(where, "m.type =") {
		t.Errorf("360 过滤错误: %s", where)
	}
}

func TestBuildWherePlaceGeoFallback(t *testing.T) {
	p := SearchParams{UserID: "u1", Place: "西湖"}
	// 文本模式：ILIKE 子串
	where, _, _, _ := buildWhere(p, nil, nil)
	if !strings.Contains(where, "m.place ILIKE '%' || $2 || '%'") {
		t.Errorf("文本模式应走 place ILIKE: %s", where)
	}
	if strings.Contains(where, "ST_DWithin") {
		t.Errorf("文本模式不应含 ST_DWithin: %s", where)
	}
	// 地理降级模式：ST_DWithin 5km
	center := &GeoCenter{Lon: 120.15, Lat: 30.27}
	where, _, args, _ := buildWhere(p, nil, center)
	if !strings.Contains(where, "ST_DWithin(m.gps::geography, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography, 5000)") {
		t.Errorf("降级模式应走 5km 半径检索: %s", where)
	}
	if strings.Contains(where, "m.place ILIKE") {
		t.Errorf("降级模式不应再做 place 文本匹配: %s", where)
	}
	if len(args) != 3 || args[1] != 120.15 || args[2] != 30.27 {
		t.Errorf("降级参数错误: %v", args)
	}
}

func TestBuildWhereSemanticRecallSlot(t *testing.T) {
	p := SearchParams{UserID: "u1", Q: "猫"}
	// 无召回：纯结构化
	where, _, _, _ := buildWhere(p, nil, nil)
	if strings.Contains(where, "ANY(") {
		t.Errorf("无召回时不应拼 OR 并集: %s", where)
	}
	// 有召回：OR 并集包装 + 相似度参与打分
	hits := []RecallHit{{ID: "id-a", Similarity: 0.30}, {ID: "id-b", Similarity: 0.20}}
	where, score, args, _ := buildWhere(p, hits, nil)
	if !strings.Contains(where, "OR m.id = ANY(") {
		t.Errorf("召回应以 OR 并集拼接: %s", where)
	}
	if !strings.Contains(score, "array_position(") {
		t.Errorf("语义命中应以相似度参与打分（否则会被按时间排序埋没）: %s", score)
	}
	// 末尾两个参数依次为 ids(uuid[]) 与 sims(float8[])
	n := len(args)
	ids, ok := args[n-2].([]string)
	if !ok || len(ids) != 2 || ids[0] != "id-a" {
		t.Errorf("召回 ID 参数错误: %v", args[n-2])
	}
	sims, ok := args[n-1].([]float64)
	if !ok || len(sims) != 2 || sims[0] != 0.30 {
		t.Errorf("召回相似度参数错误: %v", args[n-1])
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
