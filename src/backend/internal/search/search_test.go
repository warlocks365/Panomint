package search

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/cursor"
)

// ---- Job000005：q 多 token OR + 相关度评分 ----

func TestBuildWhereOrAndScore(t *testing.T) {
	// 双 token：OR 召回（任一命中），不再逐 token AND
	where, score, _, args, _ := buildWhere(SearchParams{UserID: "u1", Q: "西湖 游船"}, nil, nil)
	if len(args) != 3 { // userID + 2 token（WHERE 与 score 复用同一占位符）
		t.Fatalf("参数应为 3（userID+2token），实际 %d: %v", len(args), args)
	}
	// 两个 token 条件组之间应是 OR 连接
	if !strings.Contains(where, ")\n\tOR (COALESCE(m.filename,'') ILIKE") {
		t.Errorf("多 token 应 OR 连接: %s", where)
	}
	// trgm 补充召回阈值
	if !strings.Contains(where, "similarity(COALESCE(m.filename,''), $2) >= 0.15") {
		t.Errorf("应含 trgm 补充召回条件: %s", where)
	}
	// 评分表达式：ILIKE 权重（::int）+ similarity 连续分。
	// 注意 filename / place / folder_path 必须 COALESCE 后再 ILIKE——三者可空，
	// 否则 NULL::int 会让整个 score 变 NULL，在 ORDER BY score DESC 下
	// 因 Postgres 默认 NULLS FIRST 被排到最前（Job000010 实测踩到）。
	for _, want := range []string{
		"10*(COALESCE(m.filename,'') ILIKE",
		"6*(COALESCE(m.place,'') ILIKE",
		"4*(COALESCE(m.folder_path,'') ILIKE",
		"2*similarity(COALESCE(m.filename,''), $2)",
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
	if _, scoreEmpty, _, _, _ := buildWhere(SearchParams{UserID: "u1"}, nil, nil); scoreEmpty != "" {
		t.Errorf("无 q 时 scoreExpr 应为空，实际 %q", scoreEmpty)
	}
	// 单 token 不拼 OR
	where1, _, _, _, _ := buildWhere(SearchParams{UserID: "u1", Q: "西湖"}, nil, nil)
	if strings.Count(where1, "COALESCE(m.filename,'') ILIKE") != 1 {
		t.Errorf("单 token 应仅一组命中条件: %s", where1)
	}
}

func TestScoredCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 27, 8, 30, 0, 456, time.UTC)
	// v3：四元组 (text_matched, score, taken_at, id)，分组键必须往返保真
	for _, tm := range []bool{true, false} {
		cur := cursor.EncodeScored(tm, 12.5, ts, "abc-123")
		gotTM, sc, gotT, gotID, err := cursor.DecodeScored(cur)
		if err != nil || sc != 12.5 || gotID != "abc-123" || gotTM != tm || gotT.UnixNano() != ts.UnixNano() {
			t.Fatalf("v3 游标往返失败(tm=%v): tm=%v sc=%v id=%q t=%v err=%v", tm, gotTM, sc, gotID, gotT, err)
		}
	}
	// v2 兼容：旧三元组无分组键，按 text_matched=true 解析（升级瞬间的跨版本续页不报错）
	legacy2 := base64.URLEncoding.EncodeToString([]byte("v2|7.5|" + ts.UTC().Format(time.RFC3339Nano) + "|abc-123"))
	if gotTM, sc, _, gotID, err := cursor.DecodeScored(legacy2); err != nil || !gotTM || sc != 7.5 || gotID != "abc-123" {
		t.Fatalf("v2 游标兼容失败: tm=%v sc=%v id=%q err=%v", gotTM, sc, gotID, err)
	}
	// 旧版（无 v2/v3 前缀）游标在评分模式下应拒绝（版本化隔离）
	legacy := cursor.Encode(ts, "abc-123")
	if _, _, _, _, err := cursor.DecodeScored(legacy); err == nil {
		t.Fatal("旧版游标在评分模式应报版本错误")
	}
	if _, _, _, _, err := cursor.DecodeScored("!!!bad!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	// v3 字段非法：拒绝
	bad := base64.URLEncoding.EncodeToString([]byte("v3|1|abc|" + ts.UTC().Format(time.RFC3339Nano) + "|abc"))
	if _, _, _, _, err := cursor.DecodeScored(bad); err == nil {
		t.Fatal("非法 score 应报错")
	}
}

