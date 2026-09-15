package compute

import (
	"context"
	"log"
)

// 任务执行器（节点侧）。
//
// ⚠️ 本交付项**刻意不接真实 ffmpeg**：目标是把「拉取 → 执行 → 回传」的协议闭环
// 先跑通，把范围锁在协议层。真实转码一旦进来就会拖出输入路径解析、档位/分片规则、
// 软硬编选择、进度上报、失败重试等一整套转码子系统的问题，那是后续交付项的事。
//
// 所以这里只有接口 + 两个实现，且两个实现当前行为相同（立即成功）：
//   - NoopExecutor  默认；证明闭环，不做任何 IO；
//   - LocalExecutor 预留的真实执行器外壳，Device 决定将来软编还是硬编。
//
// 两者的区别只有 Name()，存在的意义是让"执行器可替换"这件事在类型系统里成立：
// 节点命令行用 -executor noop|local 选择，未来把 local 换成真 ffmpeg 实现时，
// 协议层、客户端、主循环、命令行**一行都不用改**。

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

// LocalExecutor 本地执行器外壳（本机 CPU / GPU 转码）。
//
// Device 取 cpu|cuda|auto，与节点的 -device 同源——真实的编码器选择
// （libx264 vs h264_nvenc）将由它决定，从而保证"同一份代码既跑得动无卡机器，
// 也用得上独显"。
type LocalExecutor struct {
	Device string
}

// NewLocalExecutor 构造本地执行器。
func NewLocalExecutor(device string) Executor { return LocalExecutor{Device: device} }

// Name 执行器名。
func (LocalExecutor) Name() string { return "local" }

// Execute ⚠️ **真实 ffmpeg 的唯一落地点**。
//
// 接入时在此处：按 Device 选择软编（libx264）或硬编（h264_nvenc），
// 读 job.InputPath、按 job.Profile/OutputSpec 产出 HLS，并把产出目录回填进
// ResultPath。当前实现与 noop 相同（立即成功），是为了把本次交付范围锁在协议闭环。
func (e LocalExecutor) Execute(ctx context.Context, job JobSpec) ResultRequest {
	if err := ctx.Err(); err != nil {
		return ResultRequest{JobID: job.JobID, Status: JobStatusFailed, Error: "任务被取消: " + err.Error()}
	}
	log.Printf("compute: local 执行器（device=%s）尚未接入 ffmpeg，任务 %s 按 noop 处理", e.Device, job.JobID)
	return ResultRequest{JobID: job.JobID, Status: JobStatusDone}
}
