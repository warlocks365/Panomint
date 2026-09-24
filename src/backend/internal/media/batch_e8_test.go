package media

// Job000114 / E8 复制同源自覆盖修复的回归网（纯逻辑级，不触库——
// 与 batch_target_test.go 同一方法论：E8 是「同一媒体复制两次 → 相同 path →
// os.Create 截断互覆 → 两行共用一个文件」的磁盘级缺陷，触发在运行期，
// 必须钉生成规则的契约）。

import (
	"strings"
	"testing"
)

// ---- copyStoredName 唯一性契约（E8 止血核心） ----

func TestCopyStoredNameUniqueAcrossCalls(t *testing.T) {
	const src = "0a1b2c3d4e5f6a7b8c9d0e1f_photo.jpg"
	seen := make(map[string]struct{}, 256)
	for i := 0; i < 256; i++ {
		n := copyStoredName(src)
		if _, dup := seen[n]; dup {
			t.Fatalf("第 %d 次生成重复存储名（E8 回归：同源复制将再度同 path）: %s", i, n)
		}
		seen[n] = struct{}{}
	}
}

func TestCopyStoredNameKeepsSourceSuffix(t *testing.T) {
	// 展示/下载语义依赖 filename 列保留源存储名后缀（源文件名部分不可丢）。
	for _, src := range []string{
		"0a1b_upload_me.jpg",
		"中文 文件名 (1).jpg",
		"noext",
		"a.b.c.PNG",
	} {
		n := copyStoredName(src)
		if !strings.HasSuffix(n, "_"+src) {
			t.Fatalf("copyStoredName(%q) = %q，缺少 _<源存储名> 后缀", src, n)
		}
	}
}

func TestCopyStoredNameMatchesUploadPrefixRule(t *testing.T) {
	// 与 upload.ingest 规则同构：24-hex 随机前缀 + "_" + 文件名（upload.go newUploadID）。
	// 钉住「前缀段不得回退为源 id」——E8 根因正是源 id 前缀在同源复制下恒定。
	n := copyStoredName("x.jpg")
	head, _, ok := strings.Cut(n, "_")
	if !ok || len(head) != 24 {
		t.Fatalf("前缀应为 24-hex 随机串（对齐 upload.newUploadID），实得 %q（全名 %q）", head, n)
	}
	for _, ch := range head {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			t.Fatalf("前缀含非 hex 字符 %q（全名 %q）", ch, n)
		}
	}
	// 决定性回归断言：同一源文件名两次调用，前缀段必不同（旧 row.id 实现此处必同）。
	a := copyStoredName("same-source.jpg")
	b := copyStoredName("same-source.jpg")
	ah, _, _ := strings.Cut(a, "_")
	bh, _, _ := strings.Cut(b, "_")
	if ah == bh {
		t.Fatalf("同源两次复制前缀相同（E8 未修）: %s vs %s", a, b)
	}
}
