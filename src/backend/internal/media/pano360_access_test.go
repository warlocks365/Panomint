package media

import (
	"os"
	"strings"
	"testing"
)

// Pano360 读口径守卫（收尾项，随 P2-01 读访问口径扩大一起收口）。
//
// 修复前：Pano360 用 checkAccess（写口径，仅属主），导致 shared 空间成员
// 「列表可见、详情可读（P2-01 已修）但 360 元数据 403」——同一条媒体在
// Detail 与 Pano360 两个读端点上口径断裂。修复后 Pano360 与 Detail/Download
// 同用 checkReadAccess（读口径：属主 ∪ shared 成员 ∪ owner/admin）。
//
// 本测试用源码断言钉住这个选择，防止后续维护把 Pano360 改回写口径。

func TestPano360UsesReadAccess(t *testing.T) {
	b, err := os.ReadFile("write_handlers.go")
	if err != nil {
		t.Fatalf("读不到 write_handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *Handler) Pano360(")
	if start < 0 {
		t.Fatal("write_handlers.go 里找不到 Pano360")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end]
	}
	if !strings.Contains(body, "h.checkReadAccess(c, id)") {
		t.Fatalf("Pano360 必须使用 checkReadAccess（读口径），函数体:\n%s", body)
	}
	if strings.Contains(body, "h.checkAccess(c, id)") {
		t.Fatal("Pano360 仍在用 checkAccess（写口径）—— shared 成员会被 403")
	}

	// 自证：修复前的坏版本（checkAccess）必须让断言落空。
	broken := "if _, ok := h.checkAccess(c, id); !ok {\n\t\treturn\n\t}"
	if !strings.Contains(broken, "h.checkAccess(c, id)") || strings.Contains(broken, "h.checkReadAccess(c, id)") {
		t.Fatal("守卫失效：坏版本无法被断言区分")
	}
}
