package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// MinPasswordLen 密码最短长度（与 POST /admin/users 的绑定校验一致）。
const MinPasswordLen = 8

// 用户状态取值。⚠️ users.status **没有 CHECK 约束**（实测 pg_constraint 里只有主键/唯一/外键），
// 所以"合法状态"这件事只能由应用层把关 —— 任何新写入路径都必须过 ValidUserStatus。
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// ValidUserStatus 校验用户状态取值。
func ValidUserStatus(s string) bool { return s == StatusActive || s == StatusDisabled }

// Store 账户数据访问。
type Store struct {
	Pool *pgxpool.Pool

	// permMu/permCache HasPerm 的进程内缓存（P2-12）：权限是低频变更数据，
	// 而 RequirePerm 每个受保护请求都至少查一次库。TTL 30s；
	// CreateRole / UpdateUser 等变更路径成功后主动调 InvalidatePermCache。
	permMu    sync.RWMutex
	permCache map[string]permCacheEntry
}

// permCacheEntry 一条缓存的判定结果（key 为 role+"\x00"+perm）。
type permCacheEntry struct {
	ok        bool
	expiresAt time.Time
}

// permCacheTTL 权限缓存有效期：角色权限变更最坏延迟 30s 生效（变更路径已主动失效，这只是兜底）。
const permCacheTTL = 30 * time.Second

// InvalidatePermCache 清空 HasPerm 缓存（角色/用户角色变更后调用）。
func (s *Store) InvalidatePermCache() {
	s.permMu.Lock()
	s.permCache = nil
	s.permMu.Unlock()
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

	// ErrUserNotFound 目标用户不存在。
	ErrUserNotFound = errors.New("用户不存在")
	// ErrRoleNotFound 指定的角色不存在。
	ErrRoleNotFound = errors.New("角色不存在")
	// ErrSelfLockout 不允许修改自己的角色或状态。
	//
	// 这条守卫的意义：管理员一旦把自己改成非 owner 或禁用，就可能**当场失去管理权限**
	// （甚至立刻登不进来），而恢复只能靠直接改库。把自己关在门外是纯自伤，没有正当场景。
	// 注意只拦"角色/状态"：改自己的昵称与密码是正常需求。
	ErrSelfLockout = errors.New("不能修改自己的角色或状态")
	// ErrLastOwner 不能移除最后一个可用的 owner。
	//
	// 系统里 owner 是唯一拥有 admin:system 的角色（实测 seed：admin/owner 两个角色有 admin:*，
	// 但默认只给 owner 建账号）。把最后一个 active owner 降级或禁用，会导致**再没有人能管理这台服务器**，
	// 且同样只能靠改库恢复。故必须在"改动会减少可用 owner 数"且"减完为 0"时拒绝。
	ErrLastOwner = errors.New("不能移除最后一个可用的 owner（否则系统将无人可管理）")
	// ErrUserHasAssets 该用户仍被业务数据引用，无法硬删除。
	ErrUserHasAssets = errors.New("该用户仍拥有媒体 / 相册 / 分享 / 审计等数据，无法删除")

	// ErrInvalidInput 入参非法（校验失败）。handler 据此映射 400，与其它包的同类错误语义一致。
	ErrInvalidInput = errors.New("输入非法")
)

// UserUpdate 用户部分更新（全部指针：nil = 不改动）。
type UserUpdate struct {
	DisplayName *string `json:"display_name"`
	Role        *string `json:"role"`
	Status      *string `json:"status"`
	Password    *string `json:"password"`
}

// Empty 是否没有任何实际改动。
func (u UserUpdate) Empty() bool {
	return u.DisplayName == nil && u.Role == nil && u.Status == nil && u.Password == nil
}

// ChangesRoleOrStatus 本次改动是否触及角色或状态（自锁守卫只针对这两项）。
func (u UserUpdate) ChangesRoleOrStatus() bool { return u.Role != nil || u.Status != nil }

// NormalizeUserUpdate 校验并归一化部分更新输入（纯函数，便于穷举单测）。
func NormalizeUserUpdate(in UserUpdate) (UserUpdate, error) {
	var out UserUpdate
	if in.DisplayName != nil {
		v := strings.TrimSpace(*in.DisplayName)
		out.DisplayName = &v
	}
	if in.Role != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Role))
		if v == "" {
			return out, errors.New("role 不能为空")
		}
		out.Role = &v
	}
	if in.Status != nil {
		v := strings.ToLower(strings.TrimSpace(*in.Status))
		if !ValidUserStatus(v) {
			return out, fmt.Errorf("status 仅支持 %s|%s", StatusActive, StatusDisabled)
		}
		out.Status = &v
	}
	if in.Password != nil {
		if len(*in.Password) < MinPasswordLen {
			return out, fmt.Errorf("密码至少 %d 位", MinPasswordLen)
		}
		out.Password = in.Password
	}
	return out, nil
}

