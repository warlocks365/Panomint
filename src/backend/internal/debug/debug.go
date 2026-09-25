// Package debug 转码远程调试通道（Job000121）：一次性、短寿命、全审计的远程排障通道。
//
// 定位与边界（设计方案 文档/转码远程调试功能设计方案_v1.0.md，下称"设计"）：
//
//   - 管理员在设置页开启 → 系统签发 URL+密钥（明文仅此一次）→ 开发者机器上的
//     agent（cmd/debugctl）凭密钥经 WSS 接入，对转码控制面执行**白名单内的结构化命令**；
//   - **不提供任意 shell / 文件系统通路 / SQL 直通**（设计附录 A·Q1 裁决 L2 受控指令）；
//   - 全局单通道：单行表 + 同一时刻至多一个 agent 连接（第二个 409）。
//
// 安全模型要点：
//
//   - 双段凭据（设计 §3.1）：channel_id 不敏感（URL/日志/审计可见），access_key 敏感——
//     库中只存 sha256 摘要，明文仅 enable/rotate 响应一次，永不进审计/日志；
//   - 握手前置矩阵（设计 §5.2）：格式 400 → 未开启 404 → 槽占 409 → 无 Bearer 401 →
//     失败锁定 429 → 摘要校验 401 → TLS 426 → upgrade；
//   - 凭据变更（enable/disable/rotate）经 pg_advisory_xact_lock(742032) 事务内串行化
//     （Job000107 的 742031 同款先例），握手认证也取同一把锁，杜绝"换钥瞬间旧钥仍握手成功"；
//   - 审计八事件（设计 §7.1）全部走 internal/audit，detail 键名避开脱敏名单
//     （禁 hash/token/secret 等子串，统一用 channel_fp=channel_id 前 8 位）。
//
// 可用性取舍（设计 §6.5）：Valkey 不可用时限流/失败计数 fail-open——256-bit 密钥使在线
// 爆破无意义，限流只是纵深一层，不为限流可用性牺牲接入可用性。该决策写入代码注释。
package debug

import "errors"

// AdvisoryLockID 调试通道凭据变更/握手认证的串行化锁号（设计 §8.2）。
// 与 Job000107 安装向导的 742031 相邻而不重叠；2^31-1 内任取，全仓唯一即可。
const AdvisoryLockID = 742032

// 可选 TTL 档位（设计 §4.1）。DefaultTTLHours 是默认档。
const (
	TTL1h  = 1
	TTL8h  = 8
	TTL24h = 24
	TTL72h = 72

	DefaultTTLHours = TTL24h
)

// ValidTTL 判定 ttl_hours 是否为合法档位（设计 §10.2：其余值 400）。
func ValidTTL(h int) bool {
	switch h {
	case TTL1h, TTL8h, TTL24h, TTL72h:
		return true
	}
	return false
}

// WS 关闭码（设计 §4.2/§9）。4000+ 为应用自定义区间。
const (
	CloseExpired       = 4001 // TTL 到期（reaper/握手/心跳复核）
	CloseManualOff     = 4003 // 管理员手动关闭
	CloseKeyRotated    = 4004 // 密钥已轮换（旧连接即刻作废）
	CloseGoingAway     = 1001 // 死连接剔除（ping 超时）/ 服务停机
	CloseUnsupported   = 1003 // 畸形消息
	CloseMessageTooBig = 1007 // 单帧超限
)

// 哨兵错误（handler 层按 errors.Is 映射状态码/关闭码）。
var (
	ErrNotEnabled      = errors.New("debug: 调试通道未开启")
	ErrExpired         = errors.New("debug: 调试通道已过期")
	ErrAlreadyEnabled  = errors.New("debug: 调试通道已开启")
	ErrBadTTL          = errors.New("debug: ttl_hours 需为 1|8|24|72")
	ErrSlotBusy         = errors.New("debug: 已有活动调试连接")
	ErrAuthFailed       = errors.New("debug: 调试密钥无效")
	ErrLocked           = errors.New("debug: 认证失败过多，已锁定")
	ErrJobNotFound      = errors.New("debug: 任务不存在")
	ErrInvalidJobState  = errors.New("debug: 任务当前状态不允许该操作")
	ErrUnknownCommand   = errors.New("debug: 未知命令")
	ErrInvalidParams    = errors.New("debug: 命令参数非法")
	ErrTLSRequired      = errors.New("debug: 调试通道仅允许 WSS 接入")
)
