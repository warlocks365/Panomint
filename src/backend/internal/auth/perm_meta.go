package auth

// Job000067（F1 权限体系增强）后端：权限元数据（中文化/自定义分类）+ 用户级增量授权。
// 边界（设计裁决，见计划 v2.0 §三-1）：
//   - perm_meta 只影响展示；enforcement 真源不变（knownPerms 白名单 + role_permissions）。
//   - user_permissions 只做**增量授予**（OR 关系），永不减角色权；只能授予调用者自己拥有的权限
//     （PermCovered 同口径，防借用户授权提权）。
//   - RequirePerm = 角色权限 OR 用户增量授权（HasPerm 的 role 缓存不动，用户层直查无缓存——
//     用户授权是低频管理操作，写时立即生效比缓存一致性重要）。

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
)

// PermMeta 权限展示元数据（perm 为 knownPerms 白名单成员）。
type PermMeta struct {
	Perm        string `json:"perm"`
	Label       string `json:"label"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// ListPermMeta GET /admin/perms——全量权限词表（白名单 ∪ 库内）+ 展示元数据。
// 库内出现但白名单没有的 perm 也返回（历史数据可见），标记 orphan=true 供清理。
func (h *Handler) ListPermMeta(c *gin.Context) {
	rows, err := h.Store.Pool.Query(c.Request.Context(),
		`SELECT perm, label, category, COALESCE(description,'') FROM perm_meta ORDER BY category, perm`)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	byPerm := map[string]PermMeta{}
	for rows.Next() {
		var m PermMeta
		if err := rows.Scan(&m.Perm, &m.Label, &m.Category, &m.Description); err == nil {
			byPerm[m.Perm] = m
		}
	}
	out := []gin.H{}
	known := map[string]bool{}
	for _, p := range knownPerms {
		known[p] = true
		m := byPerm[p]
		if m.Perm == "" {
			m = PermMeta{Perm: p, Label: p, Category: "其他", Description: ""}
		}
		out = append(out, gin.H{"perm": m.Perm, "label": m.Label, "category": m.Category,
			"description": m.Description, "orphan": false})
	}
	// 白名单外但库内有记录的（历史/拼错）如实列出，便于管理端发现并清理
	for p, m := range byPerm {
		if !known[p] {
			out = append(out, gin.H{"perm": m.Perm, "label": m.Label, "category": m.Category,
				"description": m.Description, "orphan": true})
		}
	}
	c.JSON(http.StatusOK, gin.H{"perms": out})
}

// PutPermMeta PUT /admin/perms/:perm——改中文名/分类/描述（分类可新建，相当于"自定义权限分类"）。
// 白名单外一律 400（防把展示层当成 enforcement 后门）。
func (h *Handler) PutPermMeta(c *gin.Context) {
	perm := c.Param("perm")
	inKnown := false
	for _, p := range knownPerms {
		if p == perm {
			inKnown = true
			break
		}
	}
	if !inKnown {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "perm 不在白名单（展示层不能引入新权限）")
		return
	}
	var req struct {
		Label       string `json:"label"`
		Category    string `json:"category"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "中文名不能为空")
		return
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "其他"
	}
	_, err := h.Store.Pool.Exec(c.Request.Context(),
		`INSERT INTO perm_meta (perm, label, category, description, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (perm) DO UPDATE SET label=EXCLUDED.label, category=EXCLUDED.category,
		                                  description=EXCLUDED.description, updated_at=now()`,
		perm, label, category, strings.TrimSpace(req.Description))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "保存失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"perm": perm, "label": label, "category": category})
}

// ListUserPerms GET /admin/users/:id/perms——该用户的增量授权列表。
func (h *Handler) ListUserPerms(c *gin.Context) {
	userID := c.Param("id")
	rows, err := h.Store.Pool.Query(c.Request.Context(),
		`SELECT perm FROM user_permissions WHERE user_id = $1 AND granted = true ORDER BY perm`, userID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	perms := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			perms = append(perms, p)
		}
	}
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "perms": perms})
}

// PutUserPerm PUT /admin/users/:id/perms {perm, granted}——增量授予/收回。
// 提权守卫：只能授予调用者自己拥有的权限（PermCovered 展开通配比较）。
func (h *Handler) PutUserPerm(c *gin.Context) {
	targetID := c.Param("id")
	var req struct {
		Perm    string `json:"perm"`
		Granted bool   `json:"granted"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Perm) == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体须含 perm/granted")
		return
	}
	// 目标用户须存在
	var exists bool
	if err := h.Store.Pool.QueryRow(c.Request.Context(),
		`SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, targetID).Scan(&exists); err != nil || !exists {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "用户不存在")
		return
	}
	if req.Granted {
		callerPerms := h.callerPermSet(c)
		if !PermCovered(callerPerms, req.Perm) {
			errResp(c, http.StatusForbidden, "FORBIDDEN", "只能授予自己已拥有的权限")
			return
		}
		_, err := h.Store.Pool.Exec(c.Request.Context(),
			`INSERT INTO user_permissions (user_id, perm, granted, granted_by) VALUES ($1, $2, true, $3)
			 ON CONFLICT (user_id, perm) DO UPDATE SET granted = true, granted_by = EXCLUDED.granted_by`,
			targetID, req.Perm, c.GetString("user_id"))
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "授权失败", err)
			return
		}
	} else {
		_, err := h.Store.Pool.Exec(c.Request.Context(),
			`DELETE FROM user_permissions WHERE user_id = $1 AND perm = $2`, targetID, req.Perm)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "收回失败", err)
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"user_id": targetID, "perm": req.Perm, "granted": req.Granted})
}

// callerPermSet 调用者权限集（与 CreateRole 提权守卫同口径：owner/admin 全量，
// 其余按 role_permissions 原始集——PermCovered 内部做通配展开比较）。
func (h *Handler) callerPermSet(c *gin.Context) []string {
	role := c.GetString("role")
	if role == "owner" || role == "admin" {
		return KnownPerms()
	}
	perms, err := h.Store.PermsOfRole(c.Request.Context(), role)
	if err != nil {
		return nil
	}
	return perms
}

// HasPermUser 用户级增量授权判定（直查无缓存——写后立即可生效；低频管理路径）。
func (s *Store) HasPermUser(ctx context.Context, userID, perm string) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM user_permissions
		 WHERE user_id = $1 AND granted = true
		   AND (perm = $2 OR perm = split_part($2, ':', 1) || ':*'))`,
		userID, perm).Scan(&ok)
	return ok, err
}
