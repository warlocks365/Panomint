package albums

// 相册子资源路由的归属校验守卫（参照 internal/httperr errtext_guard 的机械检查思路）。
//
// 背景：§二十六/0ec9a78 修了 GET /albums/:id 本体的越权，却漏掉了它下面的两条评论
// 子资源路由（P0-01）——「可见性覆盖面没被定义」这类疏漏靠人工 checklist 兜不住，
// 因为新增子资源路由的人没有理由回头检查评论端点。
//
// 所以这里把覆盖面变成可执行断言：扫描 cmd/api/main.go 注册的全部 /albums/:id/...
// 子资源路由，断言其 handler 函数体都经过 getAlbumMeta（归属判定的唯一入口）。
// 新增子资源路由而忘记归属校验时，本测试即红。

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// backendRoot 由本测试文件位置反推 src/backend 根目录（不依赖 go test 的 cwd 约定）。
func backendRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller 失败，无法定位仓库根")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// subresourceRouteRe 匹配 main.go 里的相册子资源注册行，如：
// authed.GET("/albums/:id/comments", permRead, albumsH.ListComments)
// 只匹配 "/albums/:id/..."（带子路径的）—— /albums 与 /albums/:id 本体不在此列。
var subresourceRouteRe = regexp.MustCompile(
	`authed\.(GET|POST|PATCH|DELETE)\("(/albums/:id/[^"]+)"[^)]*albumsH\.(\w+)\)`)

func TestAlbumSubresourceRoutesAreOwnershipGuarded(t *testing.T) {
	mainPath := filepath.Join(backendRoot(t), "cmd", "api", "main.go")
	mb, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("读不到 %s: %v", mainPath, err)
	}
	hb, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(hb)

	matches := subresourceRouteRe.FindAllStringSubmatch(string(mb), -1)
	if len(matches) == 0 {
		t.Fatal("main.go 里一条 /albums/:id/... 子资源路由都没匹配到 —— 正则失效或路由全被删？")
	}

	// 评论三条路由必须在内（本守卫存在的第一动机），缺了说明注册被改而测试没跟上。
	found := map[string]bool{}
	for _, m := range matches {
		method, path, handler := m[1], m[2], m[3]
		found[method+" "+path] = true

		body := handlerBody(t, src, handler)
		if !strings.Contains(body, "h.Store.getAlbumMeta(") {
			t.Errorf("%s %s → %s：函数体未经 getAlbumMeta 归属校验，"+
				"相册子资源对非属主敞开（P0-01 同类回归）", method, path, handler)
		}
	}
	for _, want := range []string{
		"GET /albums/:id/comments",
		"POST /albums/:id/comments",
		"DELETE /albums/:id/comments/:cid",
	} {
		if !found[want] {
			t.Errorf("main.go 里找不到路由 %s —— 评论子资源注册被改动，本守卫需同步", want)
		}
	}
}
