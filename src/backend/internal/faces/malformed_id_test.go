package faces

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 畸形 id 样本 + 泄漏标记（与 media/albums/transcode 的同名测试同口径）。
var (
	malformedIDPathSamples = []string{"not-a-uuid", "123", "..%2f.."}
	facesLeakMarkers       = []string{
		"uuid", "invalid input", "22p02", "syntax", "sqlstate", "pq:",
		"person_id", "faces", "media_id", "person_media",
	}
)

func assertNoFacesLeak(t *testing.T, where, body string, extra ...string) {
	t.Helper()
	low := strings.ToLower(body)
	for _, m := range append(append([]string{}, facesLeakMarkers...), extra...) {
		if strings.Contains(low, strings.ToLower(m)) {
			t.Fatalf("%s: 响应泄露内部实现细节 %q: %s", where, m, body)
		}
	}
}

func facesErrCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/people/x/media", nil)
	return c, rec
}

func facesEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

// unreachableFacesPool 指向必然连不上的地址（端口 1）。
func unreachableFacesPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/none")
	if err != nil {
		t.Fatalf("构造 pool 失败（本用例只用它连不上这一性质）: %v", err)
	}
	return pool
}

// TestPersonMediaMalformedIDIs404Not500 钉住 GET /people/:id/media。
// faces.person_id 是 uuid → 畸形 id 让 PG 抛 22P02；改造前落到
// `fail(500, "QUERY_FAILED", err.Error())`，既是 500，又把 PG 原文吐给客户端。
func TestPersonMediaMalformedIDIs404Not500(t *testing.T) {
	for _, id := range malformedIDPathSamples {
		c, rec := facesErrCtx()
		writePersonMediaError(c, &pgconn.PgError{
			Code:    "22P02",
			Message: `invalid input syntax for type uuid: "` + id + `"`,
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("id=%q：畸形 id 必须 404 而不是 500，实际 %d: %s", id, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusOK {
			t.Fatal("畸形 id 不得被当成「人物存在但无可见媒体」的 200 空列表")
		}
		if code, msg := facesEnvelope(t, rec); code != "NOT_FOUND" || msg != ErrPersonNotFound.Error() {
			t.Fatalf("id=%q：应为 NOT_FOUND/%q，实际 %q/%q", id, ErrPersonNotFound.Error(), code, msg)
		}
		assertNoFacesLeak(t, "GET /people/:id/media", rec.Body.String(), id)
	}

	// 真故障（非 22P02）：仍是 500，但 message 固定，不回显 DB 原文 / 列名。
	c, rec := facesErrCtx()
	writePersonMediaError(c, errors.New(`ERROR: column "person_id" does not exist (SQLSTATE 42703)`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("真故障应 500，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := facesEnvelope(t, rec); code != "QUERY_FAILED" || msg != "查询失败" {
		t.Fatalf("500 应为 QUERY_FAILED/查询失败，实际 %q/%q", code, msg)
	}
	assertNoFacesLeak(t, "GET /people/:id/media", rec.Body.String(), "42703", "column")
}

// TestPersonMediaDBFailureIs500AndDoesNotLeakDriverError 真实 HTTP 往返：
// Handler.Store.Pool 指向连不上的地址，pgx 返回真实驱动错误（含 host/port/dial 原文）。
// 改造前该错误被整段回显；现在必须 500 + 固定文案，且不得被伪装成 404。
func TestPersonMediaDBFailureIs500AndDoesNotLeakDriverError(t *testing.T) {
	pool := unreachableFacesPool(t)
	defer pool.Close()
	h := &Handler{Store: &Store{Pool: pool}}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "u1")
		c.Set("role", "viewer")
	})
	r.GET("/people/:id/media", h.PersonMedia)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/people/123/media", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("库连不上是真故障，必须 500 而不是被伪装成 404，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := facesEnvelope(t, rec); code != "QUERY_FAILED" || msg != "查询失败" {
		t.Fatalf("应为 QUERY_FAILED/查询失败，实际 %q/%q", code, msg)
	}
	for _, leak := range []string{"127.0.0.1", "dial", "connect", "refused", "postgres://", "pq:", "sqlstate"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
			t.Fatalf("500 响应回显了驱动错误原文（%q）: %s", leak, rec.Body.String())
		}
	}
}

// TestPersonMediaIsWiredToLookupError 接线断言 + 坏版本自证。
func TestPersonMediaIsWiredToLookupError(t *testing.T) {
	b, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatalf("读不到 api.go: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *Handler) PersonMedia(")
	if start < 0 {
		t.Fatal("源码里找不到 PersonMedia —— 方法被改名或删除？")
	}
	body := src[start:]
	if next := strings.Index(body[1:], "\nfunc "); next >= 0 {
		body = body[:next+1]
	}
	if !strings.Contains(body, "writePersonMediaError(c, err)") {
		t.Fatal("PersonMedia 未走 writePersonMediaError：畸形 id 会退回 500 并回显 PG 原文")
	}
	if strings.Contains(body, ", err.Error())") {
		t.Fatal("PersonMedia 内仍在回显 err.Error()：PG 原文会漏给客户端")
	}

	broken := "func (h *Handler) PersonMedia(c *gin.Context) {\n" +
		"\t_, err := h.Store.PersonMedia(c.Request.Context(), c.Param(\"id\"), \"\", 0)\n" +
		"\tif err != nil {\n" +
		"\t\tfail(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())\n" +
		"\t\treturn\n\t}\n}\n"
	if strings.Contains(broken, "writePersonMediaError") {
		t.Fatal("守卫失效：本断言在「无修复」的坏版本上也会通过，拦不住回归")
	}
}
