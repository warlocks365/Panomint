package media

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- Content-Range 解析边界 ----

func TestParseContentRange(t *testing.T) {
	// 无头 = 整文件上传
	if cr, err := parseContentRange(""); err != nil || cr != nil {
		t.Fatalf("空头应返回 nil,nil，实际 %v,%v", cr, err)
	}
	// 正常首块
	cr, err := parseContentRange("bytes 0-99/300")
	if err != nil || cr.Start != 0 || cr.End != 99 || cr.Total != 300 {
		t.Fatalf("首块解析错误: %+v, %v", cr, err)
	}
	// 末尾块（end == total-1 合法）
	if _, err := parseContentRange("bytes 200-299/300"); err != nil {
		t.Fatalf("末尾块应合法: %v", err)
	}
	bad := []string{
		"bytes 100-99/300",  // start > end
		"bytes 0-300/300",    // end >= total
		"bytes 0-99/0",       // total 为 0
		"items 0-99/300",     // 单位错误
		"bytes 0-99",         // 缺 total
		"bytes -99/300",      // 缺 end
		"bytes 0--1/300",     // 负数
		"bytes a-b/c",        // 非数字
	}
	for _, s := range bad {
		if _, err := parseContentRange(s); err == nil {
			t.Fatalf("%q 应报错", s)
		}
	}
}

// ---- 文件名/目录清洗 ----

func TestSanitize(t *testing.T) {
	if got := sanitizeFilename(`..\..\evil.jpg`); got != "evil.jpg" {
		t.Fatalf("穿越文件名应被剥壳，实际 %q", got)
	}
	if got := sanitizeFilename(`sub\dir\a b.jpg`); got != "a b.jpg" {
		t.Fatalf("反斜杠路径应取基名，实际 %q", got)
	}
	if got := sanitizeFilename(`..`); got != "" {
		t.Fatalf(".. 应拒绝，实际 %q", got)
	}
	if got := sanitizeFolder(`a/./b/../c`); got != "a/c" {
		t.Fatalf("目录清洗错误，实际 %q", got)
	}
	if got := sanitizeFolder(`2024\08\旅行`); got != "2024/08/旅行" {
		t.Fatalf("中文目录应保留，实际 %q", got)
	}
}

// ---- 分块上传状态机（HTTP 层，不入库）----

// newChunkReq 构造一块分块请求。
func newChunkReq(t *testing.T, data []byte, start, end, total int64, uploadID string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fw, err := w.CreateFormFile("file", "chunk.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	if uploadID != "" {
		if err := w.WriteField("upload_id", uploadID); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/media/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
	return req
}

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	tmp := t.TempDir()
	return &Handler{UploadDir: filepath.Join(tmp, "media"), UploadTmp: filepath.Join(tmp, "uploads")}
}

func doUpload(h *Handler, req *http.Request) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1"); c.Set("role", "member") })
	r.POST("/media/upload", h.Upload)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestChunkedUploadFlow(t *testing.T) {
	h := newTestHandler(t)
	chunk := bytes.Repeat([]byte("a"), 100)

	// 首块：生成 upload_id
	rec := doUpload(h, newChunkReq(t, chunk, 0, 99, 300, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("首块应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		UploadID string `json:"upload_id"`
		Received int64  `json:"received"`
		Complete bool   `json:"complete"`
	}
	mustJSON(t, rec, &resp)
	if resp.UploadID == "" || resp.Received != 100 || resp.Complete {
		t.Fatalf("首块响应错误: %+v", resp)
	}

	// 乱序块（起点不符）：409
	rec = doUpload(h, newChunkReq(t, chunk, 200, 299, 300, resp.UploadID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("乱序块应 409，实际 %d: %s", rec.Code, rec.Body.String())
	}

	// 重传已完成块：幂等回报进度
	rec = doUpload(h, newChunkReq(t, chunk, 0, 99, 300, resp.UploadID))
	if rec.Code != http.StatusOK {
		t.Fatalf("重传首块应幂等 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	mustJSON(t, rec, &resp)
	if resp.Received != 100 {
		t.Fatalf("重传后 received 应仍为 100，实际 %d", resp.Received)
	}

	// 块大小与声明不符：400
	rec = doUpload(h, newChunkReq(t, chunk[:50], 100, 199, 300, resp.UploadID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("块大小不符应 400，实际 %d", rec.Code)
	}

	// 非法 upload_id（路径穿越特征）：400
	rec = doUpload(h, newChunkReq(t, chunk, 100, 199, 300, "../../etc"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 upload_id 应 400，实际 %d", rec.Code)
	}

	// 不存在的会话续传：404
	rec = doUpload(h, newChunkReq(t, chunk, 100, 199, 300, "0123456789abcdef"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("无会话续传应 404，实际 %d", rec.Code)
	}

	// 中间块正常追加（末块会触发入库，本用例不覆盖——需 DB，端到端验证）
	rec = doUpload(h, newChunkReq(t, chunk, 100, 199, 300, resp.UploadID))
	if rec.Code != http.StatusOK {
		t.Fatalf("中间块应 200，实际 %d: %s", rec.Code, rec.Body.String())
	}
	// .part 应为 200 字节
	st, err := os.Stat(filepath.Join(h.UploadTmp, resp.UploadID+".part"))
	if err != nil || st.Size() != 200 {
		t.Fatalf(".part 大小应为 200，实际 %v, %v", st, err)
	}
}

func mustJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("响应非 JSON: %s (%v)", rec.Body.String(), err)
	}
}

// ---- 游标编解码 ----

func TestCursorRoundTrip(t *testing.T) {
	ts := time.Date(2026, 8, 25, 12, 34, 56, 789, time.UTC)
	cur := encodeCursor(ts, "abc-123")
	gotT, gotID, err := decodeCursor(cur)
	if err != nil || gotID != "abc-123" || gotT.UnixNano() != ts.UnixNano() {
		t.Fatalf("游标往返失败: %v %q %v", gotT, gotID, err)
	}
	if _, _, err := decodeCursor("!!!not-base64!!!"); err == nil {
		t.Fatal("非法 base64 应报错")
	}
	if _, _, err := decodeCursor("aGVsbG8"); err == nil { // "hello"，无分隔符
		t.Fatal("无分隔符游标应报错")
	}
}
