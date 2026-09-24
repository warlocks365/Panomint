// Package setup 首次安装引导（一次性初始化向导）后端。
//
// 生命周期（裁决自 Job000107，契约 §19）：
//
//   - users 表为空  → 未初始化：GET /setup/status 返回 {initialized:false}，
//     前端把所有非 /setup 导航重定向到向导；POST /setup 创建首个 owner 账号。
//   - 首个 owner 创建成功 → 已初始化：此后 POST /setup 恒 409 SETUP_COMPLETED，
//     引导流程**不再出现**（前端守卫 + 后端闸门双保险，后端为权威）。
//
// 安全语义：
//   - FAIL-CLOSED：判定"是否已初始化"的查询失败一律按"拒绝"处理（500），
//     绝不"拿不准就当已初始化"——那会把全新部署永久锁在向导外。
//   - 并发互斥：检查与创建在同一事务内，先取 pg_advisory_xact_lock 固定键，
//     两个同时提交的 setup 请求不可能都通过检查（先查再改的经典竞态在此被消除）。
//   - 权限最小化：首个账号固定 role=owner（角色由迁移种子保证存在），
//     不接受请求体指定角色——向导没有"自选角色"的合法场景。
//   - 越权同形：已初始化后 POST /setup 返回 409 而非 404/403——
//     向导页本身公开存在，409 明确表示"这一步已完成"，不是存在性预言机。
package setup

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
	"panoalbum/internal/auth"
	"panoalbum/internal/httperr"
	"panoalbum/internal/version"
)

// ErrAlreadyInitialized 系统已有用户（向导已完成或已用其他方式初始化）。
var ErrAlreadyInitialized = errors.New("系统已完成初始化")

// Store 抽象两个幂等原语，PGStore 为生产实现，测试用 fake 替换。
type Store interface {
	// Initialized 报告 users 表是否已有任何用户（查库失败必须返回 error，不许默认 true/false）。
	Initialized(ctx context.Context) (bool, error)
	// CreateOwner 在"当前确无用户"的前提下创建首个 owner；发现已有用户返回 ErrAlreadyInitialized。
	CreateOwner(ctx context.Context, email, displayName, password string) (string, error)
}

// PGStore 基于 pgxpool 的 Store 实现。
type PGStore struct {
	Pool *pgxpool.Pool
}

// setupLockKey 固定咨询锁键（任意选定的常量整数）：整个系统生命周期内
// "初始化互斥"只有这一把锁，键值无业务含义。
const setupLockKey = 742031

func (s *PGStore) Initialized(ctx context.Context) (bool, error) {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return exists, err
}

// CreateOwner 事务化"检查 + 建号"：
//
//	BEGIN → pg_advisory_xact_lock(setupLockKey) → EXISTS(users) → INSERT owner → COMMIT
//
// 锁随事务提交/回滚自动释放；INSERT 的角色子查询与 internal/auth.Store.CreateUser
// 保持同一 SQL 语义（owner 角色名由迁移种子保证）。bcrypt 散列在事务外先行计算，
// 缩短持锁窗口。
func (s *PGStore) CreateOwner(ctx context.Context, email, displayName, password string) (string, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, setupLockKey); err != nil {
		return "", err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return "", err
	}
	if exists {
		return "", ErrAlreadyInitialized
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO users (email, display_name, password_hash, role_id)
		VALUES ($1, $2, $3, (SELECT id FROM roles WHERE name = 'owner'))
		RETURNING id`, email, displayName, hash).Scan(&id)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

// Handler HTTP 处理器。
type Handler struct {
	Store Store
	Audit *audit.Recorder
}

type statusResponse struct {
	Initialized bool   `json:"initialized"`
	Version     string `json:"version"`
}

// Status GET /setup/status（公开，无鉴权）。
// 已初始化也返回 200（{initialized:true}）——前端路由守卫需要这个信号做双向重定向。
// 查库失败 → 500：向导状态不明时绝不放行初始化动作。
func (h *Handler) Status(c *gin.Context) {
	initialized, err := h.Store.Initialized(c.Request.Context())
	if err != nil {
		httperr.Abort(c, http.StatusInternalServerError, "INTERNAL", "初始化状态查询失败，请检查数据库连接")
		return
	}
	c.JSON(http.StatusOK, statusResponse{Initialized: initialized, Version: version.String()})
}

// Create POST /setup（公开，无鉴权）：创建首个 owner 账号，一次性。
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Email       string `json:"email" binding:"required,email"`
		DisplayName string `json:"display_name" binding:"omitempty,max=64"`
		Password    string `json:"password" binding:"required,min=8,max=128"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误（需合法邮箱，密码 8~128 位）")
		return
	}
	name := strings.TrimSpace(req.DisplayName)
	if name == "" {
		name = "管理员"
	}
	id, err := h.Store.CreateOwner(c.Request.Context(), strings.TrimSpace(req.Email), name, req.Password)
	if err != nil {
		if errors.Is(err, ErrAlreadyInitialized) {
			httperr.Abort(c, http.StatusConflict, "SETUP_COMPLETED", "系统已完成初始化，请直接登录")
			return
		}
		httperr.Abort(c, http.StatusInternalServerError, "INTERNAL", "初始化失败，请检查数据库连接后重试")
		return
	}
	// 审计：actor 即被创建者本人（自助初始化），detail 只记邮箱绝不记密码。
	if h.Audit != nil {
		h.Audit.Record(c.Request.Context(), audit.Entry{
			ActorUserID: id,
			Action:      "setup.complete",
			TargetType:  "user",
			TargetID:    id,
			IP:          c.ClientIP(),
			UserAgent:   c.Request.UserAgent(),
			Detail:      map[string]any{"email": req.Email},
		})
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "email": req.Email})
}
