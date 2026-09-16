package shares

// 审计接线（§二十）的回归保护：share.create / share.revoke。
//
// 两条与 media 包同构的钉法（record 语义 + 失败路径不写审计 + 源码形状守卫），
// 外加本包**特有的红线**：share token 绝不能进 detail —— 它本身就是访问凭证
// （公开端点无鉴权、token 即凭证），写进永久保留的审计表等于复制一份凭证；
// 而且键名含 "token" 会被 RedactDetail 整键剔除，调用方看不出任何异常。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
)

// recordingStore 收集 Insert 入参，并**照线上一样过一遍 Normalize**
// （RedactDetail 只在 Normalize 内调用；不过这一关就测不出键名被脱敏吃掉）。
type recordingStore struct{ entries []audit.Entry }

func (r *recordingStore) Insert(_ context.Context, e audit.Entry) (int64, error) {
	ne, err := e.Normalize()
	if err != nil {
		return 0, err
	}
	r.entries = append(r.entries, ne)
	return int64(len(r.entries)), nil
}

func shareReq(h *Handler, actor, method, path, body string, register func(*gin.Engine)) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", actor)
		c.Set("role", "owner")
	})
	register(r)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// unreachablePool 必然连不上（端口 1）：错误路径不需要真实数据库，
// 也就不需要「没库就静默跳过」的测试 —— 那类测试最容易变成永远绿。
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败（本用例只用它连不上这一性质）: %v", err)
	}
	return pool
}

// ---- 1. record 的语义（含 token 红线）----

func TestShareRecordCarriesActorAndKeepsSafeDetail(t *testing.T) {
	fake := &recordingStore{}
	h := &Handler{Audit: audit.NewWithStore(fake, nil)}
	const actor = "7c6b1b9c-cba2-4394-b982-67d9038421f3"

	// 与 Create 里**实际**使用的 detail 键名保持一致。
	detail := map[string]any{
		"kind":           "album",
		"shared_id":      "a-1",
		"wechat":         true,
		"allow_download": false,
		"passcode":       true,
		"owner_id":       actor,
	}
	shareReq(h, actor, http.MethodPost, "/shares", "", func(r *gin.Engine) {
		r.POST("/shares", func(c *gin.Context) {
			h.record(c, audit.ActionShareCreate, audit.TargetShare, "s-1", detail)
			c.Status(http.StatusCreated)
		})
	})
	if len(fake.entries) != 1 {
		t.Fatalf("应写入 1 条审计，实际 %d", len(fake.entries))
	}
	e := fake.entries[0]
	if e.ActorUserID != actor {
		t.Fatalf("actor 应为 %s（取自上下文 user_id），实际 %q —— 空 actor 会让「谁分享的」永久丢失", actor, e.ActorUserID)
	}
	if e.Action != audit.ActionShareCreate || e.TargetType != audit.TargetShare || e.TargetID != "s-1" {
		t.Fatalf("动作/目标不符：action=%q target=%q/%q", e.Action, e.TargetType, e.TargetID)
	}
	// ⭐ 这条是"前提自证"：脱敏器确实会吃掉键名，否则下面「键名安全」是废话。
	if !audit.IsSensitiveKey("has_password") || !audit.IsSensitiveKey("share_token") ||
		!audit.IsSensitiveKey("password_hash") {
		t.Fatal("前提失效：RedactDetail 应剔除含 password/token/hash 子串的键名；" +
			"若它不再剔除，本用例无法证明我们选的键名是安全的")
	}
	for k := range detail {
		if audit.IsSensitiveKey(k) {
			t.Fatalf("detail 键名 %q 命中脱敏名单：会被整键剔除、detail 静默变成 {}。"+
				"请换中性键名（口令类用 passcode / second_factor，切勿叫 has_password）", k)
		}
	}
	// detail 必须原样落库（不是被剔空后的 {}）。
	if len(e.Detail) != len(detail) {
		t.Fatalf("detail 应有 %d 项且原样保留，实际 %d 项：%v（有键被脱敏器吃掉了？）",
			len(detail), len(e.Detail), e.Detail)
	}
	for k, v := range detail {
		if got, ok := e.Detail[k]; !ok || got != v {
			t.Fatalf("detail[%q] 应保留为 %v，实际 %v（存在=%v）", k, v, got, ok)
		}
	}
}

// ---- 2. 失败路径不写审计（真实 handler + 不可达库）----

