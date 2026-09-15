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
)

// ---- 错误码（与项目既有响应包络一致：{"error":{"code","message"}}） ----

const (
	CodeUnauthorized      = "UNAUTHORIZED"        // 缺少 Authorization 头
	CodeInvalidAgentToken = "INVALID_AGENT_TOKEN" // 节点令牌无效或已过期
	CodeNodeNotFound      = "NODE_NOT_FOUND"
	CodeInvalidInput      = "INVALID_INPUT"
	CodeJobNotFound       = "JOB_NOT_FOUND"       // 回传的任务不存在或不属于本节点
	CodeJobNotOwned       = "JOB_NOT_OWNED"
	CodeNodeBusy          = "NODE_BUSY"
	CodeInternal          = "INTERNAL"
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