// ---- 评分模式排序键：仅语义结果必须排在所有文本命中之后 ----

func TestScoredOrderByPutsTextMatchesFirst(t *testing.T) {
	got := scoredOrderBy(true)
	if !strings.HasPrefix(got, "text_matched DESC, ") {
		t.Fatalf("有文本命中时排序键必须以 text_matched DESC 打头（显式排序键，不依赖 score 大小关系）: %q", got)
	}
	if !strings.Contains(got, "score DESC NULLS LAST") {
		t.Fatalf("组内仍需按 score 降序: %q", got)
	}
	// 无文本命中表达式（如仅 person 过滤）时不应引用不存在的列
	if got := scoredOrderBy(false); strings.Contains(got, "text_matched") {
		t.Fatalf("无文本命中表达式时不得引用 text_matched: %q", got)
	}
}

// ---- 人物名匹配（q 通路）----

func TestPersonNameParticipatesInRecallAndScore(t *testing.T) {
	// 人物名权重必须严格高于语义权重，否则"搜爱因斯坦"时其他人像会以同分挤到同一位次
	if personNameScoreWeight <= semanticScoreWeight {
		t.Fatalf("人物名权重 %.1f 必须 > 语义权重 %.1f", personNameScoreWeight, semanticScoreWeight)
	}
	where, score, textMatch, args, whereN := buildWhere(SearchParams{UserID: "u1", Q: "爱因斯坦"}, nil, nil)
	if !strings.Contains(where, "FROM faces f JOIN people p ON p.id = f.person_id") {
		t.Errorf("WHERE 应含人物名匹配子句: %s", where)
	}
	if !strings.Contains(score, fmt.Sprintf("%g*(EXISTS(SELECT 1 FROM faces f JOIN people p", personNameScoreWeight)) {
		t.Errorf("score 应含 %g 分的人物名项: %s", personNameScoreWeight, score)
	}
	// 人物名属文本事实 → 必须计入 textMatchExpr（否则会被标成 semantic_only）
	if !strings.Contains(textMatch, "FROM faces f JOIN people p") {
		t.Errorf("textMatchExpr 应包含人物名子句: %s", textMatch)
	}
	// 人物名子句复用同一 token 占位符，不新增参数
	if len(args) != 2 || whereN != 2 {
		t.Errorf("人物名子句不应新增参数，实得 args=%v whereN=%d", args, whereN)
	}
	// people 为空（库内尚无命名人物）时不得报错：SQL 仍是合法 EXISTS
	if !strings.Contains(where, "COALESCE(p.name, '') <> ''") {
		t.Errorf("应兜住 people.name 可空: %s", where)
	}
}

// unionBranchPrefix 语义并集分支必须以此开头：软删 + 空间可见性 + 用户参数 $1。
//
// 这是**安全不变量**：语义召回来自 embed.Store.SearchByVector，那条查询只过滤
// deleted_at/embedding，**不做 owner/space 限定**（TopK 可命中全库任何人的媒体）。
// 并集分支若只写 `m.id = ANY(...)`，授权与软删会被完全绕过——
// 实测「按可见性条件可见媒体数为 0 的用户」仍能搜到他人个人空间的照片。
const unionBranchPrefix = "OR (m.deleted_at IS NULL AND ((m.space = 'personal' AND m.owner_id = $1)"

