package transcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"panoalbum/internal/ffmpeg"
)

// 硬件编码的**回退**验证。
//
// 为什么这条测试重要：硬编可用性取决于"驱动 + ffmpeg 构建"两件本进程控制不了的事，
// 而软件的红线是"双接口必须同时保留、无卡环境只降级不报错"。所以真正要证明的不是
// "nvenc 能编码"（那要有卡机器），而是 **"请求了 nvenc 但环境跑不了时，任务仍然成功"**。
//
// 这条测试在不同环境下验证不同分支，但断言是同一组（成功 + 产出完整）：
//   - 无 NVIDIA 卡 / 发行版 ffmpeg 未编入 nvenc（本机与测试服的常态）→ 走**回退**分支；
//   - 有可用 NVENC → 走硬编分支，回退不触发。
//
// 两种都是我们关心的行为，故不需要为环境分叉，也不需要 mock 掉 ffmpeg。
func TestTranscodeHLSHardwareEncoderFallsBackToSoftware(t *testing.T) {
	ffBin, err := ffmpeg.LookPath()
	if err != nil {
		t.Skipf("本机无 ffmpeg（可设 FFMPEG_PATH 指定），跳过: %v", err)
	}

	root := t.TempDir()
	src := filepath.Join(root, "clip.mp4")
	if out, err := exec.Command(ffBin, "-y", "-v", "error",
		"-f", "lavfi", "-i", "testsrc=size=320x240:rate=10",
		"-t", "1", "-pix_fmt", "yuv420p", src).CombinedOutput(); err != nil {
		t.Fatalf("生成测试素材失败: %v\n%s", err, out)
	}

	const mediaID = "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff"
	hlsDir := filepath.Join(root, "hls")

	url, err := TranscodeHLS(context.Background(), HLSTranscodeRequest{
		MediaID: mediaID,
		Input:   src,
		HLSDir:  hlsDir,
		Profile: "1080p",
		Encoder: ffmpeg.EncoderNVENC, // 显式请求硬编：环境给不了也必须成功
	})
	if err != nil {
		t.Fatalf("请求硬编但环境不可用时，必须回退软编并成功，实际报错: %v", err)
	}
	if want := "/transcode/hls/" + mediaID + "/master.m3u8"; url != want {
		t.Fatalf("result_path 应为 %q，实际 %q", want, url)
	}

	// 产出必须**完整** —— 这一条才是"回退真的跑完了"的证据：
	// 若实现只是吞掉了硬编的错误、留下硬编失败时的半成品目录，master 或分片就会缺失。
	master := filepath.Join(hlsDir, mediaID, "master.m3u8")
	body, err := os.ReadFile(master)
	if err != nil {
		t.Fatalf("master.m3u8 未产出（回退可能没真的跑）: %v", err)
	}
	if !strings.Contains(string(body), "#EXTM3U") {
		t.Fatalf("master.m3u8 不是合法播放列表: %q", string(body))
	}
	seg, _ := filepath.Glob(filepath.Join(hlsDir, mediaID, "*", "seg_*.ts"))
	if len(seg) == 0 {
		t.Fatal("未产出任何 .ts 分片（回退那一趟没有跑完）")
	}
	t.Logf("回退路径产出：master + %d 个分片", len(seg))
}

// TestTranscodeHLSUnknownEncoderStillSucceeds 未知编码器取值不能让任务失败。
//
// 与 ffmpeg 包的单测互补：那边断言参数回退，这边断言**端到端仍然转得出东西**。
func TestTranscodeHLSUnknownEncoderStillSucceeds(t *testing.T) {
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
	_, err = TranscodeHLS(context.Background(), HLSTranscodeRequest{
		MediaID: "cccccccc-dddd-4eee-8fff-000000000000",
		Input:   src,
		HLSDir:  filepath.Join(root, "hls"),
		Encoder: ffmpeg.VideoEncoder("完全不存在的编码器"),
	})
	if err != nil {
		t.Fatalf("未知编码器应回落软编并成功，实际报错: %v", err)
	}
}

// TestTranscodeHLSRejectsMissingInputs 前置条件缺失必须报错而不是产出空目录。
func TestTranscodeHLSRejectsMissingInputs(t *testing.T) {
	ctx := context.Background()
	if _, err := TranscodeHLS(ctx, HLSTranscodeRequest{Input: "x", HLSDir: "y"}); err == nil {
		t.Fatal("缺 media_id 应报错")
	}
	if _, err := TranscodeHLS(ctx, HLSTranscodeRequest{MediaID: "m", HLSDir: "y"}); err == nil {
		t.Fatal("缺 input 应报错")
	}
	if _, err := TranscodeHLS(ctx, HLSTranscodeRequest{MediaID: "m", Input: "x"}); err == nil {
		t.Fatal("缺 HLSDir 应报错")
	}
}
