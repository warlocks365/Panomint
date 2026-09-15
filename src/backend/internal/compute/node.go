package compute

import (
	"fmt"
	"strings"
	"time"
)

// 节点领域模型与输入校验（无 DB / 无 HTTP 依赖，便于单测）。
//
// 设计依据：TDD v1.1 §6.1；契约 v1.1 §15。
// 红线：kind 三种取值一律平权——local_gpu（本机显卡）/ cloud_gpu（云 GPU）/ lan_agent（局域网第三方
// GPU 主机经 agent 接入），代码与配置中不得对任何具体节点做特殊处理。

// Kind 节点类型。与 compute_nodes.kind 的取值域一致（varchar(16)，见 DDL v1.1）。
type Kind string

const (
	KindLocalGPU Kind = "local_gpu" // 本机 GPU（进程内直接调用，无需 agent）
	KindCloudGPU Kind = "cloud_gpu" // 云 GPU 实例
	KindLANAgent Kind = "lan_agent" // 局域网第三方 GPU / CPU 主机，经 agent 守护进程接入
)

// ValidKind 校验节点类型。
func ValidKind(k Kind) bool {
	switch k {
	case KindLocalGPU, KindCloudGPU, KindLANAgent:
		return true
	}
	return false
}

// Status 节点状态。与 compute_nodes.status 的取值域一致。
//
// 注意区分「存储状态」与「生效状态」：库里的 status 是最后一次显式写入的值，
// 可能是陈旧的 online（节点已崩溃但没人改过）。对外一律看 Node.EffectiveStatus
// ——它由 EffectiveStatus() 在**查询侧**按心跳超时修正，不需要任何定时任务。
type Status string

const (
	StatusOnline  Status = "online"
	StatusBusy    Status = "busy"
	StatusOffline Status = "offline"
)

// ValidStatus 校验节点状态。
func ValidStatus(s Status) bool {
	switch s {
	case StatusOnline, StatusBusy, StatusOffline:
		return true
	}
	return false
}

const (
	// DefaultCodecs 未声明能力时的默认编码（compute_nodes.codecs 的库默认值）。
	DefaultCodecs = "h264"

	// MaxConcurrency 单节点并发任务数上限，防止一次登记把并发开到失控。
	MaxConcurrency = 64
)

// Node 一个算力节点的对外表示。
type Node struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   Kind   `json:"kind"`
	Host   string `json:"host,omitempty"` // IP/域名；可为空（本机节点无需填）
	Codecs string `json:"codecs"`         // 如 "h264,hevc"

	HasNVENC    bool `json:"has_nvenc"`   // 无 CUDA/无 NVENC 的节点为 false，以 CPU 继续工作
	VRAMMB      *int `json:"vram_mb"`     // 显存容量（MB）；库列可空，CPU-only 节点为 nil 或 0
	Concurrency int  `json:"concurrency"` // 并发任务数

	Status          Status     `json:"status"`           // 库中存储的原始状态（可能是陈旧的 online）
	EffectiveStatus Status     `json:"effective_status"` // 查询侧按心跳超时修正后的状态，对外以此为准
	LastHeartbeat   *time.Time `json:"last_heartbeat"`   // 最后一次心跳时刻；nil = 从未心跳
	CreatedAt       time.Time  `json:"created_at"`
}

// RegisterInput 登记节点（POST /compute-nodes，契约 §15）。
//
// 注意：本结构**不包含 agent_token**。令牌由服务端生成、只以 sha256 哈希入库，
// 明文仅在响应里返回一次给调用方（见 Store.Register）。调用方不得期望能再次取回明文。
type RegisterInput struct {
	Name        string `json:"name"`
	Kind        Kind   `json:"kind"`
	Host        string `json:"host"`
	Codecs      string `json:"codecs"`
	HasNVENC    bool   `json:"has_nvenc"`
	VRAMMB      *int   `json:"vram_mb"`
	Concurrency int    `json:"concurrency"`
}

