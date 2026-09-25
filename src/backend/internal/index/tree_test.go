package index

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTreeTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/admin/fs/tree", h.ListDirTree)
	return r
}

func doTree(t *testing.T, r *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/fs/tree?"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

type treeResp struct {
	Root       string      `json:"root"`
	Dir        string      `json:"dir"`
	Unreadable bool        `json:"unreadable"`
	Items      []treeEntry `json:"items"`
}

func decodeTree(t *testing.T, w *httptest.ResponseRecorder) treeResp {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp treeResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestListDirTree_ListsOnlySubdirsAndLazily(t *testing.T) {
	root := t.TempDir()
	// a/b/c 三层 + 一个文件：根层只应看到 a；逐层展开各见一层。
	if err := mkdirAll(filepath.Join(root, "a", "b", "c")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	// 根层：只有目录 a，文件不上树。
	resp := decodeTree(t, doTree(t, r, "dir="))
	if resp.Dir != "." || resp.Unreadable {
		t.Fatalf("根层响应异常: %+v", resp)
	}
	if len(resp.Items) != 1 || resp.Items[0].Name != "a" || resp.Items[0].Rel != "a" {
		t.Fatalf("根层 items=%+v，want 仅 a", resp.Items)
	}
	if !resp.Items[0].Readable {
		t.Fatalf("可读的 a 不应被标锁定: %+v", resp.Items[0])
	}

	// 展开 a：只有 b。
	resp = decodeTree(t, doTree(t, r, "dir=a"))
	if len(resp.Items) != 1 || resp.Items[0].Name != "b" || resp.Items[0].Rel != "a/b" {
		t.Fatalf("a 层 items=%+v，want 仅 b（rel=a/b）", resp.Items)
	}

	// 展开 a/b：只有 c。
	resp = decodeTree(t, doTree(t, r, "dir=a/b"))
	if len(resp.Items) != 1 || resp.Items[0].Rel != "a/b/c" {
		t.Fatalf("a/b 层 items=%+v，want 仅 c", resp.Items)
	}
}

func TestListDirTree_EmptyDirReturnsEmptyItems(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "empty")); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	resp := decodeTree(t, doTree(t, r, "dir=empty"))
	if resp.Unreadable || len(resp.Items) != 0 {
		t.Fatalf("空目录应 200 + 空 items: %+v", resp)
	}
}

func TestListDirTree_RejectsTraversalMissingAndFile(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos")); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "a.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	cases := []struct{ name, query string }{
		{"穿越", "dir=" + filepath.ToSlash(filepath.Join("..", "etc"))},
		{"不存在", "dir=no-such"},
		{"文件", "dir=a.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if w := doTree(t, r, tc.query); w.Code != http.StatusBadRequest {
				t.Fatalf("应 400，got %d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestListDirTree_UnreadableChildFlaggedNotErrored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 权限语义在 Windows 不生效，锁定态由 Linux 侧验收")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := mkdirAll(locked); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) }) // 恢复权限，否则 t.TempDir 清理失败
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	// 无权限的子目录：照常列出但 readable=false（锁定态），整请求不报错。
	resp := decodeTree(t, doTree(t, r, "dir="))
	if len(resp.Items) != 1 || resp.Items[0].Readable {
		t.Fatalf("锁定目录应列出且 readable=false: %+v", resp.Items)
	}
}

func TestListDirTree_UnreadableTargetReturnsFlagNotError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 权限语义在 Windows 不生效")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	if err := mkdirAll(locked); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	// 展开的目标目录本身无权限：200 + unreadable=true + 空 items（不报错）。
	resp := decodeTree(t, doTree(t, r, "dir=locked"))
	if !resp.Unreadable || len(resp.Items) != 0 {
		t.Fatalf("应 unreadable=true + 空 items: %+v", resp)
	}
}

func TestListDirTree_SymlinkExcluded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink 在 Windows 需要提权，符号链接排除由 Linux 侧验收")
	}
	root := t.TempDir()
	outside := t.TempDir() // MEDIA_ROOT 之外的目录
	if err := os.Symlink(outside, filepath.Join(root, "link-out")); err != nil {
		t.Fatal(err)
	}
	if err := mkdirAll(filepath.Join(root, "real")); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root}
	r := newTreeTestRouter(h)

	// 指向根外目录的符号链接不得出现在树上（防借符号链接把树引出 MEDIA_ROOT）。
	resp := decodeTree(t, doTree(t, r, "dir="))
	if len(resp.Items) != 1 || resp.Items[0].Name != "real" {
		t.Fatalf("符号链接应被排除，items=%+v", resp.Items)
	}
}
