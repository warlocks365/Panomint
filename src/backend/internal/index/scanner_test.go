package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyExt(t *testing.T) {
	cases := []struct {
		name string
		kind Kind
		ok   bool
	}{
		{"a.jpg", KindPhoto, true},
		{"b.JPEG", KindPhoto, true}, // 大小写不敏感
		{"c.png", KindPhoto, true},
		{"d.webp", KindPhoto, true},
		{"e.mp4", KindVideo, true},
		{"f.MOV", KindVideo, true},
		{"g.txt", "", false},
		{"h.gif", "", false}, // 本期不支持
		{"noext", "", false},
		{".hidden", "", false},
	}
	for _, c := range cases {
		kind, ok := ClassifyExt(c.name)
		if kind != c.kind || ok != c.ok {
			t.Errorf("ClassifyExt(%q) = (%q,%v)，期望 (%q,%v)", c.name, kind, ok, c.kind, c.ok)
		}
	}
}

func TestHashFileDeterministic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(p, []byte("pano-album-hash-test"), 0o644); err != nil {
		t.Fatal(err)
	}
	h1, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := HashFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Errorf("同文件两次哈希不同: %s vs %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("sha256 hex 应为 64 字符，实际 %d", len(h1))
	}
	// 内容不同则哈希不同（去重的判定基础）
	p2 := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(p2, []byte("pano-album-hash-test!"), 0o644); err != nil {
		t.Fatal(err)
	}
	h3, err := HashFile(p2)
	if err != nil {
		t.Fatal(err)
	}
	if h3 == h1 {
		t.Error("不同内容哈希相同，去重会误判")
	}
}

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("2024/a.jpg")
	mk("2024/b.mp4")
	mk("c.png")
	mk("skip.txt") // 不支持类型应被过滤
	mk(".git/config")

	entries, err := ScanDir(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("应识别 3 个媒体文件，实际 %d: %+v", len(entries), entries)
	}
	// 排序稳定 + 目录/类型字段正确
	if entries[0].Rel != "2024/a.jpg" || entries[0].Folder != "2024" || entries[0].Kind != KindPhoto {
		t.Errorf("条目0 字段错误: %+v", entries[0])
	}
	if entries[1].Kind != KindVideo {
		t.Errorf("条目1 应为视频: %+v", entries[1])
	}
	if entries[2].Rel != "c.png" || entries[2].Folder != "" {
		t.Errorf("根目录文件 Folder 应为空: %+v", entries[2])
	}
	for _, e := range entries {
		// Job000132 契约变更：遍历阶段不再算哈希（GB 级视频曾把清点拖成小时级 0/0 假死），
		// 哈希由 indexOne 在逐文件处理时计算。此处只验 size 与 hash 为空（延迟语义）。
		if e.Hash != "" || e.Size == 0 {
			t.Errorf("条目 hash 应为空（延迟到 indexOne）/size 必填: %+v", e)
		}
	}
}
