package media

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
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
	// Audit 审计写入器；可为 nil（测试/灰度时静默跳过，见 record）。
	Audit *audit.Recorder
}

// record 写一条审计（**尽力而为**）。Audit 为 nil 时静默跳过。
//
// 与 auth / geo 的同名方法是同一形状，三个坑在这里同样成立：
//
//  1. **公开端点要显式传 actor**。`/media/**` 全部挂在 AuthRequired 之后，
//     所以这里从上下文取 `user_id` 是可靠的；将来若要给某个公开媒体端点写审计，
//     必须改成 auth 那样的 recordAs(c, actor, ...) —— 否则 FromGin 取到空串、
//     落库成 NULL，且**不报错**。
//  2. **detail 的键名要避开 audit.RedactDetail 的敏感子串**
//     （password/passwd/pwd/secret/credential/apikey/api_key/private_key/signature/
//     hash/token/jwt/bearer/session/cookie/authorization/otp/totp/mfa）。
//     命中就**整键剔除**，detail 会静默变成 `{}`。下方各调用点只用中性键名。
//  3. **不要为了"清理"删审计行**。本包只写不删；验证用的临时行也留在表里，
//     基线一律用增量 c0→c1（审计表是只追加的，删行本身就是最该被审计的行为）。
func (h *Handler) record(c *gin.Context, action, targetType, targetID string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = action
	e.TargetType = targetType
	e.TargetID = targetID
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
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
