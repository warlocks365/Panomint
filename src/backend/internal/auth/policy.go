package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// AccountPolicy 账号策略（system_account_config singleton 的应用层形态）。
//
// 零值不是合法策略：LoadAccountPolicy 读不到行时返回 ErrConfigMissing，
// 由调用方决定降级口径（登录/注册路径 fail-closed 用 DefaultPolicy，
// 管理端读取路径报错）——见各自 handler 注释。
type AccountPolicy struct {
	AllowRegistration bool
	InviteRequired    bool
	PwdMinLength      int
	PwdRequireUpper   bool
	PwdRequireLower   bool
	PwdRequireDigit   bool
	PwdRequireSpecial bool
	LoginMaxAttempts  int
	LoginLockMinutes  int
}

// DefaultPolicy 与迁移 00043 的 DDL 默认值逐项一致：
// 注册关闭、无密码复杂度要求（沿用全局 MinPasswordLen=8）、连错 5 次锁 15 分钟。
// 用途：读库失败时登录/注册 fail-closed 的回退值 —— 绝不用"零值策略"放行（那是
// "作用域默认全开"式的反模式：读不到配置就当成什么限制都没有）。
func DefaultPolicy() AccountPolicy {
	return AccountPolicy{
		AllowRegistration: false,
		InviteRequired:    false,
		PwdMinLength:      MinPasswordLen,
		LoginMaxAttempts:  5,
		LoginLockMinutes:  15,
	}
}

// ErrPolicyMissing 策略行不存在（理论上迁移保证恒有 singleton 行，出现即环境异常）。
type ErrPolicyMissing struct{}

func (e ErrPolicyMissing) Error() string { return "账号策略配置行不存在（迁移 00043 未执行？）" }

// LoadAccountPolicy 读取 singleton 策略行。
func (s *Store) LoadAccountPolicy(ctx context.Context) (AccountPolicy, error) {
	var p AccountPolicy
	err := s.Pool.QueryRow(ctx, `
		SELECT allow_registration, invite_required, pwd_min_length,
		       pwd_require_upper, pwd_require_lower, pwd_require_digit, pwd_require_special,
		       login_max_attempts, login_lock_minutes
		FROM system_account_config WHERE singleton`).
		Scan(&p.AllowRegistration, &p.InviteRequired, &p.PwdMinLength,
			&p.PwdRequireUpper, &p.PwdRequireLower, &p.PwdRequireDigit, &p.PwdRequireSpecial,
			&p.LoginMaxAttempts, &p.LoginLockMinutes)
	if err != nil {
		return AccountPolicy{}, ErrPolicyMissing{}
	}
	return p, nil
}

// SaveAccountPolicy 覆写 singleton 策略行（管理端 PUT 的落点）。
// 字段合法性（长度范围/尝试次数范围）由 DDL CHECK + 调用方校验共同兜底。
func (s *Store) SaveAccountPolicy(ctx context.Context, p AccountPolicy) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE system_account_config SET
			allow_registration = $1, invite_required = $2,
			pwd_min_length = $3, pwd_require_upper = $4, pwd_require_lower = $5,
			pwd_require_digit = $6, pwd_require_special = $7,
			login_max_attempts = $8, login_lock_minutes = $9, updated_at = now()
		WHERE singleton`,
		p.AllowRegistration, p.InviteRequired,
		p.PwdMinLength, p.PwdRequireUpper, p.PwdRequireLower,
		p.PwdRequireDigit, p.PwdRequireSpecial,
		p.LoginMaxAttempts, p.LoginLockMinutes)
	return err
}

// ValidatePasswordPolicy 按 [自行决策] 策略校验密码强度（纯函数，穷举单测钉住）。
//
// 返回人类可读的中文错误（直接进 400 响应文案）；满足策略返回 nil。
// 长度按 rune 计（中文密码按字算，与"位数"的产品语义一致）。
func ValidatePasswordPolicy(pwd string, p AccountPolicy) error {
	n := len([]rune(pwd))
	if n < p.PwdMinLength {
		return fmt.Errorf("密码至少 %d 位", p.PwdMinLength)
	}
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range pwd {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		default:
			// 非字母数字一律算"特殊字符"（含空格）——与常见密码表单口径一致。
			hasSpecial = true
		}
	}
	var missing []string
	if p.PwdRequireUpper && !hasUpper {
		missing = append(missing, "大写字母")
	}
	if p.PwdRequireLower && !hasLower {
		missing = append(missing, "小写字母")
	}
	if p.PwdRequireDigit && !hasDigit {
		missing = append(missing, "数字")
	}
	if p.PwdRequireSpecial && !hasSpecial {
		missing = append(missing, "特殊字符")
	}
	if len(missing) > 0 {
		return fmt.Errorf("密码必须包含：%s", joinCN(missing))
	}
	return nil
}

// joinCN 中文顿号连接（避免为了一个 join 引 strings 又语义不清）。
func joinCN(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += "、"
		}
		out += s
	}
	return out
}

// GenerateInviteCode 生成邀请码明文：`inv-` + 16 位 hex（8 字节 crypto/rand，64 位熵）。
//
// 邀请码是"准入门卡"而非密钥：一次性 + 有过期时间，且必须能口头/聊天转述给受邀人，
// 因此入库明文（管理员可在列表复制重发）、格式带前缀便于人眼识别。
// 64 位熵对"猜码"攻击足够（尝试 2^63 次才有半数命中，且注册端本身在登录限流面后）。
func GenerateInviteCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "inv-" + hex.EncodeToString(b), nil
}

// LockState 登录锁定的判定结果（纯函数）。
type LockState struct {
	Locked          bool
	RemainingMinutes int
}

// EvaluateLock 按 locked_until 判定当前是否处于锁定（纯函数，穷举单测钉住）。
//
// 边界语义：locked_until == now 视为已解除（不冤枉多锁一秒）。
// ⚠️ 剩余时间必须以**传入的 now** 为基准（policy_test.go 曾揪出偷用 time.Until
// ——即真实系统时钟——的缺陷：传入 now 一变结果就错，"纯函数"名不副实）。
func EvaluateLock(lockedUntil *time.Time, now time.Time) LockState {
	if lockedUntil == nil || !lockedUntil.After(now) {
		return LockState{}
	}
	// ceil(秒/60)：15m0s→15、14m59s→15、90s→2、30s→1（对齐 policy_test 硬编码期望）
	remaining := (int(lockedUntil.Sub(now).Seconds()) + 59) / 60
	if remaining < 1 {
		remaining = 1
	}
	return LockState{Locked: true, RemainingMinutes: remaining}
}
