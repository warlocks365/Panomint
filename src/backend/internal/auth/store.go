package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// Store 账户数据访问。
type Store struct {
	Pool *pgxpool.Pool
}

// User 用户记录（含角色名）。
type User struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"`
	Status       string `json:"status"`

	// MFAEnabled 二次验证（TOTP）是否已生效。
	MFAEnabled bool `json:"mfa_enabled"`
	// MFAPending 「已生成密钥但尚未确认」——设置二次验证分两步（setup → confirm），
	// 中间态必须对界面可见，否则用户刷新页面后就无从知道"我当时配到哪一步了"。
	MFAPending bool `json:"mfa_pending"`

	// MFASecret TOTP 密钥（base32）。**绝不序列化**（json:"-"）：它是可离线生成口令的
	// 长期凭据，泄漏等于二次验证形同虚设。仅在服务端校验路径上使用。
	MFASecret string `json:"-"`
}

var (
	ErrBadCredentials = errors.New("邮箱或密码错误")
	ErrUserDisabled   = errors.New("账户已禁用")

	// ErrMFAAlreadyEnabled 已启用二次验证时不允许直接重新绑定密钥。
	//
	// 为什么不许"一键重绑"：那等于给会话劫持者一条替换认证器的捷径
	// （换成攻击者自己的认证器，真实用户就被锁在外面）。要重绑必须先关闭，
	// 而关闭需要提交一次有效的现有口令。
	ErrMFAAlreadyEnabled = errors.New("二次验证已启用，请先关闭再重新绑定")
	// ErrMFANotPending 没有待确认的密钥（跳过了 setup，或已被消耗）。
	ErrMFANotPending = errors.New("没有待确认的二次验证设置")
	// ErrMFANotEnabled 二次验证尚未启用（无需关闭）。
	ErrMFANotEnabled = errors.New("二次验证未启用")
)

// FindByEmail 按邮箱查用户。
func (s *Store) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), u.password_hash, r.name, u.status,
		       COALESCE(u.mfa_secret,''), u.mfa_enabled
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.email = $1`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status,
			&u.MFASecret, &u.MFAEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	u.MFAPending = u.MFASecret != "" && !u.MFAEnabled
	return &u, nil
}

// FindByID 按 ID 查用户（me 端点与二次验证端点）。
func (s *Store) FindByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), '', r.name, u.status,
		       COALESCE(u.mfa_secret,''), u.mfa_enabled
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, id).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status,
			&u.MFASecret, &u.MFAEnabled)
	if err != nil {
		return nil, err
	}
	u.MFAPending = u.MFASecret != "" && !u.MFAEnabled
	return &u, nil
}

// VerifyPassword bcrypt 校验。
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword bcrypt 加密（cost 默认 10）。
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(h), err
}

func hashRefresh(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession 写入会话（refresh 哈希入库，DDL sessions 已定）。
func (s *Store) CreateSession(ctx context.Context, userID, refreshToken, ip, ua string) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO sessions (user_id, refresh_token_hash, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)`,
		userID, hashRefresh(refreshToken), ip, ua, time.Now().Add(RefreshTTL))
	return err
}

// RotateSession 轮换：校验旧 refresh → 吊销旧会话 → 签发新会话。
// 返回用户 ID；若旧令牌已被吊销（重放攻击特征），吊销该用户全部会话并报错。
func (s *Store) RotateSession(ctx context.Context, oldRefresh, newRefresh, ip, ua string) (string, error) {
	h := hashRefresh(oldRefresh)
	var userID string
	var revoked bool
	err := s.Pool.QueryRow(ctx,
		`SELECT user_id, revoked FROM sessions WHERE refresh_token_hash = $1 AND expires_at > now()`, h).
		Scan(&userID, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errors.New("会话不存在或已过期")
	}
	if err != nil {
		return "", err
	}
	if revoked {
		// 重放检测：旧令牌被二次使用 → 整链吊销
		_, _ = s.Pool.Exec(ctx, `UPDATE sessions SET revoked = true WHERE user_id = $1`, userID)
		return "", errors.New("检测到令牌重放，已吊销全部会话")
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked = true WHERE refresh_token_hash = $1`, h); err != nil {
		return "", err
	}
	return userID, s.CreateSession(ctx, userID, newRefresh, ip, ua)
}

// RevokeSession 登出吊销。
func (s *Store) RevokeSession(ctx context.Context, refreshToken string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE sessions SET revoked = true WHERE refresh_token_hash = $1`, hashRefresh(refreshToken))
	return err
}