// Normalize 校验并补齐默认值（就地修改）。
//
// 默认值刻意与服务端 DDL 的列默认保持一致（codecs=h264、concurrency=1），
// 避免「登记时不传 → 库填默认 → 代码里又当成 0」这种双份默认值漂移。
func (in *RegisterInput) Normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.Codecs = strings.TrimSpace(in.Codecs)

	if in.Name == "" {
		return fmt.Errorf("节点名不能为空")
	}
	if !ValidKind(in.Kind) {
		return fmt.Errorf("节点类型 %q 非法，需为 local_gpu|cloud_gpu|lan_agent", in.Kind)
	}
	if in.Codecs == "" {
		in.Codecs = DefaultCodecs
	}
	if in.Concurrency <= 0 {
		in.Concurrency = 1
	}
	if in.Concurrency > MaxConcurrency {
		return fmt.Errorf("并发数 %d 超过上限 %d", in.Concurrency, MaxConcurrency)
	}
	if in.VRAMMB != nil && *in.VRAMMB < 0 {
		return fmt.Errorf("显存容量不能为负")
	}
	return nil
}

// NeedsAgentToken 是否需要生成 Agent 接入令牌。
//
// 只有 lan_agent（第三方主机经 agent 进程接入）才需要令牌；local_gpu / cloud_gpu 由控制端
// 自己调用算力，不存在"外部机器来敲门"，给它们发令牌只会平白多一把钥匙。
func (in RegisterInput) NeedsAgentToken() bool { return in.Kind == KindLANAgent }

// PatchInput 更新节点（PATCH /compute-nodes/:id，契约 §15）。
// 全部能力字段皆为指针：nil = 不改动，非 nil = 覆盖。这种"部分更新"语义是为了让
// 上下线（只传 status）与改能力（只传 codecs）都不必回传完整对象。
type PatchInput struct {
	Name        *string `json:"name"`
	Host        *string `json:"host"`
	Status      *Status `json:"status"`
	Codecs      *string `json:"codecs"`
	HasNVENC    *bool   `json:"has_nvenc"`
	VRAMMB      *int    `json:"vram_mb"`
	Concurrency *int    `json:"concurrency"`

	// RotateToken 轮换 agent 接入令牌（TDD §6.1「管理员触发」）。
	// 轮换后旧令牌立即失效，新明文令牌在本次响应里返回一次。
	RotateToken bool `json:"rotate_token"`
}

// Validate 校验部分更新输入。
func (in *PatchInput) Validate() error {
	if in.Name != nil {
		s := strings.TrimSpace(*in.Name)
		if s == "" {
			return fmt.Errorf("节点名不能为空")
		}
		in.Name = &s
	}
	if in.Host != nil {
		s := strings.TrimSpace(*in.Host)
		in.Host = &s
	}
	if in.Status != nil && !ValidStatus(*in.Status) {
		return fmt.Errorf("状态 %q 非法，需为 online|busy|offline", *in.Status)
	}
	if in.Codecs != nil {
		s := strings.TrimSpace(*in.Codecs)
		if s == "" {
			return fmt.Errorf("codecs 不能为空串（不改动请省略该字段）")
		}
		in.Codecs = &s
	}
	if in.Concurrency != nil {
		if *in.Concurrency <= 0 || *in.Concurrency > MaxConcurrency {
			return fmt.Errorf("并发数 %d 非法，需为 1..%d", *in.Concurrency, MaxConcurrency)
		}
	}
	if in.VRAMMB != nil && *in.VRAMMB < 0 {
		return fmt.Errorf("显存容量不能为负")
	}
	return nil
}

// Empty 是否没有任何实际改动（用于拒绝空 PATCH，避免误触发一次 UPDATE）。
func (in PatchInput) Empty() bool {
	return in.Name == nil && in.Host == nil && in.Status == nil && in.Codecs == nil &&
		in.HasNVENC == nil && in.VRAMMB == nil && in.Concurrency == nil && !in.RotateToken
}
