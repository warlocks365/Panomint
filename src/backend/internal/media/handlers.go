package media

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/queue"
	"panoalbum/internal/tags"
)

// Handler 媒体端点。
type Handler struct {
	Store     *Store
	Q         *queue.Queue // 缩略图任务队列（"media"，indexctl worker 消费）
	UploadDir string       // 上传文件存储根目录（./data/media）
	UploadTmp string       // 分块上传临时目录（./data/uploads）
	MediaRoot string       // 既有索引媒体根目录（media.path 相对它解析）
	// Tagger 可空：AI 零样本分类器（Phase 4）。未接线时 /ai/tags 预览仅返回已落库结果。
	Tagger *tags.Classifier
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

// Duplicates GET /media/duplicates?threshold=10&limit=50&space=（PRD §6.16 工具箱：重复项目）。
//
// 作用域与 List 完全一致：owner 过滤仅在 space=personal 时生效（见上方 List 的说明，
// 谓词本体在 duplicateUniverseWhere，与 timeline.go 的 buildWhere 逐字对应）。
// 阈值/limit 的解析与钳制见 ParseDuplicateParams；候选超 5000 拒绝而非挂起，见 MaxPhashUniverse。
func (h *Handler) Duplicates(c *gin.Context) {
	threshold, limit, err := ParseDuplicateParams(c.Query("threshold"), c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	p := DuplicateParams{Space: c.Query("space"), Threshold: threshold, Limit: limit}
	if p.Space == "personal" {
		p.OwnerID = c.GetString("user_id")
	}
	res, err := h.Store.FindDuplicates(c.Request.Context(), p)
	var tooMany *TooManyMediaError
	if errors.As(err, &tooMany) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "TOO_MANY_MEDIA", "message": tooMany.Error()}})
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// DateHistogram GET /media/date-histogram?granularity=year|month（Job000005，默认 month）。
// 权限过滤与 List 一致（space=personal 限本人）；taken_at NULL 归 unknown 桶。
func (h *Handler) DateHistogram(c *gin.Context) {
	granularity := c.DefaultQuery("granularity", "month")
	if granularity != "year" && granularity != "month" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": ErrInvalidGranularity.Error()}})
		return
	}
	space := c.Query("space")
	ownerID := ""
	if space == "personal" {
		ownerID = c.GetString("user_id")
	}
	buckets, err := h.Store.DateHistogram(c.Request.Context(), ownerID, space, granularity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, buckets)
}
