package index

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeScanner 替身：记录入参，onDone 由测试手动触发（模拟"任务还在跑"）。
type fakeScanner struct {
	gotRoot   string
	gotOwner  string
	onDone    func(error)
	returnErr error
	jobID     string
	calls     int
}

func (f *fakeScanner) ScanAsync(_ context.Context, root, ownerID string, onDone func(error)) (string, error) {
	f.calls++
	f.gotRoot = root
	f.gotOwner = ownerID
	f.onDone = onDone
	if f.returnErr != nil {
		return "", f.returnErr
	}
	return f.jobID, nil
}

func newScanTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/admin/scan", func(c *gin.Context) {
		if v := c.GetHeader("X-Test-User"); v != "" {
			c.Set("user_id", v)
		}
		h.Scan(c)
	})
	return r
}

func doScan(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/scan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-User", "user-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestResolveScanDir(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "photos", "trip")
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		dir     string
		want    string // 期望的绝对路径（"" 表示期望报错）
		wantErr bool
	}{
		{name: "空=媒体根本身", dir: "", want: root},
		{name: "点=媒体根本身", dir: ".", want: root},
		{name: "相对子目录", dir: "photos/trip", want: sub},
		{name: "相对带子目录穿越", dir: "photos/../photos/trip", want: sub},
		{name: "媒体根内绝对路径", dir: sub, want: sub},
		{name: "越界相对穿越", dir: "../etc", wantErr: true},
		{name: "越界多层穿越", dir: "photos/../../..", wantErr: true},
		{name: "越界卷根绝对路径", dir: volumeRoot(), wantErr: true},
		{name: "越界兄弟绝对目录", dir: filepath.Join(filepath.Dir(root), "other"), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveScanDir(root, tc.dir)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolveScanDir(%q) 应拒绝穿越，got %q", tc.dir, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveScanDir(%q): %v", tc.dir, err)
			}
			want, _ := filepath.Abs(tc.want)
			if got != filepath.Clean(want) {
				t.Fatalf("resolveScanDir(%q)=%q，want %q", tc.dir, got, filepath.Clean(want))
			}
		})
	}
}

func mkdirAll(p string) error { return os.MkdirAll(p, 0o755) }

// volumeRoot 返回当前文件系统的卷根（Windows 为 `C:\`，POSIX 为 `/`）——
// 一个天然位于任何临时媒体根之外的绝对路径，用于跨平台断言"绝对路径越界被拒"。
func volumeRoot() string {
	if v := filepath.VolumeName(os.TempDir()); v != "" {
		return v + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

func TestScanHandler_AcceptedAndOwnership(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos")); err != nil {
		t.Fatal(err)
	}
	fake := &fakeScanner{jobID: "job-abc"}
	h := &Handler{Indexer: fake, MediaRoot: root}
	r := newScanTestRouter(h)

	w := doScan(t, r, `{"dir":"photos"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["job_id"] != "job-abc" || resp["status"] != "running" {
		t.Fatalf("响应形状异常: %v", resp)
	}
	if resp["dir"] != "photos" {
		t.Fatalf("回显 dir=%v，want photos", resp["dir"])
	}
	// 归属真实调用者：ScanAsync 收到的 ownerID 必须来自 JWT 注入的 user_id，而非种子用户。
	if fake.gotOwner != "user-1" {
		t.Fatalf("ownerID=%q，want user-1（真实调用者）", fake.gotOwner)
	}
	wantRoot, _ := filepath.Abs(root)
	if fake.gotRoot != filepath.Join(wantRoot, "photos") {
		t.Fatalf("扫描根=%q，want %q", fake.gotRoot, filepath.Join(wantRoot, "photos"))
	}
}

func TestScanHandler_ConflictThenRelease(t *testing.T) {
	root := t.TempDir()
	fake := &fakeScanner{jobID: "job-1"}
	h := &Handler{Indexer: fake, MediaRoot: root}
	r := newScanTestRouter(h)

	// 第一次：受理（替身不触发 onDone → 任务"还在跑"）。
	if w := doScan(t, r, `{}`); w.Code != http.StatusAccepted {
		t.Fatalf("首次触发 status=%d", w.Code)
	}
	// 第二次：互斥命中 → 409。
	w := doScan(t, r, `{}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("并发触发应 409，got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SCAN_RUNNING") {
		t.Fatalf("409 应带 SCAN_RUNNING：%s", w.Body.String())
	}
	// 任务完成回调 → 释放 → 第三次恢复受理。
	fake.onDone(nil)
	if w := doScan(t, r, `{}`); w.Code != http.StatusAccepted {
		t.Fatalf("释放后应恢复 202，got %d", w.Code)
	}
}

func TestScanHandler_RejectsTraversalAndMissing(t *testing.T) {
	root := t.TempDir()
	fake := &fakeScanner{jobID: "job-x"}
	h := &Handler{Indexer: fake, MediaRoot: root}
	r := newScanTestRouter(h)

	// 路径穿越
	if w := doScan(t, r, `{"dir":"../../etc"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("穿越应 400，got %d", w.Code)
	}
	// 目录不存在
	if w := doScan(t, r, `{"dir":"no-such-dir"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("不存在目录应 400，got %d", w.Code)
	}
	// 非 JSON
	if w := doScan(t, r, `not-json`); w.Code != http.StatusBadRequest {
		t.Fatalf("非 JSON 应 400，got %d", w.Code)
	}
	// 非目录（指向一个文件）
	file := filepath.Join(root, "a.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if w := doScan(t, r, `{"dir":"a.jpg"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("文件应 400，got %d", w.Code)
	}
	if fake.calls != 0 {
		t.Fatalf("全部非法输入都不应触达扫描器，calls=%d", fake.calls)
	}
}

func TestScanHandler_ScannerErrorClearsMutex(t *testing.T) {
	root := t.TempDir()
	fake := &fakeScanner{returnErr: errors.New("db down")}
	h := &Handler{Indexer: fake, MediaRoot: root}
	r := newScanTestRouter(h)

	if w := doScan(t, r, `{}`); w.Code != http.StatusInternalServerError {
		t.Fatalf("建任务失败应 500，got %d", w.Code)
	}
	// 互斥必须被释放：下一次（换成正常替身语义）能重新受理。
	fake.returnErr = nil
	fake.jobID = "job-2"
	if w := doScan(t, r, `{}`); w.Code != http.StatusAccepted {
		t.Fatalf("失败后互斥未释放，got %d", w.Code)
	}
}
