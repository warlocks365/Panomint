package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// 缩略图越权修复的回归网。
//
// 核心不变量：**「无权」与「不存在」必须不可区分**。修复前 Thumb 只按 id 取列、
// 不看归属，实测 viewer 账号对他人 media id 拿到 200 + image/webp，而同一个 id 走
// GET /media/:id 是 403 —— 缩略图成了绕过保护的侧门。若修成 403，则只是把一个泄漏
// 换成另一个泄漏（存在性预言机），所以断言必须钉住「404 而非 403」与「逐字节同形」。

// fakeLookup 充当 *Store.readAllowed（签名逐字相同，故可直接替换以便无库单测）。
// allowed=true 覆盖「属主 / shared 成员 / owner/admin」全部放行形态 —— 具体属于哪一种
// 由 readAccessOf → mediascope.ReadCond 在 SQL 层判定（该层有自己的测试），
// 本层只钉「判定结果的响应映射」。
func fakeLookup(allowed bool, err error) mediaReadLookup {
	return func(context.Context, string, string, string) (bool, error) { return allowed, err }
}

// ---- 判定层：无权必须被收敛成 ErrNotFound（= 与不存在同一档） ----

func TestThumbAccessCheckDeniesNonOwnerAsNotFound(t *testing.T) {
	lookup := fakeLookup(false, nil) // 非属主且非 shared 成员（readAccessOf 判 allowed=false）
	err := thumbAccessCheck(context.Background(), lookup, "u2", "member", "m1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("他人媒体必须判为 ErrNotFound（与「不存在」不可区分），实际 %v", err)
	}
}

func TestThumbAccessCheckAllowsOwnerAndPrivileged(t *testing.T) {
	lookup := fakeLookup(true, nil) // readAccessOf 放行（含 P2-01 的 shared 成员形态）
	for _, tc := range []struct{ user, role string }{
		{"u1", "member"}, // 本人
		{"u2", "member"}, // shared 空间成员（P2-01：列表可见的媒体，缩略图也必须可读）
		{"u2", "owner"},  // 全局 owner
		{"u2", "admin"},  // 管理员
	} {
		if err := thumbAccessCheck(context.Background(), lookup, tc.user, tc.role, "m1"); err != nil {
			t.Fatalf("user=%s role=%s 应放行（与 checkReadAccess 同口径），实际 %v", tc.user, tc.role, err)
		}
	}
}

func TestThumbAccessCheckMissingMediaIsNotFound(t *testing.T) {
	lookup := fakeLookup(false, ErrNotFound)
	if err := thumbAccessCheck(context.Background(), lookup, "u1", "member", "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的媒体应为 ErrNotFound，实际 %v", err)
	}
}

func TestThumbAccessCheckPropagatesDBError(t *testing.T) {
	dbErr := errors.New("db down")
	err := thumbAccessCheck(context.Background(), fakeLookup(false, dbErr), "u1", "member", "m1")
	if !errors.Is(err, dbErr) {
		t.Fatalf("DB 错误应原样透出（由 handler 映射 500），实际 %v", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("DB 错误不得被折叠成 404 —— 否则故障被伪装成「不存在」，且与无权同形而掩盖真实原因")
	}
}

// ---- 响应层：无权分支必须是 404，且与「不存在/缺图」逐字节同形 ----

func thumbCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/media/m1/thumb", nil)
	return c, rec
}

func TestThumbDenyIsNotFoundNotForbidden(t *testing.T) {
	c, rec := thumbCtx()
	writeThumbAccessError(c, ErrNotFound) // 无权 ⇒ 判定层返回 ErrNotFound
	if rec.Code != http.StatusNotFound {
		t.Fatalf("无权必须 404（403 会暴露「该 id 存在且属于他人」），实际 %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusForbidden {
		t.Fatal("无权不得返回 403")
	}
	code, msg := errEnvelope(t, rec)
	if code != "NOT_FOUND" || msg != "缩略图不存在" {
		t.Fatalf("无权响应应同「不存在」，实际 code=%q msg=%q", code, msg)
	}
	for _, leak := range []string{"FORBIDDEN", "无权", "owner"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("响应泄露了「存在但无权」的区分信息（%q）: %s", leak, rec.Body.String())
		}
	}
}

