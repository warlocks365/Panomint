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
//
// 作用域：space 缺省 = 本人个人空间（**不是"全部"**）。解析规则与安全不变量见
// ResolveMediaScope / scope.go —— 这里只做"解析失败一律拒绝"，绝不静默放宽。
func (h *Handler) List(c *gin.Context) {
	scope, err := ResolveMediaScope(c.Query("space"), c.GetString("user_id"))
	if err != nil {
		rejectScope(c, err)
		return
	}
	p := ListParams{
		Scope:     scope,
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

// rejectScope 把作用域解析失败转成干净的错误封套。
//
// 非法 space 走 400 INVALID_PARAMS 而不是放行到 SQL：后者会返回
// `ERROR: invalid input value for enum media_space: "bogus" (SQLSTATE 22P02)`，
// 既把数据库内部结构透给调用方，又让客户端拿到 400/500 不一致的错误码。
// 身份缺失走 401：那是鉴权层的问题，不该被当成"参数写错了"。
func rejectScope(c *gin.Context, err error) {
	if errors.Is(err, ErrMissingUser) {
		errResp(c, http.StatusUnauthorized, "UNAUTHENTICATED", err.Error())
		return
	}
	errResp(c, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
}

// Duplicates GET /media/duplicates?threshold=10&limit=50&space=（PRD §6.16 工具箱：重复项目）。
//
// 作用域与 List 完全一致：同一个 ResolveMediaScope + 同一个 scopeConds 谓词
// （见 scope.go 的"唯一真源"说明）。这曾是最容易漏的一处——重复检测会把
// 他人媒体的 ID 与分组一并返回，故谓词必须共用而不是各写一份。
// 阈值/limit 的解析与钳制见 ParseDuplicateParams；候选超 5000 拒绝而非挂起，见 MaxPhashUniverse。
func (h *Handler) Duplicates(c *gin.Context) {
	threshold, limit, err := ParseDuplicateParams(c.Query("threshold"), c.Query("limit"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": err.Error()}})
		return
	}
	scope, err := ResolveMediaScope(c.Query("space"), c.GetString("user_id"))
	if err != nil {
		rejectScope(c, err)
		return
	}
	res, err := h.Store.FindDuplicates(c.Request.Context(), DuplicateParams{
		Scope: scope, Threshold: threshold, Limit: limit,
	})
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
// 作用域与 List 完全一致（space 缺省=本人；同一 scopeConds 谓词）；taken_at NULL 归 unknown 桶。
func (h *Handler) DateHistogram(c *gin.Context) {
	granularity := c.DefaultQuery("granularity", "month")
	if granularity != "year" && granularity != "month" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "INVALID_PARAMS", "message": ErrInvalidGranularity.Error()}})
		return
	}
	scope, err := ResolveMediaScope(c.Query("space"), c.GetString("user_id"))
	if err != nil {
		rejectScope(c, err)
		return
	}
	buckets, err := h.Store.DateHistogram(c.Request.Context(), scope, granularity)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, buckets)
}
