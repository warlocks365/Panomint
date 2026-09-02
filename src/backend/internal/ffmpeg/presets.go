package ffmpeg

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ThumbSize 缩略图档位（TDD §3.3：SM/MD/LG 三档 WebP）。
type ThumbSize string

const (
	ThumbSM ThumbSize = "SM" // 宽 320
	ThumbMD ThumbSize = "MD" // 宽 640
	ThumbLG ThumbSize = "LG" // 宽 1280
)

var thumbWidth = map[ThumbSize]int{ThumbSM: 320, ThumbMD: 640, ThumbLG: 1280}

// ThumbnailArgs 生成单帧 WebP 缩略图参数。
// seekUs>0 时先定位（视频取预览帧，对应 DDL media.video_preview_at）。
func ThumbnailArgs(input, output string, size ThumbSize, seekUs int64) []string {
	var args []string
	if seekUs > 0 {
		args = append(args, "-ss", strconv.FormatFloat(float64(seekUs)/1e6, 'f', 3, 64))
	}
	return append(args,
		"-i", input,
		"-frames:v", "1",
		"-vf", fmt.Sprintf("scale=%d:-2", thumbWidth[size]),
		"-c:v", "libwebp", "-quality", "82",
		"-y", output,
	)
}

// HLSRendition 一个 ABR 码率档位。
type HLSRendition struct {
	Name     string // 档位名（子目录名），如 "1080p"
	Width    int
	Height   int
	BitrateK int // 视频码率 kbits/s
}

// DefaultHLSLadder 默认三档（T2.1 360 原型测试流 / T3.5 转码管线复用）。
// 注：360 equirect 源为 2:1 比例，scale 保持等比不裁剪；球面元数据策略在 T3.5 细化。
func DefaultHLSLadder() []HLSRendition {
	return []HLSRendition{
		{Name: "1080p", Width: 1920, Height: 1080, BitrateK: 5000},
		{Name: "1440p", Width: 2560, Height: 1440, BitrateK: 10000},
		{Name: "2160p", Width: 3840, Height: 2160, BitrateK: 20000},
	}
}

// fullHLSLadder 由高到低的完整档位表（2160p→360p）。
// 档位尺寸是 16:9 目标框：HLSArgs 用 scale=...:force_original_aspect_ratio=decrease 等比缩放，
// 故 2:1 全景源按「宽受限」命中高阶梯（3840x1920 源 → 3840x2160 框 → 输出 3840x1920，不上采样）。
var fullHLSLadder = []HLSRendition{
	{Name: "2160p", Width: 3840, Height: 2160, BitrateK: 20000},
	{Name: "1440p", Width: 2560, Height: 1440, BitrateK: 10000},
	{Name: "1080p", Width: 1920, Height: 1080, BitrateK: 5000},
	{Name: "720p", Width: 1280, Height: 720, BitrateK: 2800},
	{Name: "480p", Width: 854, Height: 480, BitrateK: 1200},
	{Name: "360p", Width: 640, Height: 360, BitrateK: 700},
}

// maxRenditions 单次转码最多输出的档位数（控制算力与存储开销）。
const maxRenditions = 4

