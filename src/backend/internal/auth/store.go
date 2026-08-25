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
}

var (
	ErrBadCredentials = errors.New("邮箱或密码错误")
	ErrUserDisabled   = errors.New("账户已禁用")
)

// FindByEmail 按邮箱查用户。
func (s *Store) FindByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), u.password_hash, r.name, u.status
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.email = $1`, email).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBadCredentials
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByID 按 ID 查用户（me 端点）。
func (s *Store) FindByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), '', r.name, u.status
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE u.id = $1`, id).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status)
	if err != nil {
		return nil, err
	}
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
		SELECT u.id, u.email, COALESCE(u.display_name,''), '', r.name, u.status
		FROM users u JOIN roles r ON r.id = u.role_id ORDER BY u.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.Status); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
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
