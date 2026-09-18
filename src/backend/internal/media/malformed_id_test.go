package media

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// 畸形 id（非 UUID 的路径参数）样本。三者都必须走到 404，且都不得出现在响应体里。
var malformedIDPathSamples = []string{"not-a-uuid", "123", "..%2f.."}

// mediaLeakMarkers 是「响应把内部实现细节吐给客户端」的判定标记集（大小写不敏感）。
// 口径与 thumb_access_test.go 的 TestThumbMalformedIDIs404Not500 一致，并按本次要求
// 追加 media 表的列名（PG 原文若被回显，往往连带类型/列线索）。
var mediaLeakMarkers = []string{
	"uuid", "invalid input", "22p02", "syntax", "sqlstate", "pq:",
	"thumbnail_sm", "thumbnail_lg", "owner_id", "deleted_at", "taken_at", "media_tags",
}

func assertNoClientLeak(t *testing.T, where, body string, extra ...string) {
	t.Helper()
	low := strings.ToLower(body)
	for _, m := range append(append([]string{}, mediaLeakMarkers...), extra...) {
		if strings.Contains(low, strings.ToLower(m)) {
			t.Fatalf("%s: 响应泄露内部实现细节 %q: %s", where, m, body)
		}
	}
}

// malformedIDPgError 构造 Postgres 对非法 uuid 文本的真实报错形状。
func malformedIDPgError(id string) error {
	return &pgconn.PgError{Code: "22P02", Message: `invalid input syntax for type uuid: "` + id + `"`}
}

func errorCtx(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, rec
}

// TestMalformedIDMediaEndpointsIs404Not500 钉住 media 包两个受影响的端点：
//   - GET /media/:id       （Detail → checkAccess → writeLookupError）
//   - GET /tags/:id/media  （ListTagMedia → writeTagLookupError / tagNotFound）
//
// 每条断言都对应一个真实缺陷，且都能被"改回 500 + err.Error()"的改动弄红：
//  1. 畸形 id 必须 404（改造前是 500）；
//  2. 响应体不得含 PG 原文 / SQLSTATE / 类型名 / media 表列名 / id 样本本身；
//  3. 必须与「记录真的不存在」逐字节同形（否则是可区分的探测判据）；
//  4. 真故障仍 500，但同样不得回显内部原文（(b) 类缺陷）。
func TestMalformedIDMediaEndpointsIs404Not500(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string // 含一个 %s，填入畸形 id
		writeErr func(*gin.Context, error)
		// writeGone 复刻「记录真的不存在」的**契约**（而不是调用被测的同一个函数，
		// 否则自比自、断言永远不会红）。畸形 id 必须产出与它逐字节相同的响应。
		writeGone func(*gin.Context)
		wantMsg   string
	}{
		{
			name:     "GET /media/:id",
			endpoint: "/media/%s",
			writeErr: writeLookupError,
			writeGone: func(c *gin.Context) {
				errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在")
			},
			wantMsg: "媒体不存在",
		},
		{
			name:     "GET /tags/:id/media",
			endpoint: "/tags/%s/media",
			writeErr: writeTagLookupError,
			writeGone: func(c *gin.Context) {
				errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
			},
			wantMsg: "标签不存在",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, id := range malformedIDPathSamples {
				c, rec := errorCtx(fmt.Sprintf(tc.endpoint, id))
				tc.writeErr(c, malformedIDPgError(id))

				if rec.Code != http.StatusNotFound {
					t.Fatalf("id=%q：畸形 id 必须 404 而不是 500，实际 %d: %s", id, rec.Code, rec.Body.String())
				}
				code, msg := errEnvelope(t, rec)
				if code != "NOT_FOUND" || msg != tc.wantMsg {
					t.Fatalf("id=%q：应为 NOT_FOUND/%q，实际 %q/%q", id, tc.wantMsg, code, msg)
				}
				assertNoClientLeak(t, tc.name, rec.Body.String(), id)

				// 与「记录真的不存在」逐字节同形
				c2, rec2 := errorCtx(fmt.Sprintf(tc.endpoint, id))
				tc.writeGone(c2)
				if rec.Body.String() != rec2.Body.String() {
					t.Fatalf("%s：畸形 id 的响应必须与「不存在」同形（否则是可区分的探测判据）\n畸形=%s\n不存在=%s",
						tc.name, rec.Body.String(), rec2.Body.String())
				}
			}

			// (b) 真故障：仍旧 500，但 message 固定，绝不回显 DB 原文/列名。
			c, rec := errorCtx(fmt.Sprintf(tc.endpoint, "x"))
			tc.writeErr(c, errors.New(`ERROR: column "thumbnail_sm" does not exist (SQLSTATE 42703)`))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("%s：真故障应 500，实际 %d: %s", tc.name, rec.Code, rec.Body.String())
			}
			code, msg := errEnvelope(t, rec)
			if code != "QUERY_FAILED" || msg != "查询失败" {
				t.Fatalf("%s：500 应为 QUERY_FAILED/查询失败，实际 %q/%q", tc.name, code, msg)
			}
			assertNoClientLeak(t, tc.name, rec.Body.String(), "42703", "column")
		})
	}
}

// TestMediaMalformedIDGuardsAreWired 接线断言：映射函数必须真的被 handler 调用，
// 否则「函数在、门不在」——上面那组响应断言会变成空转。
// 含「坏版本必须落空」的前提自证，形状与 albums/ownership_test.go 一致。
func TestMediaMalformedIDGuardsAreWired(t *testing.T) {
	wh, err := os.ReadFile("write_handlers.go")
	if err != nil {
		t.Fatalf("读不到 write_handlers.go（测试需在包目录下运行）: %v", err)
	}
	ca := handlerBody(t, string(wh), "checkAccess")
	if !strings.Contains(ca, "writeLookupError(c, err)") {
		t.Fatal("checkAccess 未走 writeLookupError：畸形 id 的 404 与「不回显原文」都会失效")
	}
	if strings.Contains(ca, ", err.Error())") {
		t.Fatal("checkAccess 里仍在回显 err.Error()：PG 原文会漏给客户端")
	}

	tg, err := os.ReadFile("tag_handlers.go")
	if err != nil {
		t.Fatalf("读不到 tag_handlers.go: %v", err)
	}
	body := handlerBody(t, string(tg), "ListTagMedia")
	for _, want := range []string{"writeTagLookupError(c, err)", "tagNotFound(c)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("ListTagMedia 里找不到 %q：畸形 id 的 404 或与「标签不存在」同形会被破坏", want)
		}
	}
	// 注意匹配 "…, err.Error())" 这一调用形态，而不是裸的 err.Error()：
	// 注释里提到 err.Error() 不算回显（本项目曾因断言写得太宽而误报）。
	if strings.Contains(body, ", err.Error())") {
		t.Fatal("ListTagMedia 里仍在回显 err.Error()：PG 原文会漏给客户端")
	}

	// 前提自证：坏版本（直接把 err.Error() 写进 500）必须让上面的断言落空。
	broken := handlerBody(t, "func (h *Handler) checkAccess(c *gin.Context, id string) (string, bool) {\n"+
		"\t_, _, err := h.Store.ownerOf(c.Request.Context(), id)\n"+
		"\tif err != nil {\n"+
		"\t\terrResp(c, http.StatusInternalServerError, \"QUERY_FAILED\", err.Error())\n"+
		"\t\treturn \"\", false\n\t}\n\treturn \"\", true\n}\n", "checkAccess")
	if strings.Contains(broken, "writeLookupError") {
		t.Fatal("守卫失效：本断言在「无修复」的坏版本上也会通过，拦不住回归")
	}
}
