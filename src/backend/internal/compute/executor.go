package compute

// 任务执行器（节点侧）。
//
// 分工：
//   - NoopExecutor   空执行器。只用于验证"拉取 → 执行 → 回传"协议闭环，不碰任何 IO。
//   - LocalExecutor  真实执行器：按任务 kind 分派，`hls` 走 ffmpeg 转码。
//
// ⚠️ ffmpeg 的**调用方式**不在本文件：唯一实现在 internal/transcode 的 TranscodeHLS
// （无 DB 依赖），控制端的 cmd/transcodectl 与这里的节点 agent 共用同一份。
// 若在此处另写一份 ffmpeg 参数，两处迟早漂移（`force_divisible_by=2` 这类细节一旦漏掉，
// 症状是"某些源必失败、另一些却正常"，极难定位）。
//
// # 关于输入路径（存储与算力分离的关键约定）
//
// 控制端下发的 `InputPath` 是 **media.path 的库内相对路径**，不是节点上的绝对路径。
// 节点用自己配置的媒体根（-media-root，可多个、按序探测）把它解析成磁盘文件。
// 也就是说：**节点必须能看到源文件**（同机、或把共享存储挂到与解析根一致的路径）。
// 本实现刻意不发明"控制端上传/节点回传"的传输协议 —— 那会引入一套新的通道、
// 鉴权与配额语义，而项目红线要求软件以本地 GPU/CPU 为主，本机/共享存储路径即主场景。
// 节点侧找不到文件时**失败得明明白白**（错误里带上候选根），而不是静默跳过 ——
// 一个装错挂载点的节点应当立刻暴露，而不是产出 0 字节的"成功"。

import (
	"context"
	"fmt"
	"log"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/transcode"
)

// 任务类型常量。与 transcode_jobs.kind 的取值域对齐。
const (
	// KindNoop 空任务：证明闭环用。
	KindNoop = "noop"
	// KindHLS 多码率 HLS 转码。
	KindHLS = "hls"
)

// Executor 执行一个任务并返回可回传的结果。
//
// 约定：Execute **不应返回 error**，失败要表达为 ResultRequest{Status: failed, Error: ...}，
// 因为"任务失败"是需要回传给控制端的一等结果，而不是本地异常。返回 error 会让
// 主循环退化成"要么重试要么丢弃"，反而丢失了失败原因。
type Executor interface {
	// Name 执行器名，仅用于启动日志与排障。
	Name() string
	// Execute 同步执行一个任务；必须尊重 ctx（取消时应返回 failed 而不是 done）。
	Execute(ctx context.Context, job JobSpec) ResultRequest
}

// NoopExecutor 空执行器：立即成功，不读不写任何文件。
type NoopExecutor struct{}

// NewNoopExecutor 构造空执行器。
func NewNoopExecutor() Executor { return NoopExecutor{} }

// Name 执行器名。
func (NoopExecutor) Name() string { return "noop" }

// Execute 立即返回成功（取消时返回失败，避免停机时上报假成功）。
func (NoopExecutor) Execute(ctx context.Context, job JobSpec) ResultRequest {
	if err := ctx.Err(); err != nil {
		return ResultRequest{JobID: job.JobID, Status: JobStatusFailed, Error: "任务被取消: " + err.Error()}
	}
	return ResultRequest{JobID: job.JobID, Status: JobStatusDone}
}

// LocalExecutor 本地执行器：在本节点上用本机 CPU/GPU 执行任务。
//
// MediaRoots / HLSDir 必须由使用者在启动时给出（命令行或环境变量），
// **代码里不写任何具体路径** —— 与"不得出现任何具体节点的地址/凭据"是同一条红线：
// 路径同样是节点相关的部署事实，写死进软件会让它只在一台机器上正确。
type LocalExecutor struct {
	// Device cpu|cuda|auto。用于**日志与排障**；真正决定编码器的是 Encoder
	// （由 EncoderForDevice 结合节点真实能力算出，见 cmd/nodeagent 的装配）。
	Device string

	// Encoder 视频编码器。零值 = 软编 libx264（默认，任何环境都有）。
	// 硬编不可用时 internal/transcode 会自动回退软编重跑，不会让任务失败 ——
	// 故这里可以放心地按"节点自报能力"打开，而不必先探测 ffmpeg 的构建选项。
	Encoder ffmpeg.VideoEncoder

	// MediaRoots media.path 相对路径的解析根，按序探测（通常上传目录在前、索引根在后）。
	MediaRoots []string

	// HLSDir HLS 输出根目录；实际输出到 <HLSDir>/<mediaID>/。
	HLSDir string

	// SegSeconds HLS 分片时长（秒）；<= 0 取 transcode.DefaultSegSeconds。
	SegSeconds int
}

