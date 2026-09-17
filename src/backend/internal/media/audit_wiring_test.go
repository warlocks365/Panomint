package media

// 审计接线（§二十）的回归保护。
//
// 背景：`internal/audit/audit.go` 登记了 15 个动作，早期真正写入的只有个位数；
// 其中 **`media.purge` 是最危险的一个 —— 它是唯一不可恢复的动作**（整行删除 +
// 磁盘文件清理），旧实现在 handler 里 `checkAccess` → `Store.Purge` → 清文件 → 返回，
// **一行审计都不写**。也就是说：「谁把什么东西永久销毁了」在库里没有任何痕迹。
//
// 续（§二十五 遗留 1）：`media.delete` / `media.purge` 已审计，但**「从回收站恢复」没有**
// ⇒ `media.delete` 成了一笔无法闭合的账，库里只留「谁删了 X」，没有任何地方记录
// 「后来又拿回来了」。本文件同时钉住 `media.restore`。
//
// 本文件用三种互补的方式把它钉住：
//
//	1. record 的语义（fake store）：actor 取自上下文、action/target 落库正确、
//	   detail 的键名不会撞上 RedactDetail 被静默剔除；
//	2. **失败路径不写审计**（真实 handler + 不可达 DB）：操作没成功就不该留记录 ——
//	   「记录错误的审计比不记录更危险」（见 audit.Recorder 的注释）；
//	3. 接线本身还在（源码形状）：防止有人重构 handler 时把 record 那一行删掉，
//	   而上面两条测试都察觉不到（它们一个测 record 本身、一个测失败路径）。

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
	"go.uber.org/zap"

	"panoalbum/internal/audit"
)

// fakeRecorder 收集 RecorderStore.Insert 的入参，并**照线上一样过一遍 Normalize**。
//
// 走 Normalize 是刻意的：RedactDetail 只在 Normalize 内被调用，不过这一关就测不出
// 「键名撞上敏感子串 ⇒ 整键被剔除 ⇒ detail 静默变成 {}」这个故障。
type fakeRecorder struct{ entries []audit.Entry }

func (f *fakeRecorder) Insert(_ context.Context, e audit.Entry) (int64, error) {
	ne, err := e.Normalize()
	if err != nil {
		return 0, err
	}
	f.entries = append(f.entries, ne)
	return int64(len(f.entries)), nil
}

// auditReq 造一个带 actor 的 gin 上下文并发一个请求（不起真实网络）。
func auditReq(h *Handler, actor, method, path string, register func(*gin.Engine)) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", actor)
		c.Set("role", "owner")
	})
	register(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// unreachablePool 指向一个**必然连不上**的地址（端口 1）。
// 库连不上时 handler 必须在触库失败处返回，不得写出任何审计行。
//
// 与 internal/tags/labelcache_test.go 用的是同一手法：错误路径不需要真实数据库，
// 也就不需要「没库就静默跳过」的测试 —— 那类测试最容易变成永远绿。
func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败（本用例只用它连不上这一性质）: %v", err)
	}
	return pool
}

// ---- 1. record 的语义 ----