// CheckUserPatch 判定这次改动是否被允许（纯函数）。
//
// 入参 otherActiveOwners = **除目标用户之外**还有几个 active 的 owner（由调用方查库得到）。
// 之所以把判定抽成纯函数：这两条守卫（自锁、最后 owner）是"改错了就再也进不去"的那类逻辑，
// 必须能穷举单测，而不是靠"部署后手动试一次"。
func CheckUserPatch(actorID string, target *User, in UserUpdate, otherActiveOwners int) error {
	if target == nil {
		return ErrUserNotFound
	}
	// 自锁：不许改自己的角色/状态（改昵称、改密码不受限）
	if in.ChangesRoleOrStatus() && target.ID == actorID {
		return ErrSelfLockout
	}
	// 最后 owner：仅当"目标当前是 active owner"且"本次改动会让它不再是 active owner"时判定
	if !(target.Role == "owner" && target.Status == StatusActive) {
		return nil
	}
	willLoseOwner := (in.Role != nil && *in.Role != "owner") ||
		(in.Status != nil && *in.Status != StatusActive)
	if willLoseOwner && otherActiveOwners <= 0 {
		return ErrLastOwner
	}
	return nil
}

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

// FindByEmailCI 大小写不敏感按邮箱查用户（SSO 用）。
//
// 为什么单列而不改 FindByEmail：既有本地登录的账号存在性/口令判断都走 = 精确匹配，
// 改它等于改变登录语义（含枚举面的微妙变化），不该搭 SSO 的车。SSO 的 email 来自
// IdP claims，各家 IdP 大小写口径不一（UPN 常大写），JIT 开通前用 lower() 对齐，
// 避免同一邮箱因大小写被判成两个人、重复开通。
func (s *Store) FindByEmailCI(ctx context.Context, email string) (*User, error) {
	var u User
	err := s.Pool.QueryRow(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name,''), u.password_hash, r.name, u.status,
		       COALESCE(u.mfa_secret,''), u.mfa_enabled
		FROM users u JOIN roles r ON r.id = u.role_id
		WHERE lower(u.email) = lower($1)`, email).
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

// AppPasswordHash 读取用户的应用密码哈希（WebDAV/第三方客户端用，Job000055）。
// 未设置返回空串（调用方回落主密码）。app_password_hash 是 DDL 早为"API 专用
// 应用密码"预留的列——WebDAV Basic 认证是它的第一个消费方。
func (s *Store) AppPasswordHash(ctx context.Context, userID string) (string, error) {
	var hash string
	err := s.Pool.QueryRow(ctx,
		`SELECT COALESCE(app_password_hash,'') FROM users WHERE id = $1`, userID).Scan(&hash)
	if err != nil {
		return "", err
	}
	return hash, nil
}

// SetAppPassword 写入（轮换）应用密码散列（Job000098）。单列直改，理由同 SetPassword：
// 通用 patch 路径会把"自助凭据管理"与"管理员改用户"的语义缠在一起。
// 应用密码是**服务端生成的随机串**，调用方拿到的明文只在这一次响应里出现一次。
func (s *Store) SetAppPassword(ctx context.Context, userID, hash string) error {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE users SET app_password_hash = $1, updated_at = now() WHERE id = $2::uuid`, hash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("用户不存在")
	}
	return nil
}

