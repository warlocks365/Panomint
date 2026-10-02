// Package spaces 双空间模型端点（API v1.1 §4）。
package spaces

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/httperr"
)

// Handler 空间端点。
type Handler struct {
	Pool *pgxpool.Pool
}

// Personal 个人空间概览。
type Personal struct {
	Space      string `json:"space"`
	MediaCount int    `json:"media_count"`
	UsedBytes  int64  `json:"used_bytes"`
}

// SharedRef 共享空间条目。
type SharedRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// Get GET /spaces：{personal:{...}, shared:[...]}（shared 无数据时为空数组）。
func (h *Handler) Get(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	p := Personal{Space: "personal"}
	if err := h.Pool.QueryRow(ctx, `
		SELECT count(*)::int, COALESCE(sum(filesize),0)::bigint
		FROM media WHERE owner_id = $1 AND space = 'personal' AND deleted_at IS NULL`, userID).
		Scan(&p.MediaCount, &p.UsedBytes); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}

	shared := []SharedRef{}
	rows, err := h.Pool.Query(ctx, `
		SELECT s.id, s.name, m.role
		FROM shared_space s JOIN shared_space_members m ON m.space_id = s.id
		WHERE m.user_id = $1 ORDER BY s.created_at`, userID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var r SharedRef
		if err := rows.Scan(&r.ID, &r.Name, &r.Role); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		shared = append(shared, r)
	}
	if err := rows.Err(); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"personal": p, "shared": shared})
}