func TestRecordCarriesActorAndTarget(t *testing.T) {
	fake := &fakeRecorder{}
	h := &Handler{Audit: audit.NewWithStore(fake, zap.NewNop())}
	const actor = "7c6b1b9c-cba2-4394-b982-67d9038421f3"

	rec := auditReq(h, actor, http.MethodDelete, "/media/m-1", func(r *gin.Engine) {
		r.DELETE("/media/:id", func(c *gin.Context) {
			h.record(c, audit.ActionMediaDelete, audit.TargetMedia, c.Param("id"),
				map[string]any{"owner_id": actor})
			c.Status(http.StatusOK)
		})
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("预备请求应 200，实际 %d", rec.Code)
	}
	if len(fake.entries) != 1 {
		t.Fatalf("应写入 1 条审计，实际 %d", len(fake.entries))
	}
	e := fake.entries[0]
	// ⭐ actor 必须从上下文取到。取不到（空串）时 Normalize 不报错、落库是 NULL ——
	// 这个故障是静默的，所以必须显式断言。
	if e.ActorUserID != actor {
		t.Fatalf("actor 应为 %s（取自上下文 user_id），实际 %q —— "+
			"空 actor 会让「谁干的」永久丢失（落库成 NULL，且不报错）", actor, e.ActorUserID)
	}
	if e.Action != audit.ActionMediaDelete || e.TargetType != audit.TargetMedia || e.TargetID != "m-1" {
		t.Fatalf("动作/目标不符：action=%q target=%q/%q", e.Action, e.TargetType, e.TargetID)
	}
	if e.Detail["owner_id"] != actor {
		t.Fatalf("detail 应保留 owner_id，实际 %v", e.Detail)
	}
}

func TestRecordDetailKeysSurviveRedaction(t *testing.T) {
	// 本断言的前提自证：脱敏器确实会吃掉键名（否则下面「我们用的键名安全」是废话）。
	if !audit.IsSensitiveKey("password_set") || !audit.IsSensitiveKey("share_token") ||
		!audit.IsSensitiveKey("totp_secret") {
		t.Fatal("前提失效：RedactDetail 应剔除含 password/token/secret 子串的键名；" +
			"若它不再剔除，本用例无法证明我们选的键名是安全的")
	}
	// 我们在 handler 里实际使用的 detail 键名，必须都不命中脱敏名单 ——
	// 命中即整键剔除、detail 静默变成 {}，而调用方看不出任何异常。
	for _, k := range []string{"owner_id", "path"} {
		if audit.IsSensitiveKey(k) {
			t.Fatalf("detail 键名 %q 命中脱敏名单：它会被整键剔除、detail 静默变成 {}。"+
				"请换一个中性键名（例如口令类信息用 passcode / second_factor 命名）", k)
		}
	}

	fake := &fakeRecorder{}
	h := &Handler{Audit: audit.NewWithStore(fake, zap.NewNop())}
	auditReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3", http.MethodDelete, "/media/m-2", func(r *gin.Engine) {
		r.DELETE("/media/:id", func(c *gin.Context) {
			h.record(c, audit.ActionMediaPurge, audit.TargetMedia, c.Param("id"),
				map[string]any{"owner_id": "u-1", "path": "auditprobe/x.jpg"})
			c.Status(http.StatusOK)
		})
	})
	if len(fake.entries) != 1 {
		t.Fatalf("应写入 1 条审计，实际 %d", len(fake.entries))
	}
	d := fake.entries[0].Detail
	if len(d) != 2 || d["path"] != "auditprobe/x.jpg" {
		t.Fatalf("detail 应为 {owner_id,path} 两项且原样保留，实际 %v（被脱敏器吃掉了？）", d)
	}
}

func TestRecordWithoutRecorderIsNoop(t *testing.T) {
	// Audit 为 nil（测试/灰度）时不得 panic —— 业务路径可以无脑调用 record。
	h := &Handler{}
	auditReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3", http.MethodDelete, "/media/m-3", func(r *gin.Engine) {
		r.DELETE("/media/:id", func(c *gin.Context) {
			h.record(c, audit.ActionMediaPurge, audit.TargetMedia, c.Param("id"), nil)
			c.Status(http.StatusOK)
		})
	})
}

// ---- 2. 失败路径不写审计（真实 handler + 不可达库）----

func TestMediaWriteOpsAuditNothingWhenStoreFails(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()

	fake := &fakeRecorder{}
	h := &Handler{
		Store: &Store{Pool: pool},
		Audit: audit.NewWithStore(fake, zap.NewNop()),
	}
	reg := func(r *gin.Engine) {
		r.DELETE("/media/:id", h.Delete)
		r.DELETE("/media/trash/:id", h.Purge)
		r.POST("/media/trash/:id/restore", h.Restore)
	}
	const uuid = "3f3d7bad-6795-4e48-a039-79c44b206c67"

	for _, tc := range []struct{ name, method, path string }{
		{"Delete", http.MethodDelete, "/media/" + uuid},
		{"Purge", http.MethodDelete, "/media/trash/" + uuid},
		{"Restore", http.MethodPost, "/media/trash/" + uuid + "/restore"},
	} {
		rec := auditReq(h, "7c6b1b9c-cba2-4394-b982-67d9038421f3", tc.method, tc.path, reg)
		if rec.Code == http.StatusOK {
			t.Fatalf("%s：库不可达时不该返回 200，实际 %d", tc.name, rec.Code)
		}
	}
	// 关键断言：一次都没成功，就一条审计都不该有。
	if len(fake.entries) != 0 {
		t.Fatalf("操作全部失败，却写入了 %d 条审计：%v —— "+
			"那会记下「谁删了/恢复了 X」而 X 根本没动，比不记录更危险", len(fake.entries), fake.entries)
	}
	// ⚠️ 本用例**能**证明的与**不能**证明的，写清楚免得被当成更强的保证：
	//   能：触库不可达时完全不写审计；record 若被挪进 checkAccess 会红。
	//   不能：**证明不了 record 排在 Store 调用之后** —— 库连不上时 handler 在
	//   checkAccess 就返回了，把 record 放在 checkAccess 之后、Store 调用之前，这里照样绿。
	// 所以顺序不变量由下面 TestMediaWriteHandlersAreWiredToAudit 按源码位置单独断言。
}