// 查询命中已命名人物时，语义并集必须被闸门抑制（否则搜"爱因斯坦"会混进林肯等同为人像的结果）。
func TestPersonIntentGatesSemanticUnion(t *testing.T) {
	hits := []RecallHit{{ID: "id-a", Similarity: 0.30}}
	where, _, _, _, whereN := buildWhere(SearchParams{UserID: "u1", Q: "爱因斯坦"}, hits, nil)
	if !strings.Contains(where, "OR (m.deleted_at IS NULL AND") || !strings.Contains(where, "m.id = ANY(") {
		t.Fatalf("语义召回仍应以 OR 并集拼接: %s", where)
	}
	if !strings.Contains(where, unionBranchPrefix) {
		t.Fatalf("语义并集分支必须复用空间可见性，否则会越权返回他人媒体: %s", where)
	}
	if !strings.Contains(where, "AND NOT (EXISTS(SELECT 1 FROM people pg") {
		t.Fatalf("人物意图闸门应 AND 在语义并集上（只作用于语义，不作用于文本 base）: %s", where)
	}
	// WHERE 引用的最大参数序号不得超过 whereN（count(*) 只传 where 参数，越界即报 expected N arguments）
	if max := maxParamIndex(t, where); max > whereN {
		t.Fatalf("WHERE 引用了 whereN(%d) 之外的参数 $%d: %s", whereN, max, where)
	}
}

// 排序键与游标元组必须严格同序同长度：排序键多一列而游标仍少一列，跨页就会重复/漏行。
// 输入形态与真实调用一致：textMatchExpr 由 buildWhere 返回（已自带一层括号），scoreExpr 是未加括号的求和式。
func TestScoredCursorShapeMatchesOrderKeys(t *testing.T) {
	ts := time.Date(2026, 8, 27, 8, 30, 0, 0, time.UTC)

	// 有文本命中：ORDER BY 四键 ↔ 游标 (bool, float8, timestamptz, uuid)
	if got := strings.Split(scoredOrderBy(true), ", "); len(got) != 4 {
		t.Fatalf("有文本命中应有 4 个排序键: %q", scoredOrderBy(true))
	}
	order := scoredOrderBy(true)
	if !strings.HasPrefix(order, "text_matched DESC") || !strings.Contains(order, "score DESC NULLS LAST") {
		t.Fatalf("排序键内容错误: %q", order)
	}
	sql4, params4 := scoredCursorWhere("(TM)", "SC + 1", true, 5, true, 7.5, ts, "abc")
	want4 := "(((TM)), (SC + 1), m.taken_at, m.id) < ($5::bool, $6::float8, $7::timestamptz, $8::uuid)"
	if sql4 != want4 {
		t.Fatalf("四元组游标 SQL 错误:\n got %s\nwant %s", sql4, want4)
	}
	if len(params4) != 4 || params4[0] != true || params4[1] != 7.5 || params4[3] != "abc" {
		t.Fatalf("四元组游标参数顺序错误: %v", params4)
	}

	// 无文本命中：ORDER BY 三键 ↔ 游标 (float8, timestamptz, uuid)
	if got := strings.Split(scoredOrderBy(false), ", "); len(got) != 3 {
		t.Fatalf("无文本命中应有 3 个排序键: %q", scoredOrderBy(false))
	}
	if strings.Contains(scoredOrderBy(false), "text_matched") {
		t.Fatalf("无文本命中时不得引用 text_matched 排序键: %q", scoredOrderBy(false))
	}
	sql3, params3 := scoredCursorWhere("", "SC + 1", false, 5, true, 7.5, ts, "abc")
	want3 := "((SC + 1), m.taken_at, m.id) < ($5::float8, $6::timestamptz, $7::uuid)"
	if sql3 != want3 {
		t.Fatalf("三元组游标 SQL 错误:\n got %s\nwant %s", sql3, want3)
	}
	if len(params3) != 3 || params3[0] != 7.5 {
		t.Fatalf("三元组游标参数顺序错误: %v", params3)
	}
}

