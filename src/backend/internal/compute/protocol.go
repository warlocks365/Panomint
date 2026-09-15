// Package compute 算力节点（compute node）的存储层与节点 agent 协议层。
//
// 设计依据：TDD v1.1 §6.1「算力节点 Agent 协议」；契约 v1.1 §15。
//
// ⚠️ 长期红线——节点一律平权：
//   - 本地 GPU / 局域网第三方 GPU / 云 GPU 走**同一套协议与同一套代码**；
//   - 本包及 cmd/nodeagent 中**不得出现任何具体节点的地址、路径、凭据或默认值**；
//   - 开发期用作验证的那台机器只是"一个普通节点"，其地址/令牌只能经启动参数或环境变量传入；
//   - GPU 与 CPU **双接口必须同时保留**：无 CUDA 的环境下不得报错退出，而是声明
//     has_nvenc=false 继续以 CPU 工作（见 capabilities.go）。
//
// 本文件只放**双端共享的线协议类型**与时间常量，不含任何 DB / HTTP 逻辑：
// 服务端（handlers.go / agentapi.go）与节点侧（client.go / agent.go）都依赖它。
package compute

import (
	"time"
)

// ---- 协议时间常量（TDD §6.1） ----
//
// TDD §6.1：心跳「每 60s 发送一次」，且「3 次未收到 → 标记 offline」。
// 故离线阈值 = 3 × 心跳周期 = 180s，而非单次心跳周期。
// 判定**只在查询侧做**（见 offline.go），不引入常驻定时任务。
const (
	// DefaultHeartbeatInterval 节点默认心跳周期（TDD §6.1：60s）。
	DefaultHeartbeatInterval = 60 * time.Second

	// DefaultOfflineAfter 心跳静默多久视为离线（TDD §6.1：3 次未收到 → offline）。
	DefaultOfflineAfter = 3 * DefaultHeartbeatInterval

	// DefaultPollInterval 节点默认拉取任务周期。与心跳同源，避免再引入一个独立节奏。
	DefaultPollInterval = DefaultHeartbeatInterval

	// MaxPollJobs 单次拉取的任务数上限（防止一个节点一次吞掉整条队列）。
	MaxPollJobs = 16

	// DefaultReclaimAfter 「在跑任务的节点已僵死」的判定阈值，用于把僵死节点名下
	// 仍处于 running 的任务收回队列（见 store.go 的 reclaimJobsSQL）。
	//
	// 为什么比离线阈值再宽一倍：节点在 DefaultOfflineAfter（180s）已被判为 offline，
	// 但「判离线」只意味着停止给它派新任务，代价可控；而把**已经领走、可能正在执行**
	// 的任务收回重派，代价高得多——若原节点其实还活着（短暂网络分区、心跳丢包、
	// 时钟偏差），就会出现两个节点同时处理同一条媒体、产出互相覆盖。
	// 故回收比离线判定更保守：再多等一个离线阈值才动手。
	//
	// ⚠️ 判据是**节点心跳**，不是**任务已运行时长**。这是刻意的：节点侧的心跳已与
	// 任务执行解耦（见 agent.go 的 pollAndRun），健康节点跑几小时的大转码时心跳照常发，
	// 因此长任务不会被误回收；只有"节点不再报到了"才会被回收。
	DefaultReclaimAfter = 2 * DefaultOfflineAfter

	// DefaultMaxAttempts 单条转码任务被**认领**的次数上限（迁移 00022 的 transcode_jobs.attempts）。
	//
	// 存在意义：回收重派没有上限时，一个反复"领了任务就死"的节点会让同一条任务被无限重派，
	// 每次白烧一遍算力。取 3 与 TDD §6.1「3 次未收到心跳 → offline」同源 ——
	// 一次失败可能是网络抖动，三次都失败就基本可以判定这条任务在这个集群里跑不通，
	// 应当变成一条**看得见**的 failed 记录，而不是永远 pending。
	//
	// 可用 COMPUTE_MAX_ATTEMPTS 覆盖；<= 0 会被归一到本默认值（0 会让认领条件
	// `attempts < 0` 恒假、任何任务都领不到，属危险配置）。
	DefaultMaxAttempts = 3
)

// ReclaimAfter 由离线阈值推出回收阈值：2 × offlineAfter；offlineAfter <= 0 时取默认值。
//
// 与 DefaultReclaimAfter 同源，保证"调整离线阈值"时回收阈值随之缩放，
// 不会出现"离线判定已改成 30s、回收却仍等 360s"这类两套节奏打架的情况。
func ReclaimAfter(offlineAfter time.Duration) time.Duration {
	if offlineAfter <= 0 {
		return DefaultReclaimAfter
	}
	return 2 * offlineAfter
}

// ---- 错误码（与项目既有响应包络一致：{"error":{"code","message"}}） ----

const (
	CodeUnauthorized      = "UNAUTHORIZED"        // 缺少 Authorization 头
	CodeInvalidAgentToken = "INVALID_AGENT_TOKEN" // 节点令牌无效或已过期
	CodeNodeNotFound      = "NODE_NOT_FOUND"
	CodeInvalidInput      = "INVALID_INPUT"
	CodeJobNotFound       = "JOB_NOT_FOUND" // 回传的任务不存在或不属于本节点
	CodeJobNotOwned       = "JOB_NOT_OWNED"
	// CodeJobFinalized 任务已是终态（done/failed），迟到的回传被拒绝。
	// 与 JOB_NOT_FOUND 分开是为了让节点侧能区分「我记错了任务 id」与
	// 「任务已经结束（可能被回收后由别的节点完成）」——前者要查 bug，后者是正常竞态。
	CodeJobFinalized = "JOB_FINALIZED"
	CodeNodeBusy     = "NODE_BUSY"
	CodeInternal     = "INTERNAL"
)