// EncoderForDevice 由 -device 与**节点真实探测到的能力**推出编码器。
//
// 为什么必须同时看两个输入：
//   - 只看 device：`-device cuda` 在没有显卡的机器上会每次转码都先失败一次再回退，
//     白白多花一次进程启动与探测成本（日志里还会持续出现"回退"告警）；
//   - 只看 hasNVENC：`-device cpu` 是运维**显式要求用 CPU**（例如把 GPU 留给别的任务），
//     此时即便有卡也不该占用它。
//
// 于是规则是"两者都成立才硬编"：显式非 cpu 的 device **且** 探测到 NVENC。
// 注意 auto 也算"非 cpu"——它表示"能用就用"，与探测结果结合正是它的语义。
func EncoderForDevice(device string, hasNVENC bool) ffmpeg.VideoEncoder {
	if hasNVENC && device != DeviceCPU {
		return ffmpeg.EncoderNVENC
	}
	return ffmpeg.EncoderX264
}

// NewLocalExecutor 构造本地执行器。路径相关字段由调用方按需补齐
// （先构造再赋值，或直接用结构体字面量）——它们没有合理的默认值。
func NewLocalExecutor(device string) *LocalExecutor { return &LocalExecutor{Device: device} }

// Name 执行器名。
func (*LocalExecutor) Name() string { return "local" }

// Execute 按 kind 分派任务。
//
// 未知 kind **必须失败**：早先的实现对所有任务一律返回 done，那会让一个还没接入的
// 任务类型看起来"执行成功"，控制端据此把任务标成 done —— 比失败危险得多。
func (e *LocalExecutor) Execute(ctx context.Context, job JobSpec) ResultRequest {
	if err := ctx.Err(); err != nil {
		return ResultRequest{JobID: job.JobID, Status: JobStatusFailed, Error: "任务被取消: " + err.Error()}
	}
	switch job.Kind {
	case KindNoop:
		return ResultRequest{JobID: job.JobID, Status: JobStatusDone}
	case KindHLS:
		return e.executeHLS(ctx, job)
	default:
		return ResultRequest{JobID: job.JobID, Status: JobStatusFailed,
			Error: fmt.Sprintf("本地执行器暂不支持的任务类型 %q（当前支持 %s|%s）", job.Kind, KindNoop, KindHLS)}
	}
}

// executeHLS 执行 HLS 转码。所有失败都带**可操作的**原因（路径/配置/ffmpeg 输出），
// 因为节点是无人值守的：控制端日志里的这一行往往是唯一的排障线索。
func (e *LocalExecutor) executeHLS(ctx context.Context, job JobSpec) ResultRequest {
	fail := func(format string, args ...any) ResultRequest {
		return ResultRequest{JobID: job.JobID, Status: JobStatusFailed, Error: fmt.Sprintf(format, args...)}
	}
	if job.MediaID == "" {
		return fail("任务缺少 media_id")
	}
	if job.InputPath == "" {
		return fail("任务缺少 input_path（控制端未能提供 media.path）")
	}
	if e.HLSDir == "" {
		return fail("节点未配置 HLS 输出目录（请用 -hls-dir 或 HLS_DIR 指定）")
	}

	input, err := transcode.ResolveMediaPath(job.InputPath, e.MediaRoots...)
	if err != nil {
		return fail("源文件在本节点不可达（候选媒体根 %v，media.path=%q）: %v",
			e.MediaRoots, job.InputPath, err)
	}

	log.Printf("compute: 开始 HLS 转码 media=%s profile=%q device=%s encoder=%s input=%s",
		job.MediaID, job.Profile, e.Device, e.Encoder, input)
	url, err := transcode.TranscodeHLS(ctx, transcode.HLSTranscodeRequest{
		MediaID:    job.MediaID,
		Input:      input,
		HLSDir:     e.HLSDir,
		Profile:    job.Profile,
		SegSeconds: e.SegSeconds,
		Encoder:    e.Encoder,
	})
	if err != nil {
		return fail("HLS 转码失败: %v", err)
	}
	return ResultRequest{JobID: job.JobID, Status: JobStatusDone, ResultPath: url}
}
