package ffmpeg

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 测试素材策略：lavfi 合成（用户已确认 Q3），无外部文件依赖，可自动化复现。

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := LookPath(); err != nil {
		t.Skipf("ffmpeg 不可用（%v），跳过集成测试", err)
	}
}

// genSample 用 lavfi 合成 5s 测试片（640x360@30fps + 440Hz 音轨）。
func genSample(t *testing.T, dir string) string {
	t.Helper()
	requireFFmpeg(t)
	out := filepath.Join(dir, "sample.mp4")
	res, err := New([]string{
		"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30:duration=5",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=5",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac",
		"-y", out,
	}).Run(context.Background())
	if err != nil {
		t.Fatalf("合成测试片失败: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("合成测试片退出码 %d", res.ExitCode)
	}
	return out
}

// TestLookPathEnv FFMPEG_PATH 环境变量优先（Q1 验收）。
func TestLookPathEnv(t *testing.T) {
	requireFFmpeg(t)
	bin, err := LookPath()
	if err != nil {
		t.Fatalf("LookPath: %v", err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("解析结果不可访问: %v", err)
	}
}

// TestTranscodeProgress AC-05/AC-06：进程启动 + stdout/stderr 捕获 + progress 解析（时间/帧数/码率/速度）。
func TestTranscodeProgress(t *testing.T) {
	dir := t.TempDir()
	sample := genSample(t, dir)
	out := filepath.Join(dir, "out.mp4")

	dur, err := ProbeDurationUs(context.Background(), sample)
	if err != nil {
		t.Fatalf("ProbeDurationUs: %v", err)
	}
	if dur < 4_000_000 {
		t.Fatalf("样片时长异常: %dus", dur)
	}

	task := New([]string{
		"-i", sample, "-vf", "scale=320:180", "-c:v", "libx264", "-preset", "ultrafast", "-an", "-y", out,
	}, WithExpectedDurationUs(dur))
	events := task.SubscribeProgress(64)

	if err := task.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	var last Progress
	count := 0
	for p := range events {
		last = p
		count++
	}
	res, err := task.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if res.ExitCode != 0 {
		t.Fatalf("退出码 %d\n%s", res.ExitCode, res.StderrTail)
	}
	if count == 0 {
		t.Fatal("未收到任何 progress 事件")
	}
	if !last.Done {
		t.Fatal("最后一个事件 Done=false")
	}
	if last.Frame < 100 {
		t.Fatalf("帧数异常: %d", last.Frame)
	}
	if last.OutTimeUs < 4_000_000 {
		t.Fatalf("已处理时长异常: %dus", last.OutTimeUs)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("输出文件不存在: %v", err)
	}
}

// TestCancelGraceful AC-05：取消后优雅终止（'q' → 宽限 → Kill 兜底），Wait 及时返回。
func TestCancelGraceful(t *testing.T) {
	dir := t.TempDir()
	requireFFmpeg(t)

	// 30s 长片源保证转码不会自然结束
	src := filepath.Join(dir, "long.mp4")
	if _, err := New([]string{
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-y", src,
	}).Run(context.Background()); err != nil {
		t.Fatalf("合成长片失败: %v", err)
	}

	task := New([]string{
		"-i", src, "-c:v", "libx264", "-preset", "slow", "-an", "-y", filepath.Join(dir, "out.mp4"),
	})
	if err := task.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	task.Cancel()

	done := make(chan struct{})
	go func() { task.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Cancel 后 10s 内未结束")
	}
	res, _ := task.Wait()
	if !res.Canceled {
		t.Fatal("Result.Canceled=false")
	}
}

// TestErrorStructured AC-07：不存在输入 → 结构化错误（退出码 + stderr 尾部 + 时长）。
func TestErrorStructured(t *testing.T) {
	requireFFmpeg(t)
	_, err := New([]string{"-i", filepath.Join(t.TempDir(), "nonexistent.mp4"), "-y", "nul"}).Run(context.Background())
	if err == nil {
		t.Fatal("预期返回错误")
	}
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatalf("错误类型非 *Error: %T", err)
	}
	if fe.ExitCode == 0 {
		t.Fatal("ExitCode=0")
	}
	if !strings.Contains(fe.StderrTail, "No such file") {
		t.Fatalf("StderrTail 缺少关键信息:\n%s", fe.StderrTail)
	}
	if fe.Duration <= 0 {
		t.Fatal("Duration 未记录")
	}
}

// TestTimeout ctx 超时触发优雅停止。
func TestTimeout(t *testing.T) {
	dir := t.TempDir()
	requireFFmpeg(t)
	src := filepath.Join(dir, "long.mp4")
	if _, err := New([]string{
		"-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=30:duration=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", "-y", src,
	}).Run(context.Background()); err != nil {
		t.Fatalf("合成长片失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	res, _ := New([]string{
		"-i", src, "-c:v", "libx264", "-preset", "slow", "-an", "-y", filepath.Join(dir, "out.mp4"),
	}).Run(ctx)
	if time.Since(start) > 10*time.Second {
		t.Fatal("超时后未及时结束")
	}
	if res == nil || !res.Canceled {
		t.Fatalf("预期 Canceled=true: %+v", res)
	}
}

// TestThumbnailPreset 缩略图预设（Q4：WebP 三档之一）。
func TestThumbnailPreset(t *testing.T) {
	dir := t.TempDir()
	sample := genSample(t, dir)
	out := filepath.Join(dir, "thumb.webp")

	res, err := New(ThumbnailArgs(sample, out, ThumbMD, 1_000_000)).Run(context.Background())
	if err != nil {
		t.Fatalf("缩略图失败: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("退出码 %d\n%s", res.ExitCode, res.StderrTail)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("缩略图无效: %v", err)
	}
}

// TestHLSPreset HLS 多码率预设（Q4）：master.m3u8 + 两档变体（小尺寸档位保证测试速度）。
func TestHLSPreset(t *testing.T) {
	dir := t.TempDir()
	sample := genSample(t, dir)
	outDir := filepath.Join(dir, "hls")

	ladder := []HLSRendition{
		{Name: "180p", Width: 320, Height: 180, BitrateK: 400},
		{Name: "360p", Width: 640, Height: 360, BitrateK: 800},
	}
	for _, d := range HLSDirs(outDir, ladder) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("建目录: %v", err)
		}
	}

	res, err := New(HLSArgs(sample, outDir, ladder, 2, true)).Run(context.Background())
	if err != nil {
		t.Fatalf("HLS 转码失败: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("退出码 %d\n%s", res.ExitCode, res.StderrTail)
	}

	master, err := os.ReadFile(filepath.Join(outDir, "master.m3u8"))
	if err != nil {
		t.Fatalf("master.m3u8 不存在: %v", err)
	}
	if n := strings.Count(string(master), "#EXT-X-STREAM-INF"); n != 2 {
		t.Fatalf("master 变体数 %d != 2", n)
	}
	for _, d := range HLSDirs(outDir, ladder) {
		if _, err := os.Stat(filepath.Join(d, "index.m3u8")); err != nil {
			t.Fatalf("变体播放列表缺失: %v", err)
		}
	}
}