// 文本命中表达式同时充当排序键 text_matched：内部绝不能出现未 COALESCE 的可空列比较。
// 否则整式为 NULL，在 `ORDER BY text_matched DESC`（Postgres 默认 NULLS FIRST）下
// 会把"仅语义"结果顶到所有文本命中之前——正好是这个 bug 本身（只读 SQL 走查实测踩到）。
// media.filename / place / folder_path 三列在 DDL 中均可空（filename VARCHAR(512) 无 NOT NULL），
// 漏掉任何一个都会重新引入该缺陷，也会让游标行比较对含 NULL 的行静默漏行。
func TestTextMatchExprIsNullSafe(t *testing.T) {
	_, score, textMatch, _, _ := buildWhere(SearchParams{UserID: "u1", Q: "极光"}, nil, nil)
	for _, bad := range []string{"m.filename ILIKE", "m.place ILIKE", "m.folder_path ILIKE", "similarity(m.filename,"} {
		if strings.Contains(textMatch, bad) {
			t.Errorf("textMatchExpr 含未 COALESCE 的可空列比较 %q: %s", bad, textMatch)
		}
	}
	if strings.Contains(score, "similarity(m.filename,") || strings.Contains(score, "10*(m.filename ILIKE") {
		t.Errorf("score 表达式含未 COALESCE 的 filename（会让整个 score 变 NULL）: %s", score)
	}
	// 排序键自带的兜底：score 用 NULLS LAST，text_matched 由 COALESCE 保证非 NULL
	order := scoredOrderBy(true)
	if !strings.Contains(order, "score DESC NULLS LAST") || !strings.HasPrefix(order, "text_matched DESC") {
		t.Errorf("排序键错误: %s", order)
	}
}

// maxParamIndex 取 SQL 中最大的 $N 序号（用于守住 count(*) 的参数边界）。
func maxParamIndex(t *testing.T, sql string) int {
	t.Helper()
	max := 0
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(sql, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("占位符解析失败: %s", m[0])
		}
		if n > max {
			max = n
		}
	}
	return max
}

// WHERE 引用的最大 $N 必须恒 ≤ whereN：count(*) 只接收 args[:whereN]，
// 越界会直接报 "expected N arguments"（语义 ids/sims 参数位于 whereN 之后，不得出现在 WHERE 里）。
func TestBuildWhereParamBoundary(t *testing.T) {
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	hits := []RecallHit{{ID: "id-a", Similarity: 0.30}, {ID: "id-b", Similarity: 0.20}}
	cases := []struct {
		name string
		p    SearchParams
		hits []RecallHit
		geo  *GeoCenter
	}{
		{"空条件", SearchParams{UserID: "u1"}, nil, nil},
		{"q 单 token", SearchParams{UserID: "u1", Q: "极光"}, nil, nil},
		{"q 多 token + 语义召回", SearchParams{UserID: "u1", Q: "爱因斯坦 夜景"}, hits, nil},
		{"全组合 + 语义召回", SearchParams{UserID: "u1", Q: "西湖 游船", Tag: "旅行",
			Person: "96c9593b-9507-41e3-93b0-c338d921acd9", Place: "杭州", Type: "photo",
			Favorites: true, HasAfter: true, DateAfter: after}, hits, nil},
		{"地理降级 + 语义召回", SearchParams{UserID: "u1", Q: "雪", Place: "西湖"}, hits,
			&GeoCenter{Lon: 120.15, Lat: 30.27}},
		{"person 姓名 + 语义召回", SearchParams{UserID: "u1", Person: "爱因斯坦"}, hits, nil},
	}
	for _, c := range cases {
		where, score, _, args, whereN := buildWhere(c.p, c.hits, c.geo)
		if whereN > len(args) {
			t.Errorf("%s: whereN=%d 超过参数总数 %d", c.name, whereN, len(args))
		}
		if max := maxParamIndex(t, where); max > whereN {
			t.Errorf("%s: WHERE 引用了 whereN(%d) 之外的 $%d", c.name, whereN, max)
		}
		// 评分表达式允许引用 whereN 之后的参数（ids/sims），但不得超过参数总数
		if max := maxParamIndex(t, score); max > len(args) {
			t.Errorf("%s: score 引用 $%d 超过参数总数 %d", c.name, max, len(args))
		}
	}
}

