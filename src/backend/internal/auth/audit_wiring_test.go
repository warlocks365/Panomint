package auth

// 审计接线（§二十）的回归保护：admin.user.create。
//
// 创建账号是一条**开权限**的操作，此前 `ActionUserCreate` 常量已登记但无调用点。
// 三条钉法：源码形状（接线还在、动作名用常量、**实参里不得出现口令**）、
// 失败路径不写审计、以及一条纯函数式的入口参数互证。

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

// createUserReq 造一个带 actor 的 gin 上下文并 POST /admin/users。
func createUserReq(h *Handler, actor, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", actor)
		c.Set("role", "owner")
	})
	r.POST("/admin/users", h.CreateUser)
	req := httptest.NewRequest(http.MethodPost, "/admin/users", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// ---- 失败路径不写审计（真实 handler + 不可达库）----

func TestCreateUserAuditsNothingWhenStoreFails(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败（本用例只用它连不上这一性质）: %v", err)
	}
	defer pool.Close()

	fake := &recordingStore{}
	h := &Handler{
		Store: &Store{Pool: pool},
		Audit: audit.NewWithStore(fake, nil),
	}
	// 请求体合法（能过 binding），失败发生在建库那一步 ⇒ 409。
	rec := createUserReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3",
		`{"email":"probe@pano.local","display_name":"probe","password":"probe-pass-1234","role":"viewer"}`)
	if rec.Code == http.StatusCreated {
		t.Fatalf("库不可达时不该返回 201，实际 %d", rec.Code)
	}
	if len(fake.entries) != 0 {
		t.Fatalf("创建失败，却写入了 %d 条审计：%v —— "+
			"审计会记下一个并不存在的账号被创建，比不记录更危险", len(fake.entries), fake.entries)
	}
	// 该请求体里的口令绝不能被审计记走 —— 顺带断言脱敏名单确实管这叫敏感。
	if !audit.IsSensitiveKey("password") {
		t.Fatal("前提失效：脱敏名单不含 password")
	}
}

// ---- 源码形状 ----

func createUserBody(t *testing.T, src string) string {
	t.Helper()
	start := strings.Index(src, "func (h *Handler) CreateUser(")
	if start < 0 {
		t.Fatalf("源码里找不到 func (h *Handler) CreateUser —— 方法被改名或删除？")
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

// recordCall 抠出 `h.record(...)` 的完整实参文本（按括号配平，而不是按行猜），
// 刻意排除它上方的注释 —— 注释里出现 password/token 是允许的（我们在解释为什么不记它们）。
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

func TestCreateUserIsWiredToAudit(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	body := createUserBody(t, src)

	const want = "h.record(c, audit.ActionUserCreate, audit.TargetUser, id"
	if !strings.Contains(body, want) {
		t.Fatalf("CreateUser 里找不到审计调用 %q。\n"+
			"创建账号必须写审计：这是一条**开权限**的操作，"+
			"不记的话「谁在什么时候开了哪个账号、给了什么角色」无从追查。"+
			"若是有意改动，请同时更新本测试与 §二十 的接入清单。", want)
	}
	// 前提自证：把 record 去掉的坏版本必须让同一断言失败，否则守卫是空转的。
	broken := "func (h *Handler) CreateUser(c *gin.Context) {\n\tc.JSON(http.StatusCreated, gin.H{})\n}"
	if strings.Contains(createUserBody(t, broken), want) {
		t.Fatal("守卫失效：本断言在去掉 record 的版本上也会通过，拦不住回归")
	}

	// 反向守卫一：动作名必须是常量，不能是字面量（改名不会编译失败，而查询是精确匹配）。
	if m := regexp.MustCompile(`"admin\.user\.create"`).FindString(body); m != "" {
		t.Fatalf("CreateUser 里出现了动作名字面量 %s：请改用 audit.ActionUserCreate 常量", m)
	}

	// 反向守卫二：record 的实参里不得出现任何口令 / 口令散列。
	// 审计表永久保留，密码即便被 bcrypt 散列也不该在这里留第二份。
	call := recordCall(t, body)
	if call == "" {
		t.Fatalf("CreateUser 里找不到 h.record( 调用")
	}
	for _, bad := range []string{"Password", "password", "pwd", "Hash"} {
		if strings.Contains(call, bad) {
			t.Fatalf("CreateUser 的 record 实参里出现了 %q —— 口令绝不能进审计表。\n"+
				"detail 只记 email / role 这类非敏感元信息。实参为：%s", bad, call)
		}
	}
}
