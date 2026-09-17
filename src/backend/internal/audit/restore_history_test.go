package audit

// GET /media/restore-history 的回归保护。
//
// 这是本包**唯一面向普通用户**的端点，因此它的守卫重点与 /admin/* 不同：
// /admin/* 的问题是"别把错误包成 200"，而它的问题是 **"绝不能读到别人的行"**。
//
// 三组互补的断言：
//
//	1. 作用域：actor 必须来自上下文，且**不可**被查询参数覆盖（越权面）；
//	2. 空 actor 必须 401 而不是"退化成查全站"（这是 BuildWhere 的真实行为，非假想）；
//	3. 它**不写** ActionAuditRead —— 钉住那条刻意的决定，防止有人"顺手"加上自记录。

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// readSourceFile 读本包目录下的源码文件（源码形状守卫用）。
// 测试须在包目录下运行；读不到即 Fatal —— **不 skip**，否则守卫会变成永远绿（§二十四.3）。
func readSourceFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("读不到 %s（测试需在包目录下运行）: %v", name, err)
	}
	return string(b)
}

// actorRouter 造一个只注入指定 actor 的路由；actor 为空串时**不注入**，
// 模拟认证中间件漏挂 / 失效。
func actorRouter(h *Handler, actor string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if actor != "" {
		r.Use(func(c *gin.Context) { c.Set("user_id", actor) })
	}
	r.GET("/media/restore-history", h.RestoreHistory)
	return r
}

// ---- 1. 作用域：只按上下文里的 actor 过滤，且不可被查询参数覆盖 ----

func TestRestoreHistoryScopesToContextCaller(t *testing.T) {
	const other = "11111111-2222-4333-8444-555555555555"
	fs := &fakeStore{}
	h := &Handler{Store: fs}
	r := newTestRouter(h)

	// 同时塞入若干"想越权"的查询参数：actor 指向别人、action 指向别的动作。
	// 断言：一个都不起作用。
	w, body := doGet(t, r, "/media/restore-history?actor="+other+
		"&action=media.purge&target_id=whatever&limit=20")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
	}
	if fs.calls() != 1 {
		t.Fatalf("store.Query 应被调用 1 次，实际 %d", fs.calls())
	}

	f := fs.filter()
	if f.ActorUserID != testActor {
		t.Fatalf("ActorUserID = %q，期望上下文里的 %q —— "+
			"若这里变成了查询参数里的 %q，就是**任何人可读别人的恢复历史**（越权）",
			f.ActorUserID, testActor, other)
	}
	if f.Action != ActionMediaRestore {
		t.Fatalf("Action = %q，期望 %q —— 参数不得覆盖动作过滤，否则会把别人的 purge/delete 也带出来",
			f.Action, ActionMediaRestore)
	}
	if f.TargetID != "" || f.TargetType != "" {
		t.Fatalf("本端点不接受 target 过滤，实际 target_type=%q target_id=%q", f.TargetType, f.TargetID)
	}
	if f.Limit != 20 {
		t.Fatalf("limit 应被采纳为 20，实际 %d", f.Limit)
	}
	// 响应是审计行本身：字段名按 audit_log 的真实列（含 at / target_id / detail）。
	if _, ok := body["items"]; !ok {
		t.Fatalf("响应应含 items，实际 %s", w.Body.String())
	}
}

// ---- 2. 空 actor 必须 401，而不是"退化成查全站" ----

