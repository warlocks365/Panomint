package storage

// Job000103 存储位置（命名物理存储根）管理端点。
// 语义裁决：位置名=容器内挂载目录名——Docker 映射名约束 slug 化（小写字母/数字/连字符，
// 禁止点号斜杠空格，防路径穿越与非法挂载名）；非法名在 HTTP 层前置 400（与表 CHECK 双保险）。
// media.location_id 可空：NULL=默认存储根（存量行为不变）；删除位置时媒体回默认（SET NULL）。
// 权限沿用挂载管理口径：仅系统管理员（requirePrivileged），非管理员 403。

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"panoalbum/internal/httperr"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation pg 唯一约束冲突判定（23505；沿 auth/store.go 口径）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// LocationNameRe Docker 映射名约束（与 00034 CHECK 同口径双保险）。
var LocationNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// Location 存储位置（API 输出形态；media_count=引用计数，>0 禁删）。
type Location struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MediaCount  int    `json:"media_count"`
	CreatedAt   string `json:"created_at"`
}

// CreateLocation POST /storage/locations {name,description?} → 201。
func (h *Handler) CreateLocation(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if !LocationNameRe.MatchString(req.Name) {
		httperr.Abort(c, http.StatusBadRequest, "INVALID_NAME",
			"位置名须为 1-32 位小写字母/数字/连字符（Docker 映射名约束），且以字母开头")
		return
	}
	if len(req.Description) > 200 {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "description ≤200 字符")
		return
	}
	var id string
	err := h.Pool.QueryRow(c.Request.Context(),
		`INSERT INTO storage_locations (name, description) VALUES ($1,$2) RETURNING id::text`,
		req.Name, req.Description).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			httperr.Abort(c, http.StatusConflict, "DUPLICATE_NAME", "同名存储位置已存在")
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// ListLocations GET /storage/locations → {locations:[…]}（按 name 升序，含引用计数）。
func (h *Handler) ListLocations(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	rows, err := h.Pool.Query(c.Request.Context(),
		`SELECT l.id::text, l.name, l.description, l.created_at::text,
		        (SELECT COUNT(*)::int FROM media m WHERE m.location_id = l.id)
		 FROM storage_locations l ORDER BY l.name ASC`)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	out := []Location{}
	for rows.Next() {
		var loc Location
		if err := rows.Scan(&loc.ID, &loc.Name, &loc.Description, &loc.CreatedAt, &loc.MediaCount); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		out = append(out, loc)
	}
	c.JSON(http.StatusOK, gin.H{"locations": out})
}

// PatchLocation PATCH /storage/locations/:id {description?} → 204。
// name 禁改（引用稳定=挂载目录稳定）：请求带 name 即 400 NAME_IMMUTABLE。
func (h *Handler) PatchLocation(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	if req.Name != nil && *req.Name != "" {
		httperr.Abort(c, http.StatusBadRequest, "NAME_IMMUTABLE", "位置名即挂载目录名，创建后不可修改（引用稳定约束）")
		return
	}
	if req.Description == nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "仅支持修改 description")
		return
	}
	if len(*req.Description) > 200 {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "description ≤200 字符")
		return
	}
	tag, err := h.Pool.Exec(c.Request.Context(),
		`UPDATE storage_locations SET description=$2, updated_at=now() WHERE id=$1`, id, *req.Description)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "存储位置不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteLocation DELETE /storage/locations/:id → 204。
// 有媒体引用时 409 LOCATION_IN_USE（媒体须先迁回默认存储）；无引用物理删。
func (h *Handler) DeleteLocation(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	var n int
	if err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT COUNT(*)::int FROM media WHERE location_id=$1`, id).Scan(&n); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if n > 0 {
		httperr.Abort(c, http.StatusConflict, "LOCATION_IN_USE",
			fmt.Sprintf("该位置仍有 %d 项媒体引用，须先迁回默认存储", n))
		return
	}
	tag, err := h.Pool.Exec(c.Request.Context(), `DELETE FROM storage_locations WHERE id=$1`, id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "存储位置不存在")
		return
	}
	c.Status(http.StatusNoContent)
}
