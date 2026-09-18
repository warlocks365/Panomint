package albums

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// 畸形 id（非 UUID）样本 + 泄漏标记集。口径与 internal/media 的同名测试一致
// （单一判据，避免各包自定一套后漂移）。
var (
	malformedIDPathSamples = []string{"not-a-uuid", "123", "..%2f.."}
	albumLeakMarkers       = []string{
		"uuid", "invalid input", "22p02", "syntax", "sqlstate", "pq:",
		"album_items", "cover_media_id", "owner_id", "deleted_at",
	}
)

func assertNoAlbumLeak(t *testing.T, where, body string, extra ...string) {
	t.Helper()
	low := strings.ToLower(body)
	for _, m := range append(append([]string{}, albumLeakMarkers...), extra...) {
		if strings.Contains(low, strings.ToLower(m)) {
			t.Fatalf("%s: 响应泄露内部实现细节 %q: %s", where, m, body)
		}
	}
}

func albumErrCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/albums/x", nil)
	return c, rec
}

// TestGetAlbumMalformedIDIs404Not500 钉住 GET /albums/:id：
// albums.id 是 UUID 列 → 畸形 id 让 PG 抛 22P02。改造前它落到
// `errResp(500, "QUERY_FAILED", err.Error())`，既是 500 又把 PG 原文回了客户端。
func TestGetAlbumMalformedIDIs404Not500(t *testing.T) {
	for _, id := range malformedIDPathSamples {
		c, rec := albumErrCtx()
		writeAlbumLookupError(c, &pgconn.PgError{
			Code:    "22P02",
			Message: `invalid input syntax for type uuid: "` + id + `"`,
		})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("id=%q：畸形 id 必须 404 而不是 500，实际 %d: %s", id, rec.Code, rec.Body.String())
		}
		code, msg := albumEnvelope(t, rec)
		if code != "NOT_FOUND" || msg != "相册不存在" {
			t.Fatalf("id=%q：应为 NOT_FOUND/相册不存在，实际 %q/%q", id, code, msg)
		}
		assertNoAlbumLeak(t, "GET /albums/:id", rec.Body.String(), id)

		// 与「相册不存在」逐字节同形（契约用字面量复刻，不与被测函数自比）
		c2, rec2 := albumErrCtx()
		errResp(c2, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		if rec.Body.String() != rec2.Body.String() {
			t.Fatalf("畸形 id 必须与「相册不存在」同形\n畸形=%s\n不存在=%s", rec.Body.String(), rec2.Body.String())
		}
	}

	// (b) 真故障：仍是 500，但不得回显 DB 原文 / 列名。
	c, rec := albumErrCtx()
	writeAlbumLookupError(c, errors.New(`ERROR: column "cover_media_id" does not exist (SQLSTATE 42703)`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("真故障应 500，实际 %d: %s", rec.Code, rec.Body.String())
	}
	if code, msg := albumEnvelope(t, rec); code != "QUERY_FAILED" || msg != "查询失败" {
		t.Fatalf("500 应为 QUERY_FAILED/查询失败，实际 %q/%q", code, msg)
	}
	assertNoAlbumLeak(t, "GET /albums/:id", rec.Body.String(), "42703", "column")
}

func albumEnvelope(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是错误封套: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code, body.Error.Message
}

// TestAlbumGetIsWiredToLookupError 接线断言 + 坏版本自证：
// 映射函数必须在 Get 里被调用，且 Get 内不得再出现 err.Error()。
func TestAlbumGetIsWiredToLookupError(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	body := handlerBody(t, string(b), "Get")
	if !strings.Contains(body, "writeAlbumLookupError(c, err)") {
		t.Fatal("Get 未走 writeAlbumLookupError：畸形 id 会退回 500 并回显 PG 原文")
	}
	if strings.Contains(body, ", err.Error())") {
		t.Fatal("Get 内仍在回显 err.Error()：PG 原文会漏给客户端")
	}

	broken := handlerBody(t, "func (h *Handler) Get(c *gin.Context) {\n"+
		"\tid := c.Param(\"id\")\n"+
		"\townerID, _, err := h.Store.getAlbumMeta(c.Request.Context(), id)\n"+
		"\tif err != nil {\n"+
		"\t\terrResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())\n"+
		"\t\treturn\n\t}\n\t_ = ownerID\n}\n", "Get")
	if strings.Contains(broken, "writeAlbumLookupError") {
		t.Fatal("守卫失效：本断言在「无修复」的坏版本上也会通过，拦不住回归")
	}
}