// ClearAppPassword 清除应用密码（Job000098）：置 NULL —— DAV 的 davPasswordOK
// 以 hash != "" 判定"已设置"，NULL 与空串都回落主密码，但置 NULL 才能与 DDL 缺省一致。
func (s *Store) ClearAppPassword(ctx context.Context, userID string) error {
	tag, err := s.Pool.Exec(ctx,
		`UPDATE users SET app_password_hash = NULL, updated_at = now() WHERE id = $1::uuid`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("用户不存在")
	}
	return nil
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

// PasswordHash 读取某用户的当前口令哈希（自助改密时校验旧口令用）。
func (s *Store) PasswordHash(ctx context.Context, userID string) (string, error) {
	var h string
	err := s.Pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1::uuid`, userID).Scan(&h)
	return h, err
}

// SetPassword 精确覆写某用户的口令哈希（自助改密用）。
//
// 刻意不复用 UpdateUser：那是一条"按请求体增量改用户"的通用路径（含角色/状态/显示名的
// normalize 与 patch 拼接），而改密只需动一个字段。套通用路径会把"自助改密"与
// "管理员改用户"的语义缠在一起，日后调整任一侧的校验都容易误伤另一侧。
func (s *Store) SetPassword(ctx context.Context, userID, hash string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2::uuid`, hash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("用户不存在")
	}
	return nil
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
//
// 吊销是**单条原子 UPDATE**（P2-10）：旧实现「先 SELECT revoked 再 UPDATE」存在竞态窗口——
// 两个并发请求可都读到 revoked=false、各自签出新会话，重放检测形同虚设。
// 现在靠 `revoked=false` 条件让并发换发只有一个能命中；0 行时再 SELECT 区分
// 「不存在/已过期」与「已吊销（重放）」，保持既有重放检测语义不变。
func (s *Store) RotateSession(ctx context.Context, oldRefresh, newRefresh, ip, ua string) (string, error) {
	h := hashRefresh(oldRefresh)
	var userID string
	err := s.Pool.QueryRow(ctx, `
		UPDATE sessions SET revoked = true
		WHERE refresh_token_hash = $1 AND revoked = false AND expires_at > now()
		RETURNING user_id`, h).Scan(&userID)
	if err == nil {
		return userID, s.CreateSession(ctx, userID, newRefresh, ip, ua)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	// 0 行：区分「会话不存在/已过期」与「已吊销（重放）」。
	var replayUserID string
	selErr := s.Pool.QueryRow(ctx,
		`SELECT user_id FROM sessions WHERE refresh_token_hash = $1 AND expires_at > now()`, h).
		Scan(&replayUserID)
	if errors.Is(selErr, pgx.ErrNoRows) {
		return "", errors.New("会话不存在或已过期")
	}
	if selErr != nil {
		return "", selErr
	}
	// 重放检测：旧令牌被二次使用 → 整链吊销
	_, _ = s.Pool.Exec(ctx, `UPDATE sessions SET revoked = true WHERE user_id = $1`, replayUserID)
	return "", errors.New("检测到令牌重放，已吊销全部会话")
}

// RevokeSession 登出吊销。
func (s *Store) RevokeSession(ctx context.Context, refreshToken string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE sessions SET revoked = true WHERE refresh_token_hash = $1`, hashRefresh(refreshToken))
	return err
}

// HasPerm 角色权限判定（精确匹配 + 前缀通配 'media:*' 可覆盖 'media:read'）。
// 带 30s 进程内缓存（见 Store.permCache）；库故障的结果**不缓存**，直接向上返回错误。
func (s *Store) HasPerm(ctx context.Context, role, perm string) (bool, error) {
	key := role + "\x00" + perm
	s.permMu.RLock()
	e, hit := s.permCache[key]
	s.permMu.RUnlock()
	if hit && time.Now().Before(e.expiresAt) {
		return e.ok, nil
	}

	var ok bool
	err := s.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
			WHERE r.name = $1 AND (rp.perm = $2 OR rp.perm = split_part($2, ':', 1) || ':*'))`,
		role, perm).Scan(&ok)
	if err != nil {
		return false, err
	}

	s.permMu.Lock()
	if s.permCache == nil {
		s.permCache = map[string]permCacheEntry{}
	}
	s.permCache[key] = permCacheEntry{ok: ok, expiresAt: time.Now().Add(permCacheTTL)}
	s.permMu.Unlock()
	return ok, nil
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

// ---------------------------------------------------------------------------
// 用户与角色管理（管理端）
// ---------------------------------------------------------------------------

// Role 角色及其权限（GET /admin/roles）。
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Perms       []string `json:"permissions"`
	// Users 该角色下的用户数。带上它是因为"能不能改/删这个角色"先要看有没有人还在用，
	// 让管理界面不必再单独发一次请求去数。
	Users int `json:"users"`
}

// GetUser 按 id 取用户，找不到时返回 ErrUserNotFound（FindByID 不区分，故单列一个）。
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	u, err := s.FindByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// RoleExists 角色名是否存在（用于在 UPDATE 之前给出干净的 400，而不是等外键/NOT NULL 报错）。
func (s *Store) RoleExists(ctx context.Context, name string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, name).Scan(&ok)
	return ok, err
}

