package transcode

// HLS 转码管线（**无 DB 依赖**的部分）。
//
// 为什么要把这条管线从 worker.go 里抽出来：它现在有两个调用方 ——
//   - 控制端的 cmd/transcodectl（走 Valkey 队列，自己查库、自己回写状态与 media.hls_master）；
//   - 算力节点的 agent（走 T6.2 的 poll/result 协议，媒体元数据由控制端随任务下发，
//     状态与 hls_master 由控制端在收到回传时回写）。
//
// 若两边各写一份 ffmpeg 参数，迟早会漂移（`force_divisible_by=2` 这类细节一旦漏掉，
// 表现是"某些源转码必失败、另一些却正常"，极难定位）。故：**ffmpeg 怎么调只有这一份**，
// 调用方只负责"谁来查库、谁来记状态"。

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"panoalbum/internal/ffmpeg"
)

// DefaultProfile 未指定档位时的默认档位名（与 worker.go 的历史行为一致）。
const DefaultProfile = "1080p"

// DefaultSegSeconds HLS 分片时长（秒）。4s 是 T2.1/T3.5 定下的取值，
// 兼顾首屏延迟与请求数量。
const DefaultSegSeconds = 4

// ResolveMediaPath 把 media.path（库内保存的相对路径）解析为磁盘上的可读文件路径。
//
// roots 是候选根目录，**按序探测**（调用方的顺序通常是 UPLOAD_DIR 再 MEDIA_ROOT：
// 上传进来的媒体优先，其次才是既有索引根）。rel 为绝对路径时直接 stat，不拼 roots。
//
// 单独导出是为了让算力节点复用同一套解析规则：节点上的挂载点只要和 roots 一致，
// 同一条 media.path 就在两边解析到同一个文件 —— 这是"存储与算力分离"能成立的前提。
func ResolveMediaPath(rel string, roots ...string) (string, error) {
	if filepath.IsAbs(rel) {
		if _, err := os.Stat(rel); err == nil {
			return rel, nil
		}
		return "", fmt.Errorf("源文件不可达: %s", rel)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("源文件不可达: %s", rel)
}

// HLSTranscodeRequest 一次 HLS 转码的输入。
type HLSTranscodeRequest struct {
	// MediaID 媒体 UUID 文本，决定输出子目录名。
	MediaID string
	// Input 已解析好的输入文件绝对路径（调用方用 ResolveMediaPath 得到）。
	Input string
	// HLSDir HLS 输出根目录；实际输出到 <HLSDir>/<MediaID>/。
	HLSDir string
	// Profile 档位名（1080p|2k|4k…）；空串取 DefaultProfile。
	Profile string
	// SrcWidth / SrcHeight 源分辨率。任一 <= 0 时由 ffprobe 现场探测；
	// 仍探测不到则退回"按档位名取阶梯"。
	SrcWidth  int
	SrcHeight int
	// SegSeconds 分片时长；<= 0 取 DefaultSegSeconds。
	SegSeconds int
}

// TranscodeHLS 执行多码率 HLS 转码，成功后返回 master.m3u8 的 **URL 路径**
// （形如 /transcode/hls/<mediaID>/master.m3u8，契约 §13 的形态，与 result_path 同构）。
//
// ⚠️ 本函数**不碰数据库**：任务状态与 media.hls_master 由调用方回写。
// 这样节点侧只需具备文件系统与 ffmpeg，不必持有数据库凭据 —— 与
// internal/compute 的"节点只拿 agent_token、不进用户/角色体系"是同一个边界选择。
//
// 返回的 error 一律是**可重试语义**（转码失败/IO 失败）；调用方自行决定是死信还是退避重试。
func TranscodeHLS(ctx context.Context, req HLSTranscodeRequest) (string, error) {
	if req.MediaID == "" {
		return "", fmt.Errorf("缺少 media_id")
	}
	if req.HLSDir == "" {
		return "", fmt.Errorf("缺少 HLS 输出根目录")
	}
	if req.Input == "" {
		return "", fmt.Errorf("缺少输入路径")
	}
	profile := req.Profile
	if profile == "" {
		profile = DefaultProfile
	}
	seg := req.SegSeconds
	if seg <= 0 {
		seg = DefaultSegSeconds
	}

	srcW, srcH := req.SrcWidth, req.SrcHeight
	if srcW <= 0 || srcH <= 0 {
		if pw, ph, err := ffmpeg.ProbeSize(ctx, req.Input); err == nil {
			srcW, srcH = pw, ph
		} else {
			log.Printf("无法确认源分辨率 media=%s: %v，回退按档位名取阶梯", req.MediaID, err)
		}
	}

	// 源分辨率决定码率阶梯（绝不上采样）；分辨率未知时按档位名兜底。
	ladder := LadderForSourceProfile(profile, srcW, srcH)
	if len(ladder) == 0 {
		ladder = LadderForProfile(profile)
	}
	if len(ladder) == 0 {
		return "", fmt.Errorf("无法确定码率阶梯（profile=%q 不是已知档位）", profile)
	}

	outDir := filepath.Join(req.HLSDir, req.MediaID)
	log.Printf("转码 media=%s profile=%s src=%dx%d 档位=%s", req.MediaID, profile, srcW, srcH, ladderNames(ladder))

	// 重试/重派场景：先清掉半成品目录（幂等重转）。
	// 这一步必须在新任务开始前完成，否则上一次跑了一半的 seg_*.ts 会与新产出混在一起，
	// 表现为"播放到中途花屏或时长异常"。
	_ = os.RemoveAll(outDir)
	for _, d := range append([]string{outDir}, ffmpeg.HLSDirs(outDir, ladder)...) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}

	durUs, _ := ffmpeg.ProbeDurationUs(ctx, req.Input)
	args := append([]string{"-y"},
		ffmpeg.HLSArgs(req.Input, outDir, ladder, seg, hasAudioStream(ctx, req.Input))...)
	task := ffmpeg.New(args, ffmpeg.WithExpectedDurationUs(durUs))
	if _, err := task.Run(ctx); err != nil {
		return "", fmt.Errorf("HLS 转码: %w", err)
	}
	// ffmpeg 退出码为 0 也不代表产出完整（例如磁盘写满时可能先报错后仍以 0 退出），
	// 故显式校验 master 播放列表确实存在。
	if _, err := os.Stat(filepath.Join(outDir, "master.m3u8")); err != nil {
		return "", fmt.Errorf("转码完成但 master.m3u8 缺失: %w", err)
	}
	return "/transcode/hls/" + req.MediaID + "/master.m3u8", nil
}