func TestThumbDenyIsByteIdenticalToMissingThumb(t *testing.T) {
	ca, ra := thumbCtx()
	writeThumbAccessError(ca, ErrNotFound) // 分支 1：无权
	cb, rb := thumbCtx()
	thumbNotFound(cb) // 分支 2/3：id 不存在 或 有行但无缩略图
	if ra.Code != rb.Code || ra.Body.String() != rb.Body.String() {
		t.Fatalf("「无权」与「不存在」必须逐字节同形\n无权=%d %s\n不存在=%d %s",
			ra.Code, ra.Body.String(), rb.Code, rb.Body.String())
	}
}

func TestThumbAccessDBErrorIs500(t *testing.T) {
	// 真故障必须与「不存在」分开：500，而不是被折叠成 404。
	// 同时钉住 (b) 类缺陷：500 的 message 绝不能是 err.Error() 原文。
	dbErr := errors.New(`ERROR: column "thumbnail_sm" does not exist (SQLSTATE 42703)`)
	c, rec := thumbCtx()
	writeThumbAccessError(c, dbErr)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("DB 错误应 500，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errEnvelope(t, rec); code != "QUERY_FAILED" {
		t.Fatalf("错误码应为 QUERY_FAILED，实际 %q", code)
	}
	if strings.Contains(rec.Body.String(), dbErr.Error()) {
		t.Fatalf("500 响应回显了数据库原文（信息泄漏）: %s", rec.Body.String())
	}
	for _, leak := range []string{"thumbnail_sm", "42703", "SQLSTATE", "column"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), strings.ToLower(leak)) {
			t.Fatalf("500 响应泄露内部实现细节（%q）: %s", leak, rec.Body.String())
		}
	}
}

// 回归：media.id 是 UUID 列，畸形 id 会让 PG 报 22P02。
// 改造前 Thumb 对这个错误走的是 `err != nil → 404`；若现在变成 500，不但行为回退，
// 还会把 `invalid input syntax for type uuid` 连同类型线索写进响应。
func TestThumbMalformedIDIs404Not500(t *testing.T) {
	c, rec := thumbCtx()
	writeThumbAccessError(c, &pgconn.PgError{
		Code:    "22P02",
		Message: `invalid input syntax for type uuid: "garbage"`,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("畸形 id 应 404（改造前即如此），实际 %d: %s", rec.Code, rec.Body.String())
	}
	// 与「不存在」逐字节同形，且不得透出 PG 原文
	cb, rb := thumbCtx()
	thumbNotFound(cb)
	if rec.Body.String() != rb.Body.String() {
		t.Fatalf("畸形 id 的响应必须与「不存在」同形\n畸形=%s\n不存在=%s", rec.Body.String(), rb.Body.String())
	}
	for _, leak := range []string{"uuid", "invalid input", "22P02", "syntax", "QUERY_FAILED"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Fatalf("响应泄露 PG 原文（%q）: %s", leak, rec.Body.String())
		}
	}
}

// IsMalformedID 的单元测试已随实现迁至 internal/pgxutil/pgerr_test.go（单一真源所在处）。
// 本包只保留「判定 → 响应」的集成口径：TestThumbMalformedIDIs404Not500 在上面。

// size 白名单必须在触库前生效（Handler.Store 为 nil：走到 DB 会 panic，故 400 即证明未触库）。
func TestThumbBadSizeRejectedBeforeDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1"); c.Set("role", "member") })
	r.GET("/media/:id/thumb", (&Handler{}).Thumb)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/m1/thumb?size=huge", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 size 应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, _ := errEnvelope(t, rec); code != "BAD_SIZE" {
		t.Fatalf("错误码应为 BAD_SIZE，实际 %q", code)
	}
}

// 接线断言：Thumb 必须真的调用 thumbAccess（否则函数在、门却不在）。
// 与 audit_wiring_test.go / mediaref_single_source_test.go 同类的源码级断言。
func TestThumbIsWiredToAccessCheck(t *testing.T) {
	b, err := os.ReadFile("thumb.go")
	if err != nil {
		t.Fatalf("读 thumb.go 失败: %v", err)
	}
	src := string(b)
	body := src[strings.Index(src, "func (h *Handler) Thumb("):]
	if !strings.Contains(body, "h.thumbAccess(c,") {
		t.Fatal("Thumb 未调用 thumbAccess：归属校验被摘掉了")
	}
	if n := strings.Count(src, `"缩略图不存在"`); n != 1 {
		t.Fatalf("「不存在」文案应只有一处（thumbNotFound，三条分支共用），实际 %d 处 —— 文案分叉会重新打开存在性预言机", n)
	}
}
