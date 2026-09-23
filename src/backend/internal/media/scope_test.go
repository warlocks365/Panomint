package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 作用域矩阵单测（T-018 越权修复的回归网）。
//
// 这个 bug 之所以能存在，根因是**从没有测试断言过作用域矩阵**：
// 旧实现里"space 缺省"等于"不加任何属主条件"，而 SQL 不会因此报错、只会多返回数据。
// 因此这里的断言必须钉住两类性质：
//  1. 收敛性：缺省/非法/缺主体一律**变窄**（空结果或拒绝），绝不变宽；
//  2. 单一真源：/media 与 /media/duplicates 的谓词必须逐字相同（防止再次各写一份而漂移）。

// scopeReq 用真实 handler 发请求；Store 为 nil，故只有"在触库前就被拦掉"的分支能跑到。
func scopeReq(h *Handler, userID, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", userID); c.Set("role", "viewer") })
	r.GET("/media", h.List)
	r.GET("/media/date-histogram", h.DateHistogram)
	r.GET("/media/duplicates", h.Duplicates)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func errEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

// ---- ResolveMediaScope 全矩阵 ----

func TestResolveMediaScopeMatrix(t *testing.T) {
	cases := []struct {
		name                            string
		space, userID                   string
		wantSpace, wantOwner, wantMemID string
		wantErr                         error
	}{
		{"缺省 → 本人个人空间", "", "u1", "personal", "u1", "", nil},
		{"显式 personal → 本人", "personal", "u1", "personal", "u1", "", nil},
		{"shared → 以本人身份做成员判定", "shared", "u1", "shared", "", "u1", nil},

		// 无身份：绝不能退化成"没有属主条件"，必须拒绝
		{"缺省且无身份 → 拒绝", "", "", "", "", "", ErrMissingUser},
		{"personal 且无身份 → 拒绝", "personal", "", "", "", "", ErrMissingUser},
		{"shared 且无身份 → 拒绝", "shared", "", "", "", "", ErrMissingUser},

		// 枚举外：不走 SQL（否则回显 PG 枚举原文），一律拒绝
		{"枚举外 → 拒绝", "bogus", "u1", "", "", "", ErrInvalidSpace},
		{"大小写不宽容", "Personal", "u1", "", "", "", ErrInvalidSpace},
		{"前导空白不 trim", " personal", "u1", "", "", "", ErrInvalidSpace},
		{"纯空白 → 拒绝", " ", "u1", "", "", "", ErrInvalidSpace},
		{"空串以外的怪值 → 拒绝", "*", "u1", "", "", "", ErrInvalidSpace},
	}
	for _, tc := range cases {
		got, err := ResolveMediaScope(tc.space, tc.userID)
		if err != tc.wantErr {
			t.Fatalf("%s: err=%v，期望 %v", tc.name, err, tc.wantErr)
		}
		if tc.wantErr != nil {
			continue
		}
		if got.Space != tc.wantSpace || got.OwnerID != tc.wantOwner || got.MemberID != tc.wantMemID {
			t.Fatalf("%s: got=%+v，期望 space=%q owner=%q member=%q",
				tc.name, got, tc.wantSpace, tc.wantOwner, tc.wantMemID)
		}
	}
}

// ---- scopeConds 谓词本体 ----

func TestScopeCondsPersonalBindsOwner(t *testing.T) {
	conds, args := scopeConds(MediaScope{Space: "personal", OwnerID: "u1"})
	got := strings.Join(conds, " AND ")
	if !strings.Contains(got, "m.space = 'personal'") || !strings.Contains(got, "m.owner_id = $1") {
		t.Fatalf("本人作用域谓词不对: %q", got)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Fatalf("args 应为 [u1]，实际 %v", args)
	}
}

func TestScopeCondsSharedRequiresMembershipOrOwnership(t *testing.T) {
	conds, args := scopeConds(MediaScope{Space: "shared", MemberID: "u1"})
	got := strings.Join(conds, " AND ")
	// 关键：shared 不能只写成 m.space='shared'（那等于"所有共享媒体"），必须带成员/属主判定
	if !strings.Contains(got, "m.space = 'shared'") || !strings.Contains(got, "shared_space_members") {
		t.Fatalf("共享作用域必须带成员判定: %q", got)
	}
	if !strings.Contains(got, "shared_space ss WHERE ss.owner_id = $1") {
		t.Fatalf("共享作用域必须同时认空间属主: %q", got)
	}
	if strings.Count(got, "$1") != 2 {
		t.Fatalf("两个 EXISTS 必须绑同一个 $1（否则参数错位）: %q", got)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Fatalf("args 应为 [u1]，实际 %v", args)
	}
}

func TestScopeCondsUnprovableScopeIsFailClosed(t *testing.T) {
	for _, s := range []MediaScope{
		{},                  // 零值：忘了解析作用域
		{Space: "personal"}, // 缺主体
		{Space: "shared"},   // 缺主体
		{Space: "bogus", OwnerID: "u1"},
		{Space: "shared", OwnerID: "u1"}, // 主体放错字段：共享档要 MemberID
	} {
		conds, args := scopeConds(s)
		if len(args) != 0 {
			t.Fatalf("%+v 应不产生参数，实际 %v", s, args)
		}
		if got := strings.Join(conds, " AND "); got != failClosed {
			t.Fatalf("%+v 应收敛为 %q（空结果），实际 %q —— 未证明安全的作用域绝不允许放行", s, failClosed, got)
		}
	}
}

// ---- 组装进 WHERE：绝不允许出现"只剩软删条件"的形态 ----

func TestBuildWhereAlwaysScoped(t *testing.T) {
	// 零值 ListParams 模拟"调用方忘了解析作用域"
	where, args := (&ListParams{}).buildWhere()
	if !strings.Contains(where, failClosed) {
		t.Fatalf("未解析作用域的 WHERE 必须收敛为空结果，实际 %q", where)
	}
	if len(args) != 0 {
		t.Fatalf("空作用域不应产生参数，实际 %v", args)
	}
	if strings.Contains(where, "m.owner_id") || strings.Contains(where, "m.space") {
		t.Fatalf("空作用域不该凭空拼出属主/空间条件: %q", where)
	}
}

// TestEmptySpaceConvergesToOwnerScopedWhere 直击历史事故：缺省 space 的最终 SQL 必须带属主条件。
func TestEmptySpaceConvergesToOwnerScopedWhere(t *testing.T) {
	scope, err := ResolveMediaScope("", "u1")
	if err != nil {
		t.Fatal(err)
	}
	where, args := (&ListParams{Scope: scope}).buildWhere()
	if !strings.Contains(where, "m.owner_id = $1") {
		t.Fatalf("缺省 space 必须按属主收窄，实际 %q", where)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Fatalf("args 应为 [u1]，实际 %v", args)
	}
	if strings.Contains(where, failClosed) {
		t.Fatalf("缺省 space 是合法输入，不该被判为空结果: %q", where)
	}
}

// TestBuildWherePlaceholderNumbering 作用域谓词被插到最前面，占位符编号必须连续无空洞。
func TestBuildWherePlaceholderNumbering(t *testing.T) {
	p := ListParams{
		Scope:  MediaScope{Space: "personal", OwnerID: "u1"},
		Folder: "/a",
		Type:   "photo",
		Tag:    "t",
		Date:   "2024-01",
	}
	where, args := p.buildWhere()
	if !strings.Contains(where, "m.deleted_at IS NULL") {
		t.Fatalf("软删条件丢失: %q", where)
	}
	// scope(1) + folder(1) + type(1) + tag(1) + date(2)
	if len(args) != 6 {
		t.Fatalf("参数个数应为 6，实际 %d: %v", len(args), args)
	}
	seen := map[int]bool{}
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(where, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("占位符解析失败: %q", m[0])
		}
		seen[n] = true
	}
	for i := 1; i <= len(args); i++ {
		if !seen[i] {
			t.Fatalf("占位符 $%d 缺失（编号必须连续，否则参数错位到别的列上）: %q", i, where)
		}
	}
	if len(seen) != len(args) {
		t.Fatalf("占位符集合与 args 不匹配（占位符 %v，args %d 个）: %q", seen, len(args), where)
	}
	if args[0] != "u1" {
		t.Fatalf("作用域参数必须排在最前（$1），实际 %v", args)
	}
}