// ---- person 参数（§8）：UUID 走 person_id，非 UUID 走人物姓名 ----

func TestBuildWherePersonParam(t *testing.T) {
	// UUID：按 faces.person_id 过滤（前端 PeopleView → /search?person=<people.id>）
	p := SearchParams{UserID: "u1", Person: "96c9593b-9507-41e3-93b0-c338d921acd9"}
	where, score, _, args, whereN := buildWhere(p, nil, nil)
	if !strings.Contains(where, "f.person_id = $2::uuid") {
		t.Errorf("UUID 形式应按 person_id 过滤: %s", where)
	}
	if len(args) != 2 || whereN != 2 {
		t.Errorf("person 过滤应只占 1 个参数: %v whereN=%d", args, whereN)
	}
	// person 是过滤器而非关键词：不产生评分（保持 taken_at 排序原行为）
	if score != "" {
		t.Errorf("仅 person 过滤时不应产生评分表达式: %q", score)
	}
	// 非 UUID：按人物姓名模糊匹配（people / faces 为空时 EXISTS 恒 false，不报错）
	p2 := SearchParams{UserID: "u1", Person: "爱因斯坦"}
	where2, _, _, _, _ := buildWhere(p2, nil, nil)
	if !strings.Contains(where2, "JOIN people pn ON pn.id = f.person_id") ||
		!strings.Contains(where2, "COALESCE(pn.name, '') <> ''") {
		t.Errorf("非 UUID 形式应按人物姓名匹配: %s", where2)
	}
}

func TestIsUUID(t *testing.T) {
	cases := map[string]bool{
		"96c9593b-9507-41e3-93b0-c338d921acd9": true,
		"96C9593B-9507-41E3-93B0-C338D921ACD9": true,
		"爱因斯坦":                                 false,
		"":                                     false,
		"96c9593b-9507-41e3-93b0-c338d921acd":  false, // 35 位
		"96c9593b950741e393b0c338d921acd9":     false, // 无连字符
		"96c9593b-9507-41e3-93b0-c338d921acdg": false, // 非十六进制
	}
	for in, want := range cases {
		if got := isUUID(in); got != want {
			t.Errorf("isUUID(%q) = %v，期望 %v", in, got, want)
		}
	}
}

// ---- 参数解析器 ----

func queryGetter(raw string) func(string) string {
	v, _ := url.ParseQuery(raw)
	return v.Get
}