// HasPerm 角色权限判定（精确匹配 + 前缀通配 'media:*' 可覆盖 'media:read'）。
func (s *Store) HasPerm(ctx context.Context, role, perm string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
			WHERE r.name = $1 AND (rp.perm = $2 OR rp.perm = split_part($2, ':', 1) || ':*'))`,
		role, perm).Scan(&ok)
	return ok, err
}

// CreateUser 管理员创建用户。
func (s *Store) CreateUser(ctx context.Context, email, displayName, password, roleName string) (string, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return "", err
	}
	var id string
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO users (email, display_name, password_hash, role_id)
		VALUES ($1, $2, $3, (SELECT id FROM roles WHERE name = $4))
		RETURNING id`, email, displayName, hash, roleName).Scan(&id)
	return id, err
}

// ListUsers 用户列表（管理端点）。
func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), '', r.name, u.status,
		       COALESCE(u.mfa_secret,''), u.mfa_enabled
		FROM users u JOIN roles r ON r.id = u.role_id ORDER BY u.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status,
			&u.MFASecret, &u.MFAEnabled); err != nil {
			return nil, err
		}
		u.MFAPending = u.MFASecret != "" && !u.MFAEnabled
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// 二次验证（TOTP）状态变更
//
// 三个方法都用**带状态守卫的 UPDATE**（WHERE 里带上前置状态），而不是"先查再改"：
//   - 先查再改在并发下会竞态（两次 setup 同时通过检查 → 后写覆盖先写）；
//   - 守卫写在 SQL 里，判断与写入是同一个原子操作，天然无竞态。
// 代价是"影响 0 行"会同时覆盖"用户不存在"与"状态不对"两种情况 ——
// 此处刻意接受：调用方（handler）已经在同一请求里查过用户，能把状态判清楚并给出准确错误码。
// ---------------------------------------------------------------------------

// SetPendingMFASecret 写入待确认的 TOTP 密钥，并确保 mfa_enabled 仍为 false。
// 已启用时返回 ErrMFAAlreadyEnabled（必须先 disable 才能重绑）。
func (s *Store) SetPendingMFASecret(ctx context.Context, userID, secret string) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE users SET mfa_secret = $2, mfa_enabled = false, updated_at = now()
		WHERE id = $1 AND mfa_enabled = false`, userID, secret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMFAAlreadyEnabled
	}
	return nil
}

// EnableMFA 确认启用（要求已存在非空密钥）。无待确认密钥时返回 ErrMFANotPending。
func (s *Store) EnableMFA(ctx context.Context, userID string) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE users SET mfa_enabled = true, updated_at = now()
		WHERE id = $1 AND mfa_secret IS NOT NULL AND mfa_secret <> ''`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMFANotPending
	}
	return nil
}

// DisableMFA 关闭二次验证并**清除密钥**（不留残值：留着的密钥日后可能被误用或被拖走）。
func (s *Store) DisableMFA(ctx context.Context, userID string) error {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE users SET mfa_enabled = false, mfa_secret = NULL, updated_at = now()
		WHERE id = $1 AND mfa_enabled = true`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMFANotEnabled
	}
	return nil
}

// EnsureSeedAdmin 确保 owner 管理员存在（首次启动种子；默认密码仅开发用，生产必须改）。
func (s *Store) EnsureSeedAdmin(ctx context.Context, email, password string) error {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, email).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err := s.CreateUser(ctx, email, "系统管理员", password, "owner")
	return err
}
