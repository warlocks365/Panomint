package media

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// GET /tags/:id/media 可见性的回归网。
//
// 修复前 WHERE 只有 `deleted_at + tag_id + confirmed`，**没有任何可见性条件**，
// 实测 viewer 账号能拿到 owner 的媒体（2025-07-教会山-010.jpg）。
// 这里的断言钉住两件事：
//  1. 未解析/缺主体作用域 → 恒假（空结果），而不是「没有条件」（= 全库）；
//  2. 作用域参数从 $1 起、占位符与 args 自洽 —— 编号错只会在运行期报
//     `expected N arguments`，编译期与静态阅读都看不出来。

func TestBuildTagMediaWhereUnresolvedScopeFailClosed(t *testing.T) {
	for _, s := range []MediaScope{
		{},                              // 零值：handler 忘了解析作用域
		{Space: "personal"},             // 缺主体
		{Space: "shared"},               // 缺主体
		{Space: "bogus", OwnerID: "u1"}, // 枚举外（正常在 handler 就被 400 拦掉）
	} {
		where, args, err := buildTagMediaWhere(s, "t1", "")
		if err != nil {
			t.Fatalf("%+v: %v", s, err)
		}
		if !strings.Contains(where, failClosed) {
			t.Fatalf("%+v 必须收敛为恒假（空结果），实际 %q —— 未证明安全的作用域绝不允许放行", s, where)
		}
		if strings.Contains(where, "m.owner_id") || strings.Contains(where, "m.space") {
			t.Fatalf("%+v 不该凭空拼出属主/空间条件: %q", s, where)
		}
		for _, a := range args {
			if a == "u1" {
				t.Fatalf("%+v 不应把未证明安全的身份放进 args: %v", s, args)
			}
		}
	}
}

func TestBuildTagMediaWherePersonalBindsOwnerFirst(t *testing.T) {
	where, args, err := buildTagMediaWhere(MediaScope{Space: "personal", OwnerID: "u1"}, "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "m.space = 'personal'") || !strings.Contains(where, "m.owner_id = $1") {
		t.Fatalf("个人空间必须按属主收窄且占位符为 $1: %q", where)
	}
	if !strings.Contains(where, "mt.tag_id = $2") {
		t.Fatalf("标签条件应在 scope 之后续编为 $2: %q", where)
	}
	if !strings.Contains(where, "mt.confirmed = true") {
		t.Fatalf("仅已确认关联的语义丢失: %q", where)
	}
	if len(args) != 2 || args[0] != "u1" || args[1] != "t1" {
		t.Fatalf("args 应为 [u1 t1]（作用域参数必须排最前），实际 %v", args)
	}
}

func TestBuildTagMediaWherePlaceholderNumbering(t *testing.T) {
	cur := encodeCursor(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), "m9")
	where, args, err := buildTagMediaWhere(MediaScope{Space: "personal", OwnerID: "u1"}, "t1", cur)
	if err != nil {
		t.Fatal(err)
	}
	// scope(1) + tag(1) + cursor(2)
	if len(args) != 4 {
		t.Fatalf("参数个数应为 4（scope 1 + tag 1 + cursor 2），实际 %d: %v", len(args), args)
	}
	seen := map[int]bool{}
	for _, m := range regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(where, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("占位符解析失败: %q", m[0])
		}
		seen[n] = true
	}
	// 连续无空洞：缺 $k 会把后面的参数错位到别的列上
	for i := 1; i <= len(args); i++ {
		if !seen[i] {
			t.Fatalf("占位符 $%d 缺失（编号必须连续），实际 %q", i, where)
		}
	}
	if len(seen) != len(args) {
		t.Fatalf("占位符集合与 args 数量不匹配（占位符 %v，args %d 个）—— 运行期会报 expected N arguments: %q",
			seen, len(args), where)
	}
	if args[0] != "u1" {
		t.Fatalf("作用域参数必须排在最前（$1），实际 %v", args)
	}
}

func TestBuildTagMediaWhereSharedUsesMembership(t *testing.T) {
	where, args, err := buildTagMediaWhere(MediaScope{Space: "shared", MemberID: "u2"}, "t1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "m.space = 'shared'") || !strings.Contains(where, "shared_space_members") {
		t.Fatalf("shared 必须带成员判定（不能只写 m.space='shared'）: %q", where)
	}
	if strings.Count(where, "$1") != 2 {
		t.Fatalf("两个 EXISTS 必须绑同一个 $1（否则参数错位）: %q", where)
	}
	if len(args) != 2 || args[0] != "u2" || args[1] != "t1" {
		t.Fatalf("args 应为 [u2 t1]，实际 %v", args)
	}
}

func TestBuildTagMediaWhereCursorSeparatorIsSargable(t *testing.T) {
	// 游标条件必须与时间轴同形（(taken_at, id) < (...)，复合游标），否则分页会漏/重
	cur := encodeCursor(time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC), "m9")
	where, _, err := buildTagMediaWhere(MediaScope{Space: "personal", OwnerID: "u1"}, "t1", cur)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(where, "(m.taken_at, m.id) < ($3, $4)") {
		t.Fatalf("游标条件应续编为 ($3, $4): %q", where)
	}
}

func TestBuildTagMediaWhereInvalidCursor(t *testing.T) {
	if _, _, err := buildTagMediaWhere(MediaScope{Space: "personal", OwnerID: "u1"}, "t1", "!!!"); err == nil {
		t.Fatal("非法游标应报错（handler 转 400）")
	}
}

// 接线断言：handler 必须解析 space 并把 scope 传进 Store（否则谓词在、作用域恒零值=空结果）。
// Store 为 nil：非法 space 若在触库前被 400 拦掉，就说明 ResolveMediaScope 确实接线了；
// 若未接线，则会先撞 nil Store（panic）而不是 400。
func TestListTagMediaResolvesScopeBeforeStore(t *testing.T) {
	rec := tagMediaReq(&Handler{}, "u1", "/tags/t1/media?space=bogus")
	if rec.Code != 400 {
		t.Fatalf("非法 space 应在触库前 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errEnvelope(t, rec); code != "INVALID_PARAMS" {
		t.Fatalf("错误码应为 INVALID_PARAMS，实际 %q", code)
	}
}

func tagMediaReq(h *Handler, userID, path string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", userID); c.Set("role", "viewer") })
	r.GET("/tags/:id/media", h.ListTagMedia)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}