func TestRestoreHistoryRejectsMissingActor(t *testing.T) {
	// 前提自证：BuildWhere 对空 ActorUserID **确实会跳过** user_id 条件。
	// 这条是下面 401 断言的立论依据 —— 若哪天 BuildWhere 改了行为，这里先红，
	// 免得我们以为"反正不会查全站"而把 401 当成冗余检查删掉。
	if cond, _ := BuildWhere(Filter{Action: ActionMediaRestore}); strings.Contains(cond, "user_id") {
		t.Fatalf("前提失效：空 ActorUserID 时 BuildWhere 竟然加了 user_id 条件（%q）——"+
			"请重新评估本用例的结论", cond)
	}

	for _, tc := range []struct{ name, actor string }{
		{"完全没有 user_id", ""},
		{"user_id 是空白", "   "},
		{"user_id 不是 UUID", "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs2 := &fakeStore{}
			h2 := &Handler{Store: fs2}
			r := actorRouter(h2, tc.actor)

			w, body := doGet(t, r, "/media/restore-history")
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("状态码 %d，期望 401（body=%s）—— "+
					"空/非法 actor 若放行，BuildWhere 会跳过 user_id 条件，"+
					"这次查询就变成**返回全站所有人的恢复历史**", w.Code, w.Body.String())
			}
			assertErrorBody(t, w, body, http.StatusUnauthorized, CodeUnauthorized)
			if fs2.calls() != 0 {
				t.Fatalf("校验失败时不该触库，实际调了 %d 次 store.Query", fs2.calls())
			}
		})
	}
}

// ---- 3. 参数解析：坏游标 400 且不触库；limit 钳制沿用既有语义 ----

func TestRestoreHistoryCursorAndLimit(t *testing.T) {
	// 坏游标：400 且**不触库**（提前返回，而不是"查完再报错"）。
	//
	// ⚠️ 两个坏样本刻意都不用 `%` —— 我第一版写的是 `cursor=%%%not-base64%%%`，
	// URL 解析会把非法的百分号编码整段丢掉，于是服务端收到的 cursor 是**空串**，
	// handler 正常返回 200，测试却"看起来在测坏游标"。**输入没送达到被测代码，
	// 断言就成了空转。** 这两种非法输入都能原样通过 URL：
	//   @@@  —— 不是合法 base64（解码即失败）
	//   zzzz —— 是合法 base64，但解出来没有 `|` 分隔符（格式错误）
	for _, bad := range []string{"@@@", "zzzz"} {
		t.Run("坏游标 "+bad, func(t *testing.T) {
			fs2 := &fakeStore{}
			h2 := &Handler{Store: fs2}
			r2 := newTestRouter(h2)
			w, body := doGet(t, r2, "/media/restore-history?cursor="+bad)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("坏游标 %q 应 400，实际 %d：%s", bad, w.Code, w.Body.String())
			}
			assertErrorBody(t, w, body, http.StatusBadRequest, CodeInvalidInput)
			if fs2.calls() != 0 {
				t.Fatalf("坏游标 %q 不该触库，实际调了 %d 次", bad, fs2.calls())
			}
		})
	}

	// 合法游标：被解出 (at, id) 传给 store。
	at := mustTime(t, "2026-09-17T06:12:52Z")
	for _, tc := range []struct {
		name     string
		query    string
		wantLimi int
		wantCur  bool
	}{
		{"缺省", "", defaultAuditLimit, false},
		{"0 回落默认", "?limit=0", defaultAuditLimit, false},
		{"非数字回落默认", "?limit=abc", defaultAuditLimit, false},
		{"正常值", "?limit=7", 7, false},
		{"超大夹到上限", "?limit=9999", maxAuditLimit, false},
		{"带游标", "?cursor=" + encodeCursor(at, 42), defaultAuditLimit, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs2 := &fakeStore{}
			h2 := &Handler{Store: fs2}
			r2 := newTestRouter(h2)
			w, _ := doGet(t, r2, "/media/restore-history"+tc.query)
			if w.Code != http.StatusOK {
				t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
			}
			f := fs2.filter()
			if f.Limit != tc.wantLimi {
				t.Fatalf("limit = %d，期望 %d（钳制语义必须与 /admin/audit 一致：超上限夹到上限，而非回落默认）",
					f.Limit, tc.wantLimi)
			}
			if tc.wantCur {
				if f.CursorAt == nil || f.CursorID == nil || *f.CursorID != 42 || !f.CursorAt.Equal(at) {
					t.Fatalf("游标应解出 (%s, 42)，实际 (%v, %v)", at, f.CursorAt, f.CursorID)
				}
			} else if f.CursorAt != nil || f.CursorID != nil {
				t.Fatalf("无 cursor 参数时不该有游标条件，实际 (%v, %v)", f.CursorAt, f.CursorID)
			}
		})
	}
}

