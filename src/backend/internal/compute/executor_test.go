package compute

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"panoalbum/internal/ffmpeg"
)

// 本文件覆盖本地执行器（LocalExecutor）的分派与真实 ffmpeg 路径。
//
// 设计取舍：分派/错误路径是**免依赖**的普通单测（任何机器都能跑）；
// 真实转码则显式依赖 ffmpeg 可执行文件，探测不到就 Skip —— 而不是让它变红。
// 若把它写成硬依赖，"本机没装 ffmpeg"会被误读成"代码坏了"，久而久之就没人信这个测试了。

// TestLocalExecutorDispatch 按 kind 分派：noop 成功、未知 kind **必须失败**。
//
// 未知 kind 返回失败是刻意的回归点：早先的实现对所有任务一律返回 done，
// 那会让一个还没接入的任务类型看起来"执行成功"，控制端据此把它标成 done —— 比失败危险得多。
func TestLocalExecutorDispatch(t *testing.T) {
	ex := NewLocalExecutor(DeviceCPU)

	t.Run("noop → done", func(t *testing.T) {
		r := ex.Execute(context.Background(), JobSpec{JobID: "j1", Kind: KindNoop})
		if r.Status != JobStatusDone || r.JobID != "j1" {
			t.Fatalf("noop 应为 done 且保留 job_id，实际 %+v", r)
		}
	})

	t.Run("未知 kind → failed（不得假装成功）", func(t *testing.T) {
		for _, kind := range []string{"thumbnail", "memories", "who-knows", ""} {
			r := ex.Execute(context.Background(), JobSpec{JobID: "j2", Kind: kind})
			if r.Status != JobStatusFailed {
				t.Fatalf("kind=%q 应显式失败，实际 %+v", kind, r)
			}
			if r.Error == "" {
				t.Fatalf("kind=%q 失败时必须带原因", kind)
			}
		}
	})

	t.Run("ctx 已取消 → failed（不报假成功）", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for _, kind := range []string{KindNoop, KindHLS} {
			r := ex.Execute(ctx, JobSpec{JobID: "j3", Kind: kind})
			if r.Status != JobStatusFailed || r.Error == "" {
				t.Fatalf("kind=%q 取消时应 failed 且带原因，实际 %+v", kind, r)
			}
		}
	})
}

// TestLocalExecutorHLSConfigErrors hls 任务的前置条件缺失时必须**说得清楚**。
//
// 节点是无人值守的：控制端日志里的这一行往往是唯一线索。所以每个用例都断言
// 错误文本命中了"该去哪里配"的关键词，而不只是断言 Status=failed。
func TestLocalExecutorHLSConfigErrors(t *testing.T) {
	base := JobSpec{JobID: "j", Kind: KindHLS, MediaID: "m-1", InputPath: "a.mp4"}

	t.Run("未配 hls-dir", func(t *testing.T) {
		ex := NewLocalExecutor(DeviceCPU)
		ex.MediaRoots = []string{t.TempDir()}
		r := ex.Execute(context.Background(), base)
		if r.Status != JobStatusFailed || !strings.Contains(r.Error, "hls-dir") {
			t.Fatalf("应提示配置 -hls-dir，实际 %+v", r)
		}
	})

	t.Run("缺 media_id", func(t *testing.T) {
		ex := NewLocalExecutor(DeviceCPU)
		ex.HLSDir = t.TempDir()
		j := base
		j.MediaID = ""
		r := ex.Execute(context.Background(), j)
		if r.Status != JobStatusFailed || !strings.Contains(r.Error, "media_id") {
			t.Fatalf("应提示缺 media_id，实际 %+v", r)
		}
	})

	t.Run("缺 input_path", func(t *testing.T) {
		ex := NewLocalExecutor(DeviceCPU)
		ex.HLSDir = t.TempDir()
		j := base
		j.InputPath = ""
		r := ex.Execute(context.Background(), j)
		if r.Status != JobStatusFailed || !strings.Contains(r.Error, "input_path") {
			t.Fatalf("应提示缺 input_path，实际 %+v", r)
		}
	})

	t.Run("源文件不可达 → 错误里带上候选根（便于发现挂载点配错）", func(t *testing.T) {
		root := t.TempDir()
		ex := NewLocalExecutor(DeviceCPU)
		ex.MediaRoots = []string{root}
		ex.HLSDir = t.TempDir()

		r := ex.Execute(context.Background(), base)
		if r.Status != JobStatusFailed {
			t.Fatalf("源文件不存在应失败，实际 %+v", r)
		}
		if !strings.Contains(r.Error, root) {
			t.Fatalf("错误信息应带上候选媒体根 %q 以便排障，实际 %q", root, r.Error)
		}
	})
}

