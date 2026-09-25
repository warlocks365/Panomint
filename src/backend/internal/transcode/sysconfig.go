package transcode

// 转码系统级开关（Job000120-r2 首建；Job000124 增列 realtime_transcode）。
//
// 存储 = 单行表 system_transcode_config（singleton 主键硬约束，迁移 00038；
// realtime_transcode 列迁移 00040），写侧 ON CONFLICT (singleton) 原子 upsert。
//
// 两开关职责（Job000124 设计文档 §1.5，AND 关系）：
//
//   · auto_transcode（默认 true，Job000120-r2）= **总闸门**：false 时 CreateJob 一律
//     409 TRANSCODE_DISABLED；CLI 批量补排（transcodectl enqueue-videos）不走本端点、
//     不受开关约束——开关管「使用侧的自动/随手触发」，不管「管理侧的批量运维」；
//   · realtime_transcode（默认 false，Job000124）= **触发方式自动化**：true 时播放器
//     对无 HLS 的视频自动发起转码（CreateJob auto 分支），false 保持手动按钮现状；
//     有效自动触发 = auto_transcode AND realtime_transcode（管理 UI 把 realtime 在
//     总闸门关时禁用，AND 语义兜底库中可能出现的 (false, true) 姿态）。
//
// 读侧两条底线（与 Job000120-r2 一致）：
//
//   · 无行 = 默认值（与 DDL DEFAULT 一致）——从没打开过管理页签的部署行为不变；
//   · 查询出错 fail-closed（返回错误，由调用方 500）——宁可拒绝服务，
//     也不在无把握时放行转码（闸门被静默绕过比转码被暂停危险得多）。

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultAutoTranscode 总闸门缺省值（无配置行时）。钉死为 true：存量部署升级后
// 行为必须逐字节不变。
const DefaultAutoTranscode = true

// DefaultRealtimeTranscode 播放时自动转码缺省值（无配置行时）。钉死为 false：
// Job000124 之前不存在该能力，存量部署升级后必须保持「手动发起转码」不变。
const DefaultRealtimeTranscode = false

// GetSystemAutoTranscode 读取系统级自动转码总闸门；无行返回默认值，查询出错向上抛。
func GetSystemAutoTranscode(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	var enabled bool
	err := pool.QueryRow(ctx,
		`SELECT auto_transcode FROM system_transcode_config WHERE singleton = TRUE`).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultAutoTranscode, nil
	}
	if err != nil {
		return false, err
	}
	return enabled, nil
}

// PutSystemAutoTranscode 原子 upsert 总闸门（仅动 auto_transcode 列，realtime 列保持原值/默认）。
// 保留 Job000120-r2 的签名：CLI 与旧测试共用；实现委托 PutSystemConfig 的 COALESCE 部分更新。
func PutSystemAutoTranscode(ctx context.Context, pool *pgxpool.Pool, enabled bool) error {
	return PutSystemConfig(ctx, pool, &enabled, nil)
}

// SystemConfigView GET /transcode/config 与 GET/PUT /admin/transcode-config 的响应视图。
// RealtimeTranscode 为 Job000124 增量 key：旧前端忽略之不炸（向后兼容）。
type SystemConfigView struct {
	AutoTranscode     bool       `json:"auto_transcode"`
	RealtimeTranscode bool       `json:"realtime_transcode"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

// GetSystemConfig 读开关 + 最近更新时间（无行时 updated_at 缺省、开关=默认值）。
func GetSystemConfig(ctx context.Context, pool *pgxpool.Pool) (*SystemConfigView, error) {
	out := &SystemConfigView{AutoTranscode: DefaultAutoTranscode, RealtimeTranscode: DefaultRealtimeTranscode}
	var enabled, realtime *bool
	var updated *time.Time
	err := pool.QueryRow(ctx,
		`SELECT auto_transcode, realtime_transcode, updated_at FROM system_transcode_config WHERE singleton = TRUE`).
		Scan(&enabled, &realtime, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if enabled != nil {
		out.AutoTranscode = *enabled
	}
	if realtime != nil {
		out.RealtimeTranscode = *realtime
	}
	out.UpdatedAt = updated
	return out, nil
}

// PutSystemConfig 部分更新两个开关：nil = 该字段不动（Job000124 沿用 Job000123 的
// 「缺失不改」PUT 语义，防止管理端并发写互相覆盖）。两字段都 nil 视为调用方缺陷，
// 返回错误（Handler 层先挡 400，这是第二道）。
func PutSystemConfig(ctx context.Context, pool *pgxpool.Pool, auto, realtime *bool) error {
	if auto == nil && realtime == nil {
		return errors.New("PutSystemConfig: 至少需提供一个待更新字段")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO system_transcode_config (singleton, auto_transcode, realtime_transcode, updated_at)
		VALUES (TRUE, COALESCE($1, $3), COALESCE($2, $4), now())
		ON CONFLICT (singleton) DO UPDATE SET
			auto_transcode     = COALESCE($1, system_transcode_config.auto_transcode),
			realtime_transcode = COALESCE($2, system_transcode_config.realtime_transcode),
			updated_at         = now()`,
		auto, realtime, DefaultAutoTranscode, DefaultRealtimeTranscode)
	return err
}