func TestShareAuditNothingWhenStoreFails(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()

	fake := &recordingStore{}
	h := &Handler{
		Store: &Store{Pool: pool},
		Audit: audit.NewWithStore(fake, nil),
	}
	reg := func(r *gin.Engine) {
		r.POST("/shares", h.Create)
		r.DELETE("/shares/:id", h.Delete)
	}

	// Create：请求体合法 ⇒ 走到 TargetOwnedBy ⇒ 库不可达 ⇒ 500，且不写审计。
	post := shareReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3", http.MethodPost,
		"/shares", `{"kind":"album","target_id":"a-1"}`, reg)
	// Delete：走到 GetByID ⇒ 库不可达 ⇒ 500，且不写审计。
	del := shareReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3", http.MethodDelete,
		"/shares/3f3d7bad-6795-4e48-a039-79c44b206c67", "", reg)
	if post.Code != http.StatusInternalServerError {
		t.Fatalf("库不可达时 Create 应 500（走到 TargetOwnedBy 才失败），实际 %d：%s",
			post.Code, post.Body.String())
	}
	if del.Code != http.StatusInternalServerError {
		t.Fatalf("库不可达时 Delete 应 500（走到 GetByID 才失败），实际 %d：%s",
			del.Code, del.Body.String())
	}
	if len(fake.entries) != 0 {
		t.Fatalf("操作全部失败，却写入了 %d 条审计：%v —— "+
			"审计会记下「谁创建/吊销了 X」而 X 其实没成功，比不记录更危险", len(fake.entries), fake.entries)
	}
}

// ---- 3. 源码形状：接线还在 + token 不进 detail ----

func shareHandlerBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func (h *Handler) "+name+"(")
	if start < 0 {
		t.Fatalf("源码里找不到 func (h *Handler) %s —— 方法被改名或删除？", name)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

// recordCall 抠出 `h.record(...)` 的完整实参文本（按括号配平，而不是按行猜），
// 刻意排除它上方的注释 —— 注释里出现 "token" 是允许的（我们在解释为什么不记它）。
func recordCall(t *testing.T, body string) string {
	t.Helper()
	const call = "h.record("
	i := strings.Index(body, call)
	if i < 0 {
		return ""
	}
	depth := 0
	for j := i + len(call) - 1; j < len(body); j++ {
		switch body[j] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return body[i : j+1]
			}
		}
	}
	return body[i:]
}

func TestShareHandlersAreWiredToAudit(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	cases := []struct {
		fn, want string
		broken   string
	}{
		{"Create", "h.record(c, audit.ActionShareCreate, audit.TargetShare, id",
			"func (h *Handler) Create(c *gin.Context) {\n\tc.JSON(http.StatusCreated, gin.H{})\n}"},
		{"Delete", "h.record(c, audit.ActionShareRevoke, audit.TargetShare, id",
			"func (h *Handler) Delete(c *gin.Context) {\n\tc.Status(http.StatusNoContent)\n}"},
	}
	for _, tc := range cases {
		body := shareHandlerBody(t, src, tc.fn)
		if !strings.Contains(body, tc.want) {
			t.Fatalf("%s 里找不到审计调用 %q。\n"+
				"分享创建与吊销都必须写审计：撤销是一次**权限收回**，"+
				"不记的话「谁在什么时候撤掉了谁的分享链接」无从追查。"+
				"若是有意改动，请同时更新本测试与 §二十 的接入清单。", tc.fn, tc.want)
		}
		// 前提自证：把 record 去掉的版本必须让同一断言失败，否则守卫是空转的。
		if strings.Contains(shareHandlerBody(t, tc.broken, tc.fn), tc.want) {
			t.Fatalf("守卫失效：本断言在去掉 record 的版本上也会通过（%s），拦不住回归", tc.fn)
		}
	}

	// 反向守卫一：动作名必须是常量，不能是字面量（改名不会编译失败，而查询是精确匹配）。
	literal := regexp.MustCompile(`"share\.(create|revoke)"`)
	for _, fn := range []string{"Create", "Delete"} {
		if m := literal.FindString(shareHandlerBody(t, src, fn)); m != "" {
			t.Fatalf("%s 里出现了动作名字面量 %s：请改用 audit.ActionShare* 常量", fn, m)
		}
	}

	// ⭐ 反向守卫二（本包红线）：record 的实参里不得出现 token / PasswordHash。
	// token 是访问凭证，写进永久保留的审计表等于复制一份凭证；
	// PasswordHash 更是绝不该离开 users/share_links 表。
	for _, fn := range []string{"Create", "Delete"} {
		call := recordCall(t, shareHandlerBody(t, src, fn))
		if call == "" {
			t.Fatalf("%s 里找不到 h.record( 调用（上面的接线断言已覆盖，这里只做红线检查）", fn)
		}
		for _, bad := range []string{"Token", "token", "PasswordHash", "password"} {
			if strings.Contains(call, bad) {
				t.Fatalf("%s 的 record 实参里出现了 %q —— 分享 token 与口令散列绝不能进审计表。\n"+
					"target 用 share/<id> 即可；detail 只记 kind / shared_id / owner_id 这类非敏感元信息。\n"+
					"实参为：%s", fn, bad, call)
			}
		}
	}
}
