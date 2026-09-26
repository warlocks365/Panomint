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
	"fmt"
	"strings"
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

// Job000125 新增三配置的缺省值：与迁移 00042 的 DDL DEFAULT 逐字一致。
// seg 用 DefaultSegSeconds（hls.go）作唯一真源；缓存档 balanced 逐字节复刻历史响应头行为；
// 外部地址缺省空 = 站内相对路径（历史行为）。
const (
	DefaultHLSCacheProfile = "balanced"
	DefaultStreamBaseURL   = ""
)

// HLS 缓存策略合法枚举（写侧校验；读侧未知值按 balanced 处理——防御脏数据）。
var validHLSCacheProfiles = map[string]bool{"no_cache": true, "balanced": true, "aggressive": true}

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
// HLSSegSeconds/HLSCacheProfile/StreamBaseURL 为 Job000125 增量 key，同规则。
type SystemConfigView struct {
	AutoTranscode     bool       `json:"auto_transcode"`
	RealtimeTranscode bool       `json:"realtime_transcode"`
	HLSSegSeconds     int        `json:"hls_seg_seconds"`
	HLSCacheProfile   string     `json:"hls_cache_profile"`
	StreamBaseURL     string     `json:"stream_base_url"`
	UpdatedAt         *time.Time `json:"updated_at,omitempty"`
}

// GetSystemConfig 读开关 + 最近更新时间（无行时 updated_at 缺省、开关=默认值）。
// Job000125：同时读 HLS 三列；列由迁移 00042 补齐（NOT NULL DEFAULT），存量行直接可用。
func GetSystemConfig(ctx context.Context, pool *pgxpool.Pool) (*SystemConfigView, error) {
	out := &SystemConfigView{
		AutoTranscode:     DefaultAutoTranscode,
		RealtimeTranscode: DefaultRealtimeTranscode,
		HLSSegSeconds:     DefaultSegSeconds,
		HLSCacheProfile:   DefaultHLSCacheProfile,
		StreamBaseURL:     DefaultStreamBaseURL,
	}
	var enabled, realtime *bool
	var seg *int
	var profile, baseURL *string
	var updated *time.Time
	err := pool.QueryRow(ctx,
		`SELECT auto_transcode, realtime_transcode, hls_seg_seconds, hls_cache_profile, stream_base_url, updated_at
		 FROM system_transcode_config WHERE singleton = TRUE`).
		Scan(&enabled, &realtime, &seg, &profile, &baseURL, &updated)
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
	if seg != nil {
		out.HLSSegSeconds = *seg
	}
	if profile != nil && *profile != "" {
		out.HLSCacheProfile = *profile
	}
	if baseURL != nil {
		out.StreamBaseURL = *baseURL
	}
	return out, nil
}

// ErrValidation 配置校验失败哨兵（调用方输入缺陷）——Handler 据此映射 400，
// 与真库错误（500）区分；isValidationErr 供 Handler 判型。
var ErrValidation = errors.New("config validation")

// isValidationErr 判断错误是否为配置校验类（400）而非存储层故障（500）。
func isValidationErr(err error) bool { return errors.Is(err, ErrValidation) }

// SystemConfigUpdate 部分更新的字段集：nil = 该字段不动（「缺失不改」语义，Job000125 扩展）。
type SystemConfigUpdate struct {
	Auto          *bool
	Realtime      *bool
	SegSeconds    *int
	CacheProfile  *string
	StreamBaseURL *string
}

// validateHLSUpdate 校验 Job000125 三字段（占位符错位/枚举外的值必须在**写入前**挡下，
// 而不是等 PG 报 CHECK/解析错误）。返回带字段名的错误，Handler 直接映射 400。
func validateHLSUpdate(u SystemConfigUpdate) error {
	if u.SegSeconds != nil && (*u.SegSeconds < 2 || *u.SegSeconds > 20) {
		return fmt.Errorf("%w: hls_seg_seconds 需为 2-20 的整数", ErrValidation)
	}
	if u.CacheProfile != nil && !validHLSCacheProfiles[*u.CacheProfile] {
		return fmt.Errorf("%w: hls_cache_profile 需为 no_cache|balanced|aggressive", ErrValidation)
	}
	if u.StreamBaseURL != nil && *u.StreamBaseURL != "" {
		if err := validateStreamBaseURL(*u.StreamBaseURL); err != nil {
			return err
		}
	}
	return nil
}

