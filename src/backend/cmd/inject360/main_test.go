package main

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/index"
)

// writeTestJPEG 生成最小合法 JPEG（纯色图），返回路径。
func writeTestJPEG(t *testing.T, dir string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 0x20, G: 0x60, B: 0xa0, A: 0xff})
		}
	}
	path := filepath.Join(dir, "plain.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestInjectPhoto 端到端闭环：生成无元数据 JPEG → 注入 → 用 internal/index 检测入口回读。
func TestInjectPhoto(t *testing.T) {
	dir := t.TempDir()
	src := writeTestJPEG(t, dir, 2048, 1024)
	srcBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	// 注入前应先判非 360，确认样本本身无全景元数据
	if m, err := index.ExtractPhotoMeta(src); err != nil || m.Is360 {
		t.Fatalf("前置条件失败：原图不应判为 360（Is360=%v, err=%v）", m != nil && m.Is360, err)
	}

	dst := filepath.Join(dir, "pano.jpg")
	if err := injectPhoto(src, dst); err != nil {
		t.Fatalf("injectPhoto: %v", err)
	}

	m, err := index.ExtractPhotoMeta(dst)
	if err != nil {
		t.Fatalf("ExtractPhotoMeta: %v", err)
	}
	if !m.Is360 {
		t.Fatal("注入后 Is360 应为 true")
	}
	if m.Projection != index.ProjectionEquirect {
		t.Fatalf("Projection = %q, 期望 %q", m.Projection, index.ProjectionEquirect)
	}
	if m.Width != 2048 || m.Height != 1024 {
		t.Fatalf("尺寸 = %dx%d, 期望 2048x1024", m.Width, m.Height)
	}

	// 原文件不得被改动
	after, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(srcBytes) {
		t.Fatal("原文件被修改")
	}
}

// TestInjectPhoto_DefaultOutput 缺省输出名为 <原名>.360.<扩展名>，且不覆盖已存在文件。
func TestInjectPhoto_DefaultOutput(t *testing.T) {
	dir := t.TempDir()
	src := writeTestJPEG(t, dir, 1024, 512)
	dst := defaultOutput(src)
	if want := filepath.Join(dir, "plain.360.jpg"); dst != want {
		t.Fatalf("defaultOutput = %q, 期望 %q", dst, want)
	}
	if err := injectPhoto(src, dst); err != nil {
		t.Fatalf("injectPhoto: %v", err)
	}
	if err := injectPhoto(src, dst); err == nil {
		t.Fatal("目标已存在时应拒绝覆盖")
	}
	if err := injectPhoto(src, src); err == nil {
		t.Fatal("输出与输入相同时应报错")
	}
}

// TestInjectVideo 端到端闭环（视频）：ffmpeg 生成测试 MP4 → 注入 → 检测入口回读。
// 需要本机 ffmpeg/ffprobe，用 -short 跳过。
func TestInjectVideo(t *testing.T) {
	if testing.Short() {
		t.Skip("short 模式跳过视频端到端测试")
	}
	bin, err := ffmpeg.LookPath()
	if err != nil {
		t.Skipf("无 ffmpeg: %v", err)
	}
	if os.Getenv("FFPROBE_PATH") == "" {
		if p := os.Getenv("FFMPEG_PATH"); p != "" {
			os.Setenv("FFPROBE_PATH", p)
		}
	}
	if _, err := ffmpeg.LookProbePath(); err != nil {
		t.Skipf("无 ffprobe: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src.mp4")
	out, err := exec.Command(bin,
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=1920x960:duration=2:rate=25",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", src).CombinedOutput()
	if err != nil {
		t.Fatalf("生成测试视频失败: %v\n%s", err, out)
	}

	ctx := context.Background()
	if m, err := index.ExtractVideoMeta(ctx, src); err != nil || m.Is360 {
		t.Fatalf("前置条件失败：原视频不应判为 360（Is360=%v, err=%v）", m != nil && m.Is360, err)
	}

	dst := filepath.Join(dir, "pano.mp4")
	if err := injectVideo(ctx, src, dst); err != nil {
		t.Fatalf("injectVideo: %v", err)
	}
	m, err := index.ExtractVideoMeta(ctx, dst)
	if err != nil {
		t.Fatalf("ExtractVideoMeta: %v", err)
	}
	if !m.Is360 || m.Projection != index.ProjectionEquirect {
		t.Fatalf("Is360=%v Projection=%q, 期望 true/%q", m.Is360, m.Projection, index.ProjectionEquirect)
	}
	if m.Width != 1920 || m.Height != 960 {
		t.Fatalf("尺寸 = %dx%d, 期望 1920x960", m.Width, m.Height)
	}
}
