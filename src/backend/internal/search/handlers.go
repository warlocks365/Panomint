package search

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
)

// Handler 搜索端点（Stage 3 / Job000001）。
type Handler struct {
	Store *Store
}

// Search GET /search（API v1.1 §10 结构化子集）。
func (h *Handler) Search(c *gin.Context) {
	p, err := ParseParams(c.Query, c.GetString("user_id"))
	if err != nil {
		code := "INVALID_PARAMS"
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	res, err := h.Store.Search(c.Request.Context(), p)
	if err != nil {
		// 可区分的语义必须保留：非法游标是本端构造、面向用户的文案（400/INVALID_CURSOR），
		// 原样回；其余是 DB 故障（500/QUERY_FAILED），message 固定、完整错误只进服务端日志。
		if errors.Is(err, ErrInvalidCursor) {
			httperr.Envelope(c, http.StatusBadRequest, "INVALID_CURSOR", err.Error())
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, res)
}