// ---- 3. 接线本身还在（源码形状）----

// handlerBody 抠出某个 handler 方法的函数体（到下一个顶层 func 为止）。
func handlerBody(t *testing.T, src, name string) string {
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

func TestMediaWriteHandlersAreWiredToAudit(t *testing.T) {
	b, err := os.ReadFile("write_handlers.go")
	if err != nil {
		t.Fatalf("读不到 write_handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	// broken 是「去掉 record 那一行」的坏版本，用于前提自证。
	// wants 是**全部**必须同时出现的片段：只断言 "调了 record" 挡不住
	// 「顺手把 detail 里的可解析线索删掉」—— 那正是不报错、只静默降级的坏改动。
	cases := []struct {
		fn        string
		wants     []string
		storeCall string
		broken    string
	}{
		{"Delete",
			[]string{"h.record(c, audit.ActionMediaDelete, audit.TargetMedia, id",
				`"owner_id": ownerID`},
			"h.Store.SoftDelete(",
			"func (h *Handler) Delete(c *gin.Context) {\n\tc.JSON(http.StatusOK, gin.H{})\n}"},
		{"Purge",
			[]string{"h.record(c, audit.ActionMediaPurge, audit.TargetMedia, id",
				`"path": path`},
			"h.Store.Purge(",
			"func (h *Handler) Purge(c *gin.Context) {\n\tc.JSON(http.StatusOK, gin.H{})\n}"},
		{"Restore",
			[]string{"h.record(c, audit.ActionMediaRestore, audit.TargetMedia, id",
				`"owner_id": ownerID`,
				`"path": path`},
			"h.Store.Restore(",
			"func (h *Handler) Restore(c *gin.Context) {\n\tc.JSON(http.StatusOK, gin.H{})\n}"},
	}
	for _, tc := range cases {
		body := handlerBody(t, src, tc.fn)
		for _, want := range tc.wants {
			if !strings.Contains(body, want) {
				t.Fatalf("%s 里找不到 %q。\n"+
					"这三个动作必须写审计，其中 media.purge 是**唯一不可恢复**的动作："+
					"它会把 media 行整行删除，此后 target_id 再也查不回任何东西 ——\n"+
					"审计行是「谁永久销毁了什么」仅存的证据。\n"+
					"media.restore 记 path 是同一理由（恢复后仍可能被 purge），"+
					"且该值**只有在恢复的当口才取得到**，事后补不回来。\n"+
					"若是有意改动，请同时更新本测试与 §二十 的接入清单。", tc.fn, want)
			}
		}
		// §二十四.1 的顺序不变量：record 必须在触库调用**之后**。
		// 这条只能按源码位置断言 —— 见 TestMediaWriteOpsAuditNothingWhenStoreFails 的说明：
		// 库不可达时 handler 在 checkAccess 就返回了，那种用例**证实不了**顺序。
		iStore := strings.Index(body, tc.storeCall)
		iRecord := strings.Index(body, tc.wants[0])
		if iStore < 0 {
			t.Fatalf("%s 里找不到触库调用 %q —— 方法被改名或重构？本守卫无法判定顺序",
				tc.fn, tc.storeCall)
		}
		if iRecord >= 0 && iRecord < iStore {
			t.Fatalf("%s：审计调用出现在 %q **之前**（下标 %d < %d）—— 违反 §二十四.1。"+
				"审计记的是**事实**不是**尝试**：失败/404 不该留下记录，"+
				"否则库里会出现「谁恢复了 X」而 X 根本没在回收站里。",
				tc.fn, tc.storeCall, iRecord, iStore)
		}
		// 前提自证：把 record 去掉后，同一条断言必须失败，否则这个守卫是空转的。
		if strings.Contains(handlerBody(t, tc.broken, tc.fn), tc.wants[0]) {
			t.Fatalf("守卫失效：本断言在去掉 record 的版本上也会通过（%s），拦不住回归", tc.fn)
		}
	}

	// 反向守卫：这两个 handler 里不该出现字符串字面量的动作名（必须引用 audit 常量）。
	// 字面量在改名时不会编译失败，而查询侧按精确匹配已登记的动作名 ⇒ 静默查不到。
	literal := regexp.MustCompile(`"media\.(delete|purge|restore)"`)
	for _, fn := range []string{"Delete", "Purge", "Restore"} {
		if m := literal.FindString(handlerBody(t, src, fn)); m != "" {
			t.Fatalf("%s 里出现了动作名字面量 %s：请改用 audit.ActionMedia* 常量"+
				"（字面量在改名时不会编译失败，而查询是按精确匹配的）", fn, m)
		}
	}
}