// ---- 单一真源：重复检测的谓词必须与时间轴逐字一致 ----

func TestDuplicateUniverseWhereSharesScopeWithList(t *testing.T) {
	for _, scope := range []MediaScope{
		{Space: "personal", OwnerID: "u1"},
		{Space: "shared", MemberID: "u2"},
	} {
		lw, largs := (&ListParams{Scope: scope}).buildWhere()
		dw, dargs := duplicateUniverseWhere(DuplicateParams{Scope: scope})
		if want := lw + " AND m.phash IS NOT NULL"; dw != want {
			t.Fatalf("作用域 %+v：重复检测谓词必须与时间轴共用同一真源\n got=%q\nwant=%q", scope, dw, want)
		}
		if len(dargs) != len(largs) || dargs[0] != largs[0] {
			t.Fatalf("作用域 %+v：参数应一致，list=%v dup=%v", scope, largs, dargs)
		}
	}
	// 未解析作用域的重复检测同样必须收敛为空结果（历史泄漏就在这里：它自己抄了一份谓词）
	if got, _ := duplicateUniverseWhere(DuplicateParams{}); !strings.Contains(got, failClosed) {
		t.Fatalf("未解析作用域的重复检测必须收敛为空结果，实际 %q", got)
	}
}

// ---- handler 层：非法值必须在触库前被拒，且不回显 DB 细节 ----