// TestLocalExecutorRealHLS 真实 ffmpeg 端到端：生成一段测试视频 → 转 HLS → 校验产出。
//
// 这是"执行器真的接了 ffmpeg"的证据，而不是"代码看起来接了"。
// 源用 320x240、1 秒：LadderForSourceProfile 对小于 360p 的源会退化成单档（按源尺寸），
// 于是这次转码只产出 1 个变体，跑得很快，同时仍完整走通
// 「探测分辨率 → 选阶梯 → 建目录 → 调 ffmpeg → 校验 master.m3u8」全链路。
//
// 无 ffmpeg 时 Skip（见文件头说明）。
func TestLocalExecutorRealHLS(t *testing.T) {
	ffBin, err := ffmpeg.LookPath()
	if err != nil {
		t.Skipf("本机无 ffmpeg（可设 FFMPEG_PATH 指定），跳过真实转码验证: %v", err)
	}

	root := t.TempDir()
	src := filepath.Join(root, "clip.mp4")
	gen := exec.Command(ffBin,
		"-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10",
		"-t", "1", "-pix_fmt", "yuv420p", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("生成测试素材失败: %v\n%s", err, out)
	}

	hlsDir := filepath.Join(root, "hls")
	const mediaID = "11111111-2222-4333-8444-555555555555"

	ex := NewLocalExecutor(DeviceCPU)
	ex.MediaRoots = []string{root}
	ex.HLSDir = hlsDir

	r := ex.Execute(context.Background(), JobSpec{
		JobID: "job-hls-1", Kind: KindHLS, MediaID: mediaID,
		InputPath: "clip.mp4", Profile: "1080p",
	})
	if r.Status != JobStatusDone {
		t.Fatalf("真实转码应成功，实际 status=%s error=%s", r.Status, r.Error)
	}
	want := "/transcode/hls/" + mediaID + "/master.m3u8"
	if r.ResultPath != want {
		t.Fatalf("result_path 应为 %q（契约 §13 的 URL 形态），实际 %q", want, r.ResultPath)
	}

	// 产出必须真的在磁盘上：master 播放列表 + 至少一个变体播放列表 + 分片。
	master := filepath.Join(hlsDir, mediaID, "master.m3u8")
	if _, err := os.Stat(master); err != nil {
		t.Fatalf("master.m3u8 未产出: %v", err)
	}
	body, err := os.ReadFile(master)
	if err != nil {
		t.Fatalf("读取 master.m3u8 失败: %v", err)
	}
	if !strings.Contains(string(body), "#EXTM3U") {
		t.Fatalf("master.m3u8 内容不是合法播放列表: %q", string(body))
	}

	seg, err := filepath.Glob(filepath.Join(hlsDir, mediaID, "*", "seg_*.ts"))
	if err != nil {
		t.Fatalf("glob 分片失败: %v", err)
	}
	if len(seg) == 0 {
		t.Fatalf("未产出任何 .ts 分片（转码没真正跑完）")
	}
	variant, err := filepath.Glob(filepath.Join(hlsDir, mediaID, "*", "index.m3u8"))
	if err != nil || len(variant) == 0 {
		t.Fatalf("未产出变体播放列表 index.m3u8: err=%v n=%d", err, len(variant))
	}
	fmt.Printf("真实转码产出：master + %d 个变体 + %d 个分片\n", len(variant), len(seg))
}

// TestLocalExecutorRealHLSIsIdempotent 重复执行同一任务必须得到一致的产出。
//
// 这条对应"回收后重派"的真实场景：同一个 media 可能被转两次（原节点僵死、任务被重派）。
// 若第二次转码没有先清掉上一次的半成品目录，新旧 seg_*.ts 会混在一起，
// 表现为"播放到中途花屏或时长异常"——极难定位，所以在这里钉死。
func TestLocalExecutorRealHLSIsIdempotent(t *testing.T) {
	ffBin, err := ffmpeg.LookPath()
	if err != nil {
		t.Skipf("本机无 ffmpeg，跳过: %v", err)
	}

	root := t.TempDir()
	src := filepath.Join(root, "clip.mp4")
	if out, err := exec.Command(ffBin, "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10",
		"-t", "1", "-pix_fmt", "yuv420p", src).CombinedOutput(); err != nil {
		t.Fatalf("生成测试素材失败: %v\n%s", err, out)
	}

	hlsDir := filepath.Join(root, "hls")
	const mediaID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	ex := NewLocalExecutor(DeviceCPU)
	ex.MediaRoots = []string{root}
	ex.HLSDir = hlsDir

	job := JobSpec{JobID: "j", Kind: KindHLS, MediaID: mediaID, InputPath: "clip.mp4", Profile: "1080p"}

	// 第一次：模拟"转到一半留下垃圾"——手工塞一个不属于本次产出的陈旧分片。
	staleDir := filepath.Join(hlsDir, mediaID, "0")
	if err := os.MkdirAll(staleDir, 0o755); err != nil {
		t.Fatalf("预置陈旧目录失败: %v", err)
	}
	stale := filepath.Join(staleDir, "seg_999.ts")
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("预置陈旧分片失败: %v", err)
	}

	r := ex.Execute(context.Background(), job)
	if r.Status != JobStatusDone {
		t.Fatalf("转码应成功，实际 %+v", r)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("陈旧分片 %s 应被清掉（否则新旧分片混在一起会花屏）", stale)
	}
}
