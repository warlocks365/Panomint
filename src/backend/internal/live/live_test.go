package live

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 假 binary 是 sh 脚本：Windows 本机不可执行，守卫在 Linux（CI/测试服）执行。
func skipWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("live 会话守卫含 sh 假 binary，须在 Linux 运行")
	}
}

// 守卫纪律：硬编码期望值；ffmpeg 用假 binary（写清单后常驻），不依赖真实转码。

// fakeFFmpeg 生成一个假 ffmpeg：写空清单 + 空分片后 sleep 常驻，响应 SIGTERM 退出。
func fakeFFmpeg(t *testing.T, dir string) string {
	t.Helper()
	bin := filepath.Join(dir, "fake-ffmpeg.sh")
	script := `#!/bin/sh
# 末参是输出 m3u8 路径
out=""
for a in "$@"; do out="$a"; done
mkdir -p "$(dirname "$out")"
echo "#EXTM3U" > "$out"
echo "#EXT-X-ENDLIST" >> "$out"
touch "$(dirname "$out")/seg_00000.ts"
sleep 600 &
wait
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func testFile(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func newTestManager(t *testing.T, maxConc int) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	return NewManager(Config{FFmpegBin: fakeFFmpeg(t, root), MaxConcurrent: maxConc, RootDir: filepath.Join(root, "live")}), root
}

func TestEnsure_ReuseSamePos(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 1)
	defer m.Stop()
	src := testFile(t, root, "src.mp4")

	s1, err := m.Ensure("m1", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := m.Ensure("m1", src, 0.5) // 容差 1s 内复用
	if err != nil {
		t.Fatal(err)
	}
	if s1 != s2 {
		t.Fatal("同起点应复用同一会话")
	}
}

func TestEnsure_SeekRebuilds(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 1)
	defer m.Stop()
	src := testFile(t, root, "src.mp4")

	s1, err := m.Ensure("m1", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := m.Ensure("m1", src, 30)
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s2 {
		t.Fatal("seek 必须重建会话")
	}
	if s2.StartPos != 30 {
		t.Fatalf("新会话起点应为 30，得 %v", s2.StartPos)
	}
}

func TestEnsure_ConcurrencyCap(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 1)
	defer m.Stop()
	src := testFile(t, root, "src.mp4")

	if _, err := m.Ensure("m1", src, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Ensure("m2", src, 0); err != ErrBusy {
		t.Fatalf("并发上限 1 时第二路必须 ErrBusy，得 %v", err)
	}
}

func TestEnsure_SourceGone(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 1)
	defer m.Stop()
	_, err := m.Ensure("m1", filepath.Join(root, "nope.mp4"), 0)
	if err != ErrSourceGone {
		t.Fatalf("源缺失必须 ErrSourceGone，得 %v", err)
	}
}

func TestSegmentPath_Validation(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 1)
	defer m.Stop()
	src := testFile(t, root, "src.mp4")
	s, err := m.Ensure("m1", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.SegmentPath(s, "../escape.ts"); ok {
		t.Fatal("路径穿越必须拒绝")
	}
	if _, ok := m.SegmentPath(s, "stream.m3u8"); ok {
		t.Fatal("非分片名必须拒绝")
	}
	// 合法名但文件不存在 → false
	if _, ok := m.SegmentPath(s, "seg_00099.ts"); ok {
		t.Fatal("不存在的分片必须 false")
	}
}

func TestReapIdle_EndedSession(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 2)
	defer m.Stop()
	src := testFile(t, root, "src.mp4")

	s, err := m.Ensure("m1", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	s.close() // 模拟结束（假 ffmpeg 被 kill → done 关闭 → ended）
	deadline := time.Now().Add(3 * time.Second)
	for !s.isEnded() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	m.ReapIdle()
	m.mu.Lock()
	_, ok := m.sessions["m1"]
	m.mu.Unlock()
	if ok {
		t.Fatal("已结束会话必须被回收")
	}
}

func TestManager_StopCleansAll(t *testing.T) {
	skipWindows(t)
	m, root := newTestManager(t, 2)
	src := testFile(t, root, "src.mp4")
	if _, err := m.Ensure("m1", src, 0); err != nil {
		t.Fatal(err)
	}
	m.Stop()
	if _, err := os.Stat(filepath.Join(root, "live")); !os.IsNotExist(err) {
		t.Fatal("Stop 后会话根目录必须清除")
	}
}
