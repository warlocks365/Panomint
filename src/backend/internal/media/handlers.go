package media

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/queue"
)

// Handler 媒体端点。
type Handler struct {
	Store     *Store
	Q         *queue.Queue // 缩略图任务队列（"media"，indexctl worker 消费）
	UploadDir string       // 上传文件存储根目录（./data/media）
	UploadTmp string       // 分块上传临时目录（./data/uploads）
	MediaRoot string       // 既有索引媒体根目录（media.path 相对它解析）
}

// List GET /media（API v1.1 §3：时间轴分页 + 筛选 + 时间桶）。
func (h *Handler) List(c *gin.Context) {
	p := ListParams{
		Space:     c.Query("space"),
		View:      c.DefaultQuery("view", "all"),
		Date:      c.Query("date"),
		Type:      c.Query("type"),
		Favorites: c.Query("favorites") == "true",
		Tag:       c.Query("tag"),
		Person:    c.Query("person"),
		Place:     c.Query("place"),
		Folder:    c.Query("folder"),
		Cursor:    c.Query("cursor"),
	}
	// owner 过滤仅在 space=personal 时生效（共享空间可见性模型待 Phase 3 双空间任务落地，
	// 当前不过滤 = 本人 + 未来共享媒体并集，决策已记录）
	if p.Space == "personal" {
		p.OwnerID = c.GetString("user_id")
	}
	if v := c.Query("limit"); v != "" {
		p.Limit, _ = strconv.Atoi(v)
	}
	res, err := h.Store.List(c.Request.Context(), p)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, res)
}
