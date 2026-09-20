package media

// Job000055 WebDAV 测试：davPath 纯函数穷举 + 源码结构守卫（读写语义/可见性口径/路由方法集）。
// 端到端（PROPFIND/PUT/MOVE/DELETE 真链路）由部署后 curl 实测承担（登记簿证据）。

import (
	"os"
	"strings"
	"testing"
)

func TestDavPath(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		folder string
		file   string
		isDir  bool
	}{
		{"根", "/", "", "", true},
		{"根无斜杠", "", "", "", true},
		{"单层无尾斜杠→文件位（Stat 层回退目录解析）", "/2024", "", "2024", false},
		{"目录尾斜杠", "/2024/", "", "2024", true},
		{"目录下文件", "/2024/06/a.jpg", "2024/06", "a.jpg", false},
		{"深层", "/a/b/c/d.png", "a/b/c", "d.png", false},
		{"点段被规范化", "/a/./b/x.jpg", "a/b", "x.jpg", false},
		{"目录尾斜杠深层", "/a/b/", "a", "b", true},
	}
	for _, tc := range cases {
		folder, file, isDir, err := davPath(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if folder != tc.folder || file != tc.file || isDir != tc.isDir {
			t.Fatalf("%s: want (%q,%q,%v) got (%q,%q,%v)",
				tc.name, tc.folder, tc.file, tc.isDir, folder, file, isDir)
		}
	}
}

// TestDavSemanticsGuard 钉住 dav.go 的安全语义（读路径即源码断言，防后续改动漂移）：
// 1) 全部读查询 owner 限定 + mediascope.ReadCond 双保险；2) PUT 走 ingest（与上传同管线）；
// 3) DELETE 落 SoftDelete（软删，可恢复）；4) MOVE 只动 folder_path/filename 不动存储实体。
func TestDavSemanticsGuard(t *testing.T) {
	b, err := os.ReadFile("dav.go")
	if err != nil {
		t.Fatalf("读不到 dav.go: %v", err)
	}
	src := string(b)
	checks := map[string]string{
		"listChildren 走 mediascope": "mediascope.ReadCond(3, fs.userID, fs.role",
		"findByPath 走 mediascope":   "mediascope.ReadCond(4, fs.userID, fs.role",
		"PUT 走同一 ingest 管线":        "p.fs.h.ingest(",
		"DELETE 软删（非物理删）":        "fs.h.Store.SoftDelete(",
		"MOVE 只改 folder_path/filename": "UPDATE media SET folder_path = NULLIF($2,''), filename = $3",
		"空间限定 personal":            "m.space = 'personal'",
		"未删除限定":                    "m.deleted_at IS NULL",
	}
	for name, m := range checks {
		if !strings.Contains(src, m) {
			t.Fatalf("dav.go 缺少「%s」（%q）", name, m)
		}
	}
	// 反向钉：不得出现物理删除/直接改存储路径的语句
	for _, banned := range []string{"os.RemoveAll(fs.h", "DELETE FROM media", "UPDATE media SET path"} {
		if strings.Contains(src, banned) {
			t.Fatalf("dav.go 出现违禁操作 %q（软删/组织语义被破坏）", banned)
		}
	}
}

// TestDavRoutesGuard 钉住 main.go 的方法集与挂载点（漏一个方法客户端就 405）。
func TestDavRoutesGuard(t *testing.T) {
	var b []byte
	var err error
	for _, p := range []string{"../cmd/api/main.go", "../../cmd/api/main.go"} {
		if b, err = os.ReadFile(p); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("读不到 main.go: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "WebDAV 直读直写（Job000055")
	if start < 0 {
		t.Fatal("main.go 找不到 WebDAV 注册块")
	}
	body := src[start : start+700]
	for _, m := range []string{`"GET"`, `"HEAD"`, `"PUT"`, `"DELETE"`, `"PROPFIND"`, `"MKCOL"`, `"MOVE"`, `"OPTIONS"`, `"LOCK"`, `"UNLOCK"`, `"/dav"`, `gin.WrapH`} {
		if !strings.Contains(body, m) {
			t.Fatalf("WebDAV 注册块缺 %s", m)
		}
	}
}
