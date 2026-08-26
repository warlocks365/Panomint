package search

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
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
		status := http.StatusInternalServerError
		code := "QUERY_FAILED"
		if errors.Is(err, ErrInvalidCursor) {
			status = http.StatusBadRequest
			code = "INVALID_CURSOR"
		}
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, res)
}