// APIError 统一错误响应体。
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// APIErrorEnvelope 契约 §15 的既有错误包络。
type APIErrorEnvelope struct {
	Error APIError `json:"error"`
}

// ---- 任务拉取（TDD §6.1：agent 发送 {type:"poll"} → 服务端返回任务数组或空） ----

// JobSpec 服务端派发给节点的单个任务描述。
// 字段名对齐 TDD §6.1：{job_id, kind, media_id, profile, input_path, output_spec}。
type JobSpec struct {
	JobID   string `json:"job_id"`
	Kind    string `json:"kind"`     // noop | hls | thumbnail | memories
	MediaID string `json:"media_id"` // 关联 media.id（UUID 文本）
	Profile string `json:"profile,omitempty"`

	// Attempts 这是第几次被认领（1-based，认领时自增）。节点据此在日志里打出
	// "第 2/3 次尝试"，让"某条任务总在重试"这类问题不必翻数据库就能看出来。
	Attempts int `json:"attempts,omitempty"`

	// InputPath 输入素材路径。当前为 media.path 的库内取值（相对路径），
	// 真实绝对路径解析（MediaRoot / UploadDir 回退，同 internal/transcode）留待接入
	// ffmpeg 时落地——本交付项的执行器是 noop，不读该字段。
	InputPath string `json:"input_path,omitempty"`

	// OutputSpec 输出规格。TDD §6.1 列为 poll 返回字段；本交付项尚无真实转码产出，
	// 故暂为空对象，待 ffmpeg 执行器落地后填充（如档位/分片目录）。
	OutputSpec map[string]string `json:"output_spec,omitempty"`
}

// PollRequest 节点拉取任务的请求体（HTTP 化后的 {type:"poll"}）。
type PollRequest struct {
	Max int `json:"max,omitempty"` // 期望拉取上限；0 或负数取 MaxPollJobs
}

// PollResponse 拉取响应；无任务时 Jobs 为空数组而非 null。
type PollResponse struct {
	Jobs       []JobSpec `json:"jobs"`
	ServerTime time.Time `json:"server_time"`
}

// ---- 心跳（TDD §6.1：{type:"heartbeat", gpu_util, vram_used, active_tasks}） ----

// HeartbeatRequest 节点心跳。指针字段用于区分「未上报」与「上报了零值」：
// 能力类字段（Codecs/HasNVENC/VRAMMB/Concurrency）只在非 nil 时覆盖库中记录，
// 对应 TDD §6.1「重连后重新注册能力」——节点首次心跳即携带完整能力声明。
type HeartbeatRequest struct {
	// Status 节点自报状态：online | busy。留空表示只续心跳不改状态。
	// 注意：offline 只能由管理端 PATCH 显式下线，或由查询侧心跳超时判定，节点不得自报。
	Status string `json:"status,omitempty"`

	// 能力声明（首次心跳 / 重连后重新声明）
	Codecs      *string `json:"codecs,omitempty"`      // 如 "h264,hevc"
	HasNVENC    *bool   `json:"has_nvenc,omitempty"`   // 无 CUDA 的节点上报 false，以 CPU 继续工作
	VRAMMB      *int    `json:"vram_mb,omitempty"`     // 显存容量（CPU-only 节点为 0）
	Concurrency *int    `json:"concurrency,omitempty"` // 并发任务数

	// 实时负载（TDD §6.1 的 gpu_util / vram_used / active_tasks）
	GPUUtil     *float64 `json:"gpu_util,omitempty"`     // 0~100；CPU-only 节点留空
	VRAMUsedMB  *int     `json:"vram_used_mb,omitempty"` // 已用显存
	ActiveTasks int      `json:"active_tasks"`
}

// HeartbeatResponse 心跳响应。节点据此对齐服务端的离线阈值，
// 避免节点侧自造一套与 TDD 不一致的超时参数。
type HeartbeatResponse struct {
	NodeID                   string    `json:"node_id"`
	Status                   string    `json:"status"` // 服务端记录的（经超时修正后的）状态
	ServerTime               time.Time `json:"server_time"`
	HeartbeatIntervalSeconds int       `json:"heartbeat_interval_seconds"`
	OfflineAfterSeconds      int       `json:"offline_after_seconds"`
}

// ---- 结果回传（TDD §6.1：{type:"result", job_id, status:"done"...}） ----

// 任务终态。与 transcode_jobs.status 既有取值（pending|running|done|failed）一致。
const (
	JobStatusDone   = "done"
	JobStatusFailed = "failed"
)

// ResultRequest 节点回传的任务结果。
type ResultRequest struct {
	JobID      string `json:"job_id"`
	Status     string `json:"status"`                // done | failed
	ResultPath string `json:"result_path,omitempty"` // 产出路径（noop 为占位）
	Error      string `json:"error,omitempty"`       // status=failed 时的原因
}
