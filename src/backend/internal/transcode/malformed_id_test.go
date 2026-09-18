package transcode

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// 畸形 id 样本 + 泄漏标记（与 media/albums/faces 的同名测试同口径，避免各包自定一套后漂移）。
var (
	malformedIDPathSamples = []string{"not-a-uuid", "123", "..%2f.."}
	transLeakMarkers       = []string{
		"uuid", "invalid input", "22p02", "syntax", "sqlstate", "pq:",
		"transcode_jobs", "result_path", "media_id",
	}
)

func assertNoTransLeak(t *testing.T, where, body string, extra ...string) {
	t.Helper()
	low := strings.ToLower(body)
	for _, m := range append(append([]string{}, transLeakMarkers...), extra...) {
		if strings.Contains(low, strings.ToLower(m)) {
			t.Fatalf("%s: 响应泄露内部实现细节 %q: %s", where, m, body)
		}
	}
}

func transErrCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/transcode/job/x", nil)
	return c, rec
}

func transEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

func readTranscodeSrc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("transcode.go")
	if err != nil {
		t.Fatalf("读不到 transcode.go（测试需在包目录下运行）: %v", err)
	}
	return string(b)
}

// TestJobStatusMalformedIDIs404Not500 钉住 GET /transcode/job/:id。
// transcode_jobs.id 是 UUID 列 → 畸形 id 让 PG 抛 22P02；改造前落到
// `errJSON(500, "QUERY_FAILED", err.Error())`：既是 500，又把 PG 原文吐给客户端。
func TestJobStatusMalformedIDIs404Not500(t *testing.T) {
	for _, id := range malformedIDPathSamples {
		c, rec := transErrCtx()
		writeJobLookupError(c, &pgconn.PgError{
			Code:    "22P02",
			Message: `invalid input syntax for type uuid: "` + id + `"`,
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("id=%q：畸形 id 必须 404 而不是 500，实际 %d: %s", id, rec.Code, rec.Body.String())
		}
		if code, msg := transEnvelope(t, rec); code != "NOT_FOUND" || msg != "任务不存在" {
			t.Fatalf("id=%q：应为 NOT_FOUND/任务不存在，实际 %q/%q", id, code, msg)
		}
		assertNoTransLeak(t, "GET /transcode/job/:id", rec.Body.String(), id)

		// 与「任务不存在」（pgx.ErrNoRows 分支）逐字节同形；契约用字面量复刻，不与被测函数自比。
		c2, rec2 := transErrCtx()
		errJSON(c2, http.StatusNotFound, "NOT_FOUND", "任务不存在")
		if rec.Body.String() != rec2.Body.String() {
			t.Fatalf("畸形 id 必须与「任务不存在」同形\n畸形=%s\n不存在=%s", rec.Body.String(), rec2.Body.String())
		}
	}

	// 真故障（非 22P02、非 NoRows）：仍是 500，但 message 固定，不回显 DB 原文。
	c, rec := transErrCtx()
	writeJobLookupError(c, errors.New(`ERROR: column "result_path" does not exist (SQLSTATE 42703)`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("真故障应 500，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := transEnvelope(t, rec); code != "QUERY_FAILED" || msg != "查询失败" {
		t.Fatalf("500 应为 QUERY_FAILED/查询失败，实际 %q/%q", code, msg)
	}
	assertNoTransLeak(t, "GET /transcode/job/:id", rec.Body.String(), "42703", "column")

	// 反向：pgx.ErrNoRows 必须仍是 404（不能被当成故障变 500）。
	c3, rec3 := transErrCtx()
	writeJobLookupError(c3, pgx.ErrNoRows)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("ErrNoRows 必须 404，实际 %d: %s", rec3.Code, rec3.Body.String())
	}
}

// TestJobStatusDBFailureIs500AndDoesNotLeakDriverError 是**真实 HTTP 往返**的守卫：
// Handler.Pool 指向必然连不上的地址（端口 1），pgx 返回真实驱动错误（含 host/port/dial 原文）。
// 改造前该错误被整段回显；现在必须 500 + 固定文案，且不得被伪装成 404。
func TestJobStatusDBFailureIs500AndDoesNotLeakDriverError(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()
	h := &Handler{Pool: pool}

	rec := transReq(h, otherUUID, "viewer", http.MethodGet, "/transcode/job/"+jobUUID,
		func(r *gin.Engine) { r.GET("/transcode/job/:id", h.JobStatus) })

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("库连不上是真故障，必须 500 而不是被伪装成 404，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := transEnvelope(t, rec); code != "QUERY_FAILED" || msg != "查询失败" {
		t.Fatalf("应为 QUERY_FAILED/查询失败，实际 %q/%q", code, msg)
	}
	// 驱动错误原文里含这些标记；回显 err.Error() 时本断言必红。
	for _, leak := range []string{"127.0.0.1", "dial", "connect", "refused", "postgres://", "pq:", "sqlstate"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), leak) {
			t.Fatalf("500 响应回显了驱动错误原文（%q）: %s", leak, rec.Body.String())
		}
	}
}

// TestJobStatusIsWiredToLookupError 接线断言 + 坏版本自证。
func TestJobStatusIsWiredToLookupError(t *testing.T) {
	src := readTranscodeSrc(t)
	js := methodBody(t, src, "JobStatus")
	if !strings.Contains(js, "writeJobLookupError(c, err)") {
		t.Fatal("JobStatus 未走 writeJobLookupError：畸形 id 会退回 500 并回显 PG 原文")
	}
	if strings.Contains(js, ", err.Error())") {
		t.Fatal("JobStatus 内仍在回显 err.Error()：PG 原文会漏给客户端")
	}

	broken := methodBody(t, "func (h *Handler) JobStatus(c *gin.Context) {\n"+
		"\t_, err := h.Pool.QueryRow(c.Request.Context(), `SELECT 1`).Scan()\n"+
		"\tif err != nil {\n"+
		"\t\terrJSON(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())\n"+
		"\t\treturn\n\t}\n}\n", "JobStatus")
	if strings.Contains(broken, "writeJobLookupError") {
		t.Fatal("守卫失效：本断言在「无修复」的坏版本上也会通过，拦不住回归")
	}
}