func TestParseParams(t *testing.T) {
	p, err := ParseParams(queryGetter("q=西湖 游船&tag=旅行&place=杭州&person=96c9593b-9507-41e3-93b0-c338d921acd9&type=photo&favorites=true&date_after=2024-01-01&date_before=2024-12-31&limit=30"), "u1")
	if err != nil {
		t.Fatalf("合法参数不应报错: %v", err)
	}
	if p.Q != "西湖 游船" || p.Tag != "旅行" || p.Place != "杭州" || p.Type != "photo" || !p.Favorites || !p.HasAfter || !p.HasBefore {
		t.Fatalf("解析结果错误: %+v", p)
	}
	if p.Person != "96c9593b-9507-41e3-93b0-c338d921acd9" {
		t.Fatalf("person 参数未解析: %+v", p)
	}
	if p.Limit != 30 || p.UserID != "u1" {
		t.Fatalf("limit/userID 错误: %+v", p)
	}
	// 未传 person：空值（不得报错，也不得产生过滤）
	if p3, err := ParseParams(queryGetter("q=雪"), "u1"); err != nil || p3.Person != "" {
		t.Fatalf("未传 person 应为空且不报错: %v %+v", err, p3)
	}
	// person 传人物姓名也可（非 UUID 走姓名匹配，不报错）
	if p4, err := ParseParams(queryGetter("person="+url.QueryEscape("爱因斯坦")), "u1"); err != nil || p4.Person != "爱因斯坦" {
		t.Fatalf("person 传姓名应被接受: %v %+v", err, p4)
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
	where, _, _, args, _ := buildWhere(SearchParams{UserID: "u1"}, nil, nil)
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

	// 全组合：q 双 token + tag + person + 日期 + place + type + favorites
	after := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	p := SearchParams{
		UserID: "u1", Q: "西湖 游船", Tag: "旅行", Person: "爱因斯坦", Place: "杭州",
		HasAfter: true, DateAfter: after, HasBefore: true, DateBefore: before,
		Type: "photo", Favorites: true,
	}
	where, _, _, args, _ = buildWhere(p, nil, nil)
	checks := []string{
		"COALESCE(m.filename,'') ILIKE", "COALESCE(m.place,'') ILIKE", "COALESCE(m.folder_path,'') ILIKE", "t.name ILIKE", // q 子串匹配
		"media_tags mt JOIN tags t",             // tag EXISTS
		"JOIN people pn ON pn.id = f.person_id", // person 姓名过滤
		"m.taken_at >= $", "m.taken_at < $",     // 日期
		"m.type = $", "m.is_360 = false", // type
		"a.type = 'favorites'", // favorites 相册成员
	}
	for _, c := range checks {
		if !strings.Contains(where, c) {
			t.Errorf("WHERE 缺少 %q: %s", c, where)
		}
	}
	// 参数数：userID + 2 q token + tag + person + after + before + place + type = 9
	if len(args) != 9 {
		t.Errorf("参数个数应为 9，实际 %d: %v", len(args), args)
	}

	// type=360 仅 is_360
	where, _, _, _, _ = buildWhere(SearchParams{UserID: "u1", Type: "360"}, nil, nil)
	if !strings.Contains(where, "m.is_360 = true") || strings.Contains(where, "m.type =") {
		t.Errorf("360 过滤错误: %s", where)
	}
}

func TestBuildWherePlaceGeoFallback(t *testing.T) {
	p := SearchParams{UserID: "u1", Place: "西湖"}
	// 文本模式：ILIKE 子串
	where, _, _, _, _ := buildWhere(p, nil, nil)
	if !strings.Contains(where, "m.place ILIKE '%' || $2 || '%'") {
		t.Errorf("文本模式应走 place ILIKE: %s", where)
	}
	if strings.Contains(where, "ST_DWithin") {
		t.Errorf("文本模式不应含 ST_DWithin: %s", where)
	}
	// 地理降级模式：ST_DWithin 5km
	center := &GeoCenter{Lon: 120.15, Lat: 30.27}
	where, _, _, args, _ := buildWhere(p, nil, center)
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
	where, _, _, _, _ := buildWhere(p, nil, nil)
	if strings.Contains(where, "ANY(") {
		t.Errorf("无召回时不应拼 OR 并集: %s", where)
	}
	// 有召回：OR 并集包装 + 相似度参与打分
	hits := []RecallHit{{ID: "id-a", Similarity: 0.30}, {ID: "id-b", Similarity: 0.20}}
	where, score, _, args, _ := buildWhere(p, hits, nil)
	if !strings.Contains(where, "OR (m.deleted_at IS NULL AND") || !strings.Contains(where, "m.id = ANY(") {
		t.Errorf("召回应以 OR 并集拼接: %s", where)
	}
	// 安全不变量：并集分支必须复用空间可见性（见 unionBranchPrefix 的说明）。
	if !strings.Contains(where, unionBranchPrefix) {
		t.Errorf("语义并集分支必须带软删与空间可见性，否则会越权返回他人媒体: %s", where)
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
	cur := cursor.Encode(ts, "abc-123")
	gotT, gotID, err := cursor.Decode(cur)
	if err != nil || gotID != "abc-123" || gotT.UnixNano() != ts.UnixNano() {
		t.Fatalf("游标往返失败: %v %q %v", gotT, gotID, err)
	}
	if _, _, err := cursor.Decode("!!!bad!!!"); err == nil {
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