// CountActiveOwnersExcept 统计**除 excludeID 之外**还有几个 active 的 owner。
//
// 供"最后 owner"守卫使用：传目标用户 id，得到的就是"改完还剩几个"。
func (s *Store) CountActiveOwnersExcept(ctx context.Context, excludeID string) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FROM users u JOIN roles r ON r.id = u.role_id
		WHERE r.name = 'owner' AND u.status = $1 AND u.id <> $2::uuid`,
		StatusActive, excludeID).Scan(&n)
	return n, err
}

// buildUserUpdate 拼 SET 子句与参数（纯函数）。
//
// 与 compute.buildNodeUpdate 同思路：占位符由"append 之后取 len(args)"生成，
// 编号与下标同源，杜绝两处各算一次导致的错位（那类错误 pgx 只会报参数个数不匹配，
// 或者更糟——静默写错列）。
//
// passwordHash 非空时才写 password_hash：密码哈希由调用方（handler 层）生成，
// 因为 bcrypt 是 CPU 开销，不该让纯拼装函数承担。
func buildUserUpdate(in UserUpdate, passwordHash string) (sets []string, args []any) {
	add := func(expr string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf(expr, len(args)))
	}
	if in.DisplayName != nil {
		add("display_name = $%d", *in.DisplayName)
	}
	if in.Role != nil {
		add("role_id = (SELECT id FROM roles WHERE name = $%d)", *in.Role)
	}
	if in.Status != nil {
		add("status = $%d", *in.Status)
	}
	if in.Password != nil && passwordHash != "" {
		add("password_hash = $%d", passwordHash)
	}
	return sets, args
}

// UpdateUser 部分更新用户。**不做守卫判定**（自锁 / 最后 owner 由 handler 用
// CheckUserPatch 判定后再调用），因为守卫需要"操作者是谁"这个 handler 才有的信息。
func (s *Store) UpdateUser(ctx context.Context, id string, in UserUpdate) (*User, error) {
	in, err := NormalizeUserUpdate(in)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	if in.Empty() {
		return nil, fmt.Errorf("%w: 没有任何待更新字段", ErrInvalidInput)
	}
	if in.Role != nil {
		ok, err := s.RoleExists(ctx, *in.Role)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrRoleNotFound
		}
	}

	var hash string
	if in.Password != nil {
		if hash, err = HashPassword(*in.Password); err != nil {
			return nil, err
		}
	}

	sets, args := buildUserUpdate(in, hash)
	args = append(args, id)
	q := fmt.Sprintf(`UPDATE users SET %s, updated_at = now() WHERE id = $%d::uuid
		RETURNING id, email, COALESCE(display_name,''), '', (SELECT name FROM roles WHERE id = role_id), status,
		          COALESCE(mfa_secret,''), mfa_enabled`,
		strings.Join(sets, ", "), len(args))

	var u User
	err = s.Pool.QueryRow(ctx, q, args...).Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash,
		&u.Role, &u.Status, &u.MFASecret, &u.MFAEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u.MFAPending = u.MFASecret != "" && !u.MFAEnabled
	if in.Role != nil {
		// 用户角色变更：权限判定缓存立即失效（否则旧角色权限最长残留 30s）。
		s.InvalidatePermCache()
	}
	return &u, nil
}

// RevokeAllSessions 吊销某用户的全部会话。
//
// 为什么禁用用户时必须连带吊销：access token 在签出后到过期前是**自证**的（服务端不查库），
// 单靠 status 拦不住手上已有令牌的人；更要命的是 refresh 会不断换出新的 access token。
// 只改状态而不清会话，等于"禁用"要等最长 7 天（refresh TTL）才真正生效。
func (s *Store) RevokeAllSessions(ctx context.Context, userID string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked = true WHERE user_id = $1::uuid AND revoked = false`, userID)
	return err
}

// DeleteUser 硬删除用户。
//
// ⚠️ **仍有「资产」时会被外键拒绝，这是刻意的**：albums / media / memories / share_links /
// shared_space / index_jobs 六张表都以 NO ACTION 引用 users(id)
// （2026-09-18 用 information_schema.referential_constraints 实测的清单），
// 意思是"这些数据必须继续可归属到某个人"。因此真正可行的"删除"是**禁用**
// （status=disabled）；本方法只在用户确实没有任何此类归属数据时（例如建错的账号）
// 才会成功，否则返回 ErrUserHasAssets 让调用方改用禁用。
//
// **审计不再阻止删账号**：audit_log.user_id 已由迁移 00025 改为 ON DELETE SET NULL，
// 且迁移 00026 给它加了 actor_email（**写入时**快照的操作者邮箱）。删账号只把该账号审计行的
// actor 引用置 NULL、审计行原样保留，归因仍在 —— 原设计"审计尤其不能因为删掉用户就失去主体"
// 的意图，已由 actor_email 快照满足，不再靠"拒绝删除"来满足。
//
// sessions / user_preferences / user_ui_prefs / shared_space_members / album_comments 是 CASCADE，会随删。
func (s *Store) DeleteUser(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1::uuid`, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign_key_violation
		return ErrUserHasAssets
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ListRoles 角色与权限列表（GET /admin/roles）。
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT r.name, COALESCE(r.description,''),
		       COALESCE(string_agg(rp.perm, ',' ORDER BY rp.perm), ''),
		       (SELECT count(*) FROM users u WHERE u.role_id = r.id)
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id
		GROUP BY r.id, r.name, r.description
		ORDER BY r.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Role{}
	for rows.Next() {
		var r Role
		var perms string
		if err := rows.Scan(&r.Name, &r.Description, &perms, &r.Users); err != nil {
			return nil, err
		}
		r.Perms = []string{}
		if perms != "" {
			r.Perms = strings.Split(perms, ",")
		}
		out = append(out, r)
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