// ---- 4. store 错误必须 500，不得包成 200 ----

func TestRestoreHistoryStoreErrorIs500(t *testing.T) {
	fs := &fakeStore{queryErr: errBoom}
	h := &Handler{Store: fs}
	r := newTestRouter(h)

	w, body := doGet(t, r, "/media/restore-history")
	assertErrorBody(t, w, body, http.StatusInternalServerError, CodeInternal)
}

// ---- 5. 它**不写** ActionAuditRead（钉住那条刻意决定） ----

func TestRestoreHistoryDoesNotWriteAuditRead(t *testing.T) {
	// 两个假实现都接上：Store 供查询，RecorderStore 供记录。
	// 若有人给本端点加上"读审计也要留痕"，下面的 entries() 会非空 ⇒ 红。
	fs := &fakeStore{}
	h := &Handler{
		Store:    fs,
		Recorder: NewWithStore(fs, zap.NewNop()),
	}
	r := newTestRouter(h)

	w, _ := doGet(t, r, "/media/restore-history")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
	}
	if n := len(fs.entries()); n != 0 {
		t.Fatalf("本端点写了 %d 条审计：%v —— "+
			"读**自己**的恢复历史不产生新的知情面（与 /admin/audit 的前提不同），"+
			"且每开一次工具箱写一条会把账本淹没。若确要改成自记录，"+
			"请先更新 internal/audit/restore_history.go 的文件头说明理由与契约。", n, fs.entries())
	}
	// 前提自证：同一个假 store 接在 ListAudit 上**必须**写出 admin.audit.read。
	// 否则上面的 0 条可能只是"AppendRecord 压根没工作"，而不是"本端点刻意不写"。
	w2, _ := doGet(t, r, "/admin/audit")
	if w2.Code != http.StatusOK {
		t.Fatalf("预备请求 /admin/audit 应 200，实际 %d", w2.Code)
	}
	if n := len(fs.entries()); n != 1 || fs.entries()[0].Action != ActionAuditRead {
		t.Fatalf("前提失效：ListAudit 应写出 1 条 %s，实际 %d 条 %v —— "+
			"这条自证不过，上面「本端点不写审计」的结论就没有意义",
			ActionAuditRead, n, fs.entries())
	}
}

// ---- 6. 接线本身还在（源码形状）：动作名不得写成字面量 ----

func TestRestoreHistoryUsesConstantNotLiteral(t *testing.T) {
	// §二十四.4：常量改名不会编译失败，而审计查询是精确匹配 ⇒ 静默查不到。
	// 本端点的 Action 过滤值必须是常量。
	src := readSourceFile(t, "restore_history.go")
	if !strings.Contains(src, "Action: ActionMediaRestore") {
		t.Fatalf("restore_history.go 里找不到 `Action: ActionMediaRestore` —— " +
			"过滤动作必须是常量引用（字面量在改名后不会编译失败，而查询是精确匹配的）")
	}
	if strings.Contains(src, `"media.restore"`) {
		t.Fatalf("restore_history.go 里出现了动作名字面量 \"media.restore\"：请改用 ActionMediaRestore 常量")
	}
	// 自证：同一个"找字面量"的手法在一个含字面量的假源码上必须命中，
	// 否则这条守卫可能是空转的（例如字符串写法不同导致永远不匹配）。
	fakeSrc := "Action: \"" + ActionMediaRestore + "\","
	if !strings.Contains(fakeSrc, `"`+ActionMediaRestore+`"`) {
		t.Fatal("守卫失效：构造的坏样本都不含字面量，本断言拦不住回归")
	}
}