// validateStreamBaseURL 外部流媒体地址形态：http(s):// 开头、可解析、无 query/fragment、
// 不以 / 结尾（播放端拼接 master.m3u8 前缀时按「原样 + 相对路径」处理，尾斜杠会造成 //）。
func validateStreamBaseURL(s string) error {
	if !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		return fmt.Errorf("%w: stream_base_url 须以 http:// 或 https:// 开头（或留空使用站内地址）", ErrValidation)
	}
	if strings.ContainsAny(s, "?# \t\r\n") {
		return fmt.Errorf("%w: stream_base_url 不得含查询串、片段或空白字符", ErrValidation)
	}
	if strings.HasSuffix(s, "/") {
		return fmt.Errorf("%w: stream_base_url 不应以 / 结尾", ErrValidation)
	}
	return nil
}

// PutSystemConfigFields 结构化部分更新（Job000125）：至少一个字段非 nil，且 HLS 三字段过校验。
// SQL 用 9 个占位符：6 个新值 + 3 个 INSERT 缺省兜底值（与 DDL DEFAULT 一致）。
// 显式 ::type casts 与 Job000124 同因：nil 指针以 unknown 类型进 COALESCE 会撞 42804。
func PutSystemConfigFields(ctx context.Context, pool *pgxpool.Pool, u SystemConfigUpdate) error {
	if u.Auto == nil && u.Realtime == nil && u.SegSeconds == nil && u.CacheProfile == nil && u.StreamBaseURL == nil {
		return fmt.Errorf("%w: PutSystemConfigFields 至少需提供一个待更新字段", ErrValidation)
	}
	if err := validateHLSUpdate(u); err != nil {
		return err
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO system_transcode_config
			(singleton, auto_transcode, realtime_transcode, hls_seg_seconds, hls_cache_profile, stream_base_url, updated_at)
		VALUES (TRUE,
			COALESCE($1::boolean, $6::boolean),
			COALESCE($2::boolean, $7::boolean),
			COALESCE($3::int, $8::int),
			COALESCE($4::text, $9::text),
			COALESCE($5::text, $10::text),
			now())
		ON CONFLICT (singleton) DO UPDATE SET
			auto_transcode   = COALESCE($1, system_transcode_config.auto_transcode),
			realtime_transcode = COALESCE($2, system_transcode_config.realtime_transcode),
			hls_seg_seconds  = COALESCE($3, system_transcode_config.hls_seg_seconds),
			hls_cache_profile = COALESCE($4, system_transcode_config.hls_cache_profile),
			stream_base_url  = COALESCE($5, system_transcode_config.stream_base_url),
			updated_at       = now()`,
		u.Auto, u.Realtime, u.SegSeconds, u.CacheProfile, u.StreamBaseURL,
		DefaultAutoTranscode, DefaultRealtimeTranscode, DefaultSegSeconds, DefaultHLSCacheProfile, DefaultStreamBaseURL)
	return err
}

// PutSystemConfig 部分更新两个开关：nil = 该字段不动（Job000124 沿用 Job000123 的
// 「缺失不改」PUT 语义，防止管理端并发写互相覆盖）。两字段都 nil 视为调用方缺陷，
// 返回错误（Handler 层先挡 400，这是第二道）。
// 保留旧签名：PutSystemAutoTranscode 与既有测试共用；实现委托结构化版本。
func PutSystemConfig(ctx context.Context, pool *pgxpool.Pool, auto, realtime *bool) error {
	return PutSystemConfigFields(ctx, pool, SystemConfigUpdate{Auto: auto, Realtime: realtime})
}
