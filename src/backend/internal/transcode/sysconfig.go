package transcode

// 转码系统级开关（Job000120-r2，用户裁决 2026-09-25：从 user_ui_prefs 用户级偏好
// 上收为管理后台「转码」页签维护的系统级配置，全站生效）。
//
// 存储 = 单行表 system_transcode_config（singleton 主键硬约束，迁移 00038；
// system_map_config 的 00023 同款先例），写侧 ON CONFLICT (singleton) 原子 upsert。
//
// 语义（与 Job000120 首版一致，仅管理面从用户上收到管理员）：
//
//   · auto_transcode=true（默认）：播放器/API 可正常发起 HLS 转码；
//   · false：CreateJob 一律 409 TRANSCODE_DISABLED；
//   · CLI 批量补排（transcodectl enqueue-videos）不走本端点、不受此开关约束——
//     开关管「使用侧的自动/随手触发」，不管「管理侧的批量运维」。
//
// 读侧两条底线：
//
//   · 无行 = 默认开启（与 DDL DEFAULT TRUE 一致）——从没打开过管理页签的部署行为不变；
//   · 查询出错 fail-closed（返回错误，由调用方 500）——宁可拒绝服务，
//     也不在无把握时放行转码（闸门被静默绕过比转码被暂停危险得多）。

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DefaultAutoTranscode 系统开关缺省值（无配置行时）。钉死为 true：存量部署升级后
// 行为必须逐字节不变。
const DefaultAutoTranscode = true

// GetSystemAutoTranscode 读取系统级自动转码开关；无行返回默认值，查询出错向上抛。
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

// PutSystemAutoTranscode 原子 upsert 系统开关（单行主键保证 ON CONFLICT 语义正确）。
func PutSystemAutoTranscode(ctx context.Context, pool *pgxpool.Pool, enabled bool) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO system_transcode_config (singleton, auto_transcode, updated_at)
		VALUES (TRUE, $1, now())
		ON CONFLICT (singleton) DO UPDATE SET
			auto_transcode = EXCLUDED.auto_transcode,
			updated_at     = now()`, enabled)
	return err
}

// SystemConfigView GET /transcode/config 与 GET/PUT /admin/transcode-config 的响应视图。
type SystemConfigView struct {
	AutoTranscode bool       `json:"auto_transcode"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty"`
}

// GetSystemConfig 读开关 + 最近更新时间（无行时 updated_at 缺省、开关=默认）。
func GetSystemConfig(ctx context.Context, pool *pgxpool.Pool) (*SystemConfigView, error) {
	out := &SystemConfigView{AutoTranscode: DefaultAutoTranscode}
	var enabled *bool
	var updated *time.Time
	err := pool.QueryRow(ctx,
		`SELECT auto_transcode, updated_at FROM system_transcode_config WHERE singleton = TRUE`).
		Scan(&enabled, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if enabled != nil {
		out.AutoTranscode = *enabled
	}
	out.UpdatedAt = updated
	return out, nil
}