func TestHandlersRejectUnknownSpaceBeforeDB(t *testing.T) {
	h := &Handler{} // Store 为 nil：能走到 SQL 就会 panic/报错，故 400 证明是在触库前拦掉的
	paths := []string{
		"/media?space=bogus",
		"/media?space=BOGUS",
		"/media?limit=200&space=everything",
		"/media/date-histogram?granularity=month&space=bogus",
		"/media/duplicates?space=bogus",
		"/media/duplicates?threshold=10&space=../etc/passwd",
	}
	for _, path := range paths {
		rec := scopeReq(h, "u1", path)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d: %s", path, rec.Code, rec.Body.String())
		}
		code, msg := errEnvelope(t, rec)
		if code != "INVALID_PARAMS" {
			t.Fatalf("%s 错误码应为 INVALID_PARAMS，实际 %q", path, code)
		}
		// 不得把 PostgreSQL/枚举/表结构透给调用方
		for _, leak := range []string{"enum", "media_space", "SQLSTATE", "pgx", "SELECT", "FROM media", "22P02"} {
			if strings.Contains(msg, leak) {
				t.Fatalf("%s 错误信息泄露 DB 细节（%q）: %q", path, leak, msg)
			}
		}
	}
}

func TestHandlersFailClosedWithoutIdentity(t *testing.T) {
	h := &Handler{}
	for _, path := range []string{"/media", "/media?space=personal", "/media?space=shared"} {
		rec := scopeReq(h, "", path)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s 无身份却放行了 —— 这正是「无主体即全库」的事故形态", path)
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s 无身份应 401，实际 %d: %s", path, rec.Code, rec.Body.String())
		}
		code, _ := errEnvelope(t, rec)
		if code != "UNAUTHENTICATED" {
			t.Fatalf("%s 错误码应为 UNAUTHENTICATED，实际 %q", path, code)
		}
	}
}

// TestBuildWhereNoAlbum Job000101 未分组桶谓词：NOT EXISTS 绑 Scope.OwnerID，
// 且占位符在作用域谓词（$1）之后续编无错位。
func TestBuildWhereNoAlbum(t *testing.T) {
	p := ListParams{Scope: MediaScope{Space: "personal", OwnerID: "u1"}, NoAlbum: true}
	where, args := p.buildWhere()
	if !strings.Contains(where, "NOT EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id") {
		t.Fatalf("NoAlbum 谓词缺失: %q", where)
	}
	if !strings.Contains(where, "a.owner_id = $2") {
		t.Fatalf("NoAlbum 应绑 $2（作用域占 $1）: %q", where)
	}
	if len(args) != 2 || args[1] != "u1" {
		t.Fatalf("args 应为 [u1 u1]，实际 %v", args)
	}
}

// TestBuildWhereNoAlbumSharedScopeIsNoOp shared 档 OwnerID 为空 → NOT EXISTS 恒真退化不过滤
// （前端仅在 space=personal 使用 album=none；语义见 albums/groups.go 头注释）。
func TestBuildWhereNoAlbumSharedScopeIsNoOp(t *testing.T) {
	p := ListParams{Scope: MediaScope{Space: "shared", MemberID: "u1"}, NoAlbum: true}
	where, _ := p.buildWhere()
	if !strings.Contains(where, "a.owner_id = $") {
		t.Fatalf("shared 档仍应拼出谓词（绑空串不命中任何属主）: %q", where)
	}
}
