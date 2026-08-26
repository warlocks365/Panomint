package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"panoalbum/internal/index"
)

// mkTree 构造临时目录树并扫描，返回扫描根与条目。
func mkTree(t *testing.T, files map[string]string) (string, []index.FileEntry) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := index.ScanDir(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	return root, entries
}

// TestStateResume 断点续扫：标记一半已导入 → 落盘 → 重新加载 → Pending 只剩另一半。
func TestStateResume(t *testing.T) {
	root, entries := mkTree(t, map[string]string{
		"2024/a.jpg": "aaa",
		"2024/b.jpg": "bbb",
		"2024/c.jpg": "ccc",
		"2024/d.mp4": "ddd",
	})
	if len(entries) != 4 {
		t.Fatalf("应扫到 4 个文件，实际 %d", len(entries))
	}
	statePath := filepath.Join(t.TempDir(), "state.json")

	// 第一轮：导入前两个后“中断”
	s1, err := LoadState(statePath, root)
	if err != nil {
		t.Fatal(err)
	}
	s1.Mark(entries[0])
	s1.Mark(entries[1])
	if err := s1.Save(); err != nil {
		t.Fatal(err)
	}

	// 第二轮：重新加载，只应剩 entries[2:]
	s2, err := LoadState(statePath, root)
	if err != nil {
		t.Fatal(err)
	}
	pending := s2.Pending(entries)
	if len(pending) != 2 {
		t.Fatalf("断点续扫应剩 2 个待导入，实际 %d: %+v", len(pending), pending)
	}
	for _, e := range pending {
		if e.Rel == entries[0].Rel || e.Rel == entries[1].Rel {
			t.Errorf("已完成条目 %s 不应再次出现", e.Rel)
		}
	}
}

// TestStateDeduplication 去重：size+mtime 指纹未变的文件被跳过；内容变更后重新待导入。
func TestStateDeduplication(t *testing.T) {
	root, entries := mkTree(t, map[string]string{"a.jpg": "content-v1"})
	statePath := filepath.Join(t.TempDir(), "state.json")

	s, err := LoadState(statePath, root)
	if err != nil {
		t.Fatal(err)
	}
	s.Mark(entries[0])
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	// 指纹未变 → 去重跳过
	s2, err := LoadState(statePath, root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Pending(entries); len(got) != 0 {
		t.Errorf("指纹未变应去重跳过，实际待导入 %d", len(got))
	}

	// 覆写内容（size 变化）→ 重新待导入
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(root, "a.jpg"), []byte("content-v2-longer"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries2, err := index.ScanDir(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Pending(entries2); len(got) != 1 {
		t.Errorf("内容变更后应重新待导入，实际 %d", len(got))
	}
}

// TestStateRootMismatch 根目录变更时状态重置，避免串库。
func TestStateRootMismatch(t *testing.T) {
	root1, entries := mkTree(t, map[string]string{"a.jpg": "x"})
	statePath := filepath.Join(t.TempDir(), "state.json")
	s, err := LoadState(statePath, root1)
	if err != nil {
		t.Fatal(err)
	}
	s.Mark(entries[0])
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	root2 := t.TempDir() // 不同根
	s2, err := LoadState(statePath, root2)
	if err != nil {
		t.Fatal(err)
	}
	if s2.Done() != 0 {
		t.Errorf("根目录变更应重置状态，实际已完成 %d", s2.Done())
	}
}