// LadderForSource 按源分辨率裁剪码率阶梯，**绝不上采样**（720p 源不会去转 1080p/1440p/2160p）。
//
// 命中条件：档位宽 ≤ 源宽 **或** 档位高 ≤ 源高。这与 HLSArgs 里
// scale=...:force_original_aspect_ratio=decrease 的行为一致——缩放因子
// s = min(档宽/源宽, 档高/源高)，s ≤ 1 即无上采样；只要有一条边被约束住就成立。
// 因档位表降序，命中的是「从最高可用档往下」的连续若干档，最多 maxRenditions 档。
//
// 源比 360p 还小时退化为单档：按源尺寸输出（libx264 要求偶数边长）。
// srcWidth/srcHeight ≤ 0（分辨率未知）时返回 nil，由调用方决定兜底策略。
func LadderForSource(srcWidth, srcHeight int) []HLSRendition {
	if srcWidth <= 0 || srcHeight <= 0 {
		return nil
	}
	out := make([]HLSRendition, 0, maxRenditions)
	for _, r := range fullHLSLadder {
		if r.Width <= srcWidth || r.Height <= srcHeight {
			out = append(out, r)
			if len(out) == maxRenditions {
				break
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	w, h := srcWidth&^1, srcHeight&^1 // 偶数化
	if w < 2 {
		w = 2
	}
	if h < 2 {
		h = 2
	}
	return []HLSRendition{{Name: "src", Width: w, Height: h, BitrateK: 500}}
}

// HLSArgs 生成多码率 HLS 参数（软编 libx264；NVENC 在有 GPU 的算力节点启用，见 T6.2）。
// 输出结构：outDir/master.m3u8 + outDir/<索引>/index.m3u8 + seg_*.ts（%v 展开为档位索引 0..n-1）。
// 注意：调用方需预创建各档位索引子目录（ffmpeg 不自动建目录），可用 HLSDirs 获取列表。
// withAudio 为 true 时按变体逐份映射首个音频流（要求输入含音轨，可先 ProbeDurationUs/探针确认）。
func HLSArgs(input, outDir string, ladder []HLSRendition, segSeconds int, withAudio bool) []string {
	n := len(ladder)

	var fc strings.Builder
	fmt.Fprintf(&fc, "[0:v]split=%d", n)
	for i := range ladder {
		fmt.Fprintf(&fc, "[v%d]", i)
	}
	for i, r := range ladder {
		// force_divisible_by=2：等比缩放后的取整结果可能是奇数（如 1280x720 缩到
		// 480p 得到 853x480），libx264 要求宽高均为偶数，否则报
		// "width not divisible by 2" 直接转码失败。此选项强制向下取偶。
		fmt.Fprintf(&fc, ";[v%d]scale=w=%d:h=%d:force_original_aspect_ratio=decrease:force_divisible_by=2[v%dout]", i, r.Width, r.Height, i)
	}

	args := []string{"-i", input, "-filter_complex", fc.String()}
	varStream := make([]string, 0, n)
	for i, r := range ladder {
		args = append(args,
			"-map", fmt.Sprintf("[v%dout]", i),
			fmt.Sprintf("-c:v:%d", i), "libx264",
			fmt.Sprintf("-b:v:%d", i), fmt.Sprintf("%dk", r.BitrateK),
			fmt.Sprintf("-maxrate:v:%d", i), fmt.Sprintf("%dk", r.BitrateK*12/10),
			fmt.Sprintf("-bufsize:v:%d", i), fmt.Sprintf("%dk", r.BitrateK*2),
			fmt.Sprintf("-preset:v:%d", i), "veryfast",
		)
		entry := fmt.Sprintf("v:%d", i)
		if withAudio {
			entry = fmt.Sprintf("v:%d,a:%d", i, i)
		}
		varStream = append(varStream, entry)
	}
	if withAudio {
		// HLS muxer 要求同一基本流不得出现在多个变体中 → 音频按变体逐份映射（a:0..a:n-1）
		for range ladder {
			args = append(args, "-map", "0:a?")
		}
		args = append(args, "-c:a", "aac", "-b:a", "128k")
	}

	return append(args,
		"-f", "hls",
		"-hls_time", strconv.Itoa(segSeconds),
		"-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(outDir, "%v", "seg_%03d.ts"),
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", strings.Join(varStream, " "),
		filepath.Join(outDir, "%v", "index.m3u8"),
	)
}

// HLSDirs 返回调用方需预创建的档位索引子目录（"0","1",...，对应 %v 展开）。
func HLSDirs(outDir string, ladder []HLSRendition) []string {
	dirs := make([]string, 0, len(ladder))
	for i := range ladder {
		dirs = append(dirs, filepath.Join(outDir, strconv.Itoa(i)))
	}
	return dirs
}
