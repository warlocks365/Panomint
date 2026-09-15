// Package ffmpeg 提供 GPL 安全的 ffmpeg/ffprobe 子进程封装（不链接 libav*，仅进程调用）。
// 设计依据：TDD v1.1 §3.3 转码管线；许可约束见 PRD §12.2（P0：ffmpeg 仅 subprocess）。
package ffmpeg

import "time"

// Progress 一次转码的实时进度快照（来自 ffmpeg -progress 管道输出）。
type Progress struct {
	Frame       int64   // 已处理帧数
	FPS         float64 // 当前处理帧率
	BitrateKbps float64 // 当前输出码率（kbits/s）；N/A 时为 0
	OutTimeUs   int64   // 已处理时长（微秒）
	Speed       float64 // 处理速度倍数（1.0 = 实时）
	RemainingUs int64   // 预计剩余时长（微秒）；未知为 -1（需 WithExpectedDurationUs）
	Done        bool    // progress=end 时为 true
}

// Result 一次执行的最终结果。
type Result struct {
	ExitCode   int           // 进程退出码；被强制 Kill 时为 -1
	StderrTail string        // stderr 尾部（默认 20 行，WithTailLines 可调）
	Duration   time.Duration // 墙钟执行时长
	Canceled   bool          // 是否经 Cancel/ctx 取消（优雅 'q' 退出时 ExitCode 通常为 0）
}
