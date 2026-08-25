package media

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Thumb GET /media/:id/thumb?size=sm|md|lg —— 缩略图服务（G2 缺口补齐）。
// 从 DB 读缩略图文件名，THUMB_DIR（默认 ./testdata/thumbnails）解析落盘文件。
func (h *Handler) Thumb(c *gin.Context) {
	size := c.DefaultQuery("size", "md")
	col := map[string]string{"sm": "thumbnail_sm", "md": "thumbnail_md", "lg": "thumbnail_lg"}[size]
	if col == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "BAD_SIZE", "message": "size 仅支持 sm|md|lg"}})
		return
	}
	var name *string
	if err := h.Store.Pool.QueryRow(c.Request.Context(),
		`SELECT `+col+` FROM media WHERE id = $1 AND deleted_at IS NULL`, c.Param("id")).Scan(&name); err != nil || name == nil || *name == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "缩略图不存在"}})
		return
	}
	// 防路径穿越
	base := filepath.Base(*name)
	dir := os.Getenv("THUMB_DIR")
	if dir == "" {
		dir = "./testdata/thumbnails"
	}
	path := filepath.Join(dir, base)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "BAD_PATH", "message": "非法路径"}})
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(path)
}
