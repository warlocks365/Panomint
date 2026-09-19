package auth

// 角色管理（契约 §2 的 `POST /admin/roles`「新建角色 + 权限」）。
//
// # 权限词表是白名单，不是自由文本
//
// `role_permissions.perm` 是自由文本列（没有 permissions 表可做外键），所以"合法权限"
// 只能由应用层把关。白名单取自**两份真值的并集**（2026-09-16 实测）：
//   - 库内 `role_permissions` 的去重取值（11 个）；
//   - 代码里真正 `RequirePerm` 检查过的权限名。
// 存进一个拼错的权限串不会报错，只会**永远匹配不上**（表现为"给了权限却仍然 403"），
// 这是最难查的一类问题之一，因此必须在写入时就拒掉。
//
// # ⚠️ 提权守卫（本文件最重要的部分）
//
// `POST /admin/roles` 挂在 `admin:users` 下（与契约一致），而 `POST /admin/users` 允许
// **按名字指定任意角色**。两者一叠加，只持 `admin:users` 的人就能：
//   建一个含 `admin:system` 的新角色 → 建一个用该角色的账号 → 登录 → 拿到 admin:system。
// 于是本文件强制一条不变量：
//
//	**只能授予调用者自己已拥有的权限**（通配符展开后比较，见 PermCovered）。
//
// 这样 `admin:users` 无法自我提权到 `admin:system`，但 owner/admin（两者都有 admin:*）
// 仍可自由建角色。这是把"权限授予"收敛为"权限子集"的标准做法。

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// knownPerms 权限白名单（两份真值的并集，见文件头）。
var knownPerms = []string{
	"media:read", "media:write", "media:*",
	"album:read", "album:write", "album:*",
	"share:create", "share:*",
	"space:*",
	"admin:users", "admin:system",
}

// KnownPerms 返回权限白名单的副本（供 handler 文档化与测试断言）。
func KnownPerms() []string {
	out := make([]string, len(knownPerms))
	copy(out, knownPerms)
	return out
}

// roleNameRe 角色名：小写字母开头，其后为小写字母/数字/下划线（与内置角色同风格）。
// 刻意不允许大写与空格：角色名会出现在 URL 查询、审计 detail 与前端下拉里，
// 允许大小写混排只会让"看起来一样的两个角色"难以排查。
var roleNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)

var (
	// ErrRoleExists 角色名已存在。
	ErrRoleExists = errors.New("角色已存在")
	// ErrPermNotAllowed 请求的权限不在白名单内。
	ErrPermNotAllowed = errors.New("权限名不在白名单内")
	// ErrPermNotGrantable 调用者不具备该权限，因此不能授予他人（提权守卫）。
	ErrPermNotGrantable = errors.New("只能授予自己已拥有的权限")
)

// RoleInput 新建角色入参。
type RoleInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// NormalizeRoleInput 校验并归一化（纯函数，便于穷举单测）。
// 去重保持输入顺序；权限名统一小写去空白。
func NormalizeRoleInput(in RoleInput) (RoleInput, error) {
	var out RoleInput
	out.Name = strings.ToLower(strings.TrimSpace(in.Name))
	out.Description = strings.TrimSpace(in.Description)
	if !roleNameRe.MatchString(out.Name) {
		return out, errors.New("role 名需以小写字母开头、仅含小写字母/数字/下划线，长度 2..32")
	}
	if len(out.Description) > 200 {
		return out, errors.New("description 过长（≤200 字符）")
	}

	seen := map[string]bool{}
	out.Permissions = []string{}
	for _, raw := range in.Permissions {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		if !isKnownPerm(p) {
			return out, fmt.Errorf("%w: %q（可用：%s）", ErrPermNotAllowed, p, strings.Join(knownPerms, ","))
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out.Permissions = append(out.Permissions, p)
	}
	// 空权限角色是"能登录但什么都做不了"，几乎必然是漏填 —— 拒掉比留下一个谜之角色好。
	if len(out.Permissions) == 0 {
		return out, errors.New("permissions 不能为空")
	}
	return out, nil
}

func isKnownPerm(p string) bool {
	for _, k := range knownPerms {
		if k == p {
			return true
		}
	}
	return false
}

// PermCovered 判断 callerPerms 是否覆盖 want（**通配符展开**）。
//
// 语义与 SQL 侧的 HasPerm 保持一致：`media:*` 覆盖任意 `media:<任意>`。
// 两处若不一致，就会出现"守卫说能授、实际却 403"（或反之）的诡异现象，
// 故这里刻意复刻同一条规则并单测钉住。
//
// 两侧都做大小写/空白归一：库内种子是全小写，但归一化让"某天有人写进了大写权限名"
// 不会变成"合法管理员突然授不出权限"（那种故障很难联想到这里）。
func PermCovered(callerPerms []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	if want == "" {
		return false
	}
	family := want
	if i := strings.Index(want, ":"); i >= 0 {
		family = want[:i]
	}
	for _, raw := range callerPerms {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == want {
			return true
		}
		// 通配符：`<family>:*` 覆盖该族全部权限
		if p == family+":*" {
			return true
		}
		// 调用者持有全局通配（如 `*`）时一律放行（当前词表里没有，留作前向兼容）
		if p == "*" {
			return true
		}
	}
	return false
}

// PermsOfRole 取某角色已授予的权限（提权守卫的输入）。
func (s *Store) PermsOfRole(ctx context.Context, roleName string) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT rp.perm FROM role_permissions rp JOIN roles r ON r.id = rp.role_id
		WHERE r.name = $1 ORDER BY rp.perm`, roleName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateRole 新建角色并授予权限。**角色行与权限行必须在同一事务里落库** ——
// 否则一次失败会留下一个"存在但没有任何权限"的角色（能登录、什么都做不了，极难排查）。
func (s *Store) CreateRole(ctx context.Context, in RoleInput) (*Role, error) {
	norm, err := NormalizeRoleInput(in)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var roleID string
	err = tx.QueryRow(ctx,
		`INSERT INTO roles (name, description) VALUES ($1, NULLIF($2,'')) RETURNING id`,
		norm.Name, norm.Description).Scan(&roleID)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
		return nil, ErrRoleExists
	}
	if err != nil {
		return nil, err
	}

	for _, p := range norm.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_permissions (role_id, perm) VALUES ($1, $2)`, roleID, p); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// 角色权限落库：HasPerm 缓存立即失效（否则同名旧判定最长残留 30s）。
	s.InvalidatePermCache()
	return &Role{Name: norm.Name, Description: norm.Description, Perms: norm.Permissions, Users: 0}, nil
}

// RoleByName 按名字取角色（含权限），不存在返回 ErrRoleNotFound。
// 供 handler 在创建后回读，也供守卫路径查询调用者角色。
func (s *Store) RoleByName(ctx context.Context, name string) (*Role, error) {
	var r Role
	var perms string
	err := s.Pool.QueryRow(ctx, `
		SELECT r.name, COALESCE(r.description,''), COALESCE(string_agg(rp.perm, ',' ORDER BY rp.perm), '')
		FROM roles r LEFT JOIN role_permissions rp ON rp.role_id = r.id
		WHERE r.name = $1
		GROUP BY r.id, r.name, r.description`, name).
		Scan(&r.Name, &r.Description, &perms)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}
	r.Perms = []string{}
	if perms != "" {
		r.Perms = strings.Split(perms, ",")
	}
	return &r, nil
}
