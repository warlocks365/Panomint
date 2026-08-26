package watch

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"panoalbum/internal/index"
)

// TestEntriesFromPaths 变更路径 → 索引条目：过滤目录/不支持类型/根外路径，rel 正确。
func TestEntriesFromPaths(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, content string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	img := mk("2024/p.jpg", "photo")
	vid := mk("v.insv", "insta360-video") // Insta360 原生格式
	txt := mk("notes.txt", "skip")

	entries := EntriesFromPaths(root, []string{
		img, vid, txt,
		root,                             // 目录跳过
		filepath.Join(root, "ghost.jpg"), // Rename 旧路径（已不存在）跳过
		filepath.Join(root, "..", "out.jpg"), // 根外路径跳过
	})
	if len(entries) != 2 {
		t.Fatalf("应得 2 个条目，实际 %d: %+v", len(entries), entries)
	}
	if entries[0].Rel != "2024/p.jpg" || entries[0].Kind != index.KindPhoto || entries[0].Folder != "2024" {
		t.Errorf("照片条目字段错误: %+v", entries[0])
	}
	if entries[1].Rel != "v.insv" || entries[1].Kind != index.KindVideo {
		t.Errorf("insv 应识别为视频: %+v", entries[1])
	}
	for _, e := range entries {
		if len(e.Hash) != 64 {
			t.Errorf("缺内容哈希: %+v", e)
		}
	}
}

// TestWatcherDetectsNewFile fsnotify 集成：新建文件经过去抖后进入批次回调。
func TestWatcherDetectsNewFile(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	w, err := NewWatcher(root, 60*time.Millisecond, func(paths []string) {
		mu.Lock()
		got = append(got, paths...)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("创建监听器失败: %v", err)
	}
	defer w.Close()
	go w.Run(ctx) //nolint:errcheck // 测试 ctx 取消即退出

	target := filepath.Join(root, "new.jpg")
	if err := os.WriteFile(target, []byte("watched"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) == 0 {
		t.Fatal("5s 内未收到新文件事件")
	}
	found := false
	for _, p := range got {
		if p == target {
			found = true
		}
	}
	if !found {
		t.Errorf("批次中应包含新文件 %s，实际 %v", target, got)
	}
}

// TestScanDirSkipsEaDir 全量扫描跳过群晖 @eaDir sidecar 目录（防缩略图副本入库）。
func TestScanDirSkipsEaDir(t *testing.T) {
	root, entries := mkTree(t, map[string]string{
		"real.jpg":               "real",
		"@eaDir/thumb.jpg":       "synology-thumb",
		"@eaDir/sub/SYNOINFO":    "meta",
		"2024/@eaDir/inner.jpg":  "nested-eadir",
		"2024/keep.jpg":          "keep",
	})
	if len(entries) != 2 {
		t.Fatalf("应跳过 @eaDir 只留 2 个文件，实际 %d: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Rel != "real.jpg" && e.Rel != "2024/keep.jpg" {
			t.Errorf("不应导入 @eaDir 内容: %s", e.Rel)
		}
	}
	_ = root
}
