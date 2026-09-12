package media

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/tags"
)

// 手工标签 HTTP 处理器（Job000005）。读 media:read；写 media:write + 媒体归属校验。

// ListTags GET /tags?q=（自动补全：子串过滤 + 使用计数）
func (h *Handler) ListTags(c *gin.Context) {
	tags, err := h.Store.ListTags(c.Request.Context(), c.Query("q"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"tags": tags})
}

// AddTag POST /media/:id/tags {name}（幂等：不存在则以 kind=user 创建，已关联不报错）
func (h *Handler) AddTag(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"name\": \"标签名\"}")
		return
	}
	name, err := NormalizeTagName(req.Name)
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	tag, err := h.Store.FindOrCreateUserTag(c.Request.Context(), name)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "TAG_CREATE_FAILED", err.Error())
		return
	}
	if err := h.Store.AttachTag(c.Request.Context(), id, tag.ID); err != nil {
		errResp(c, http.StatusInternalServerError, "TAG_ATTACH_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tag": tag})
}

// RemoveTag DELETE /media/:id/tags/:tag_id（解除关联；未关联按幂等 200 处理，标签不存在 404）
func (h *Handler) RemoveTag(c *gin.Context) {
	id := c.Param("id")
	tagID := c.Param("tag_id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	exists, err := h.Store.tagExists(c.Request.Context(), tagID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if !exists {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	removed, err := h.Store.DetachTag(c.Request.Context(), id, tagID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tag_id": tagID, "removed": removed})
}

// ---- Phase 4：标签管理端点 ----
// 权限沿用 media:read / media:write（种子权限中无独立 tag:*，与既有手工标签一致）。

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// normalizeColor 校验 #RRGGBB；空串视为「清除」（返回 nil）。
func normalizeColor(s string) (*string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if !colorRe.MatchString(s) {
		return nil, errors.New("颜色需为 #RRGGBB")
	}
	return &s, nil
}

// CreateTag POST /tags {name, kind?, color?}：显式创建标签。
func (h *Handler) CreateTag(c *gin.Context) {
	var req struct {
		Name  string `json:"name"`
		Kind  string `json:"kind"`
		Color string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"name\":\"...\"}")
		return
	}
	name, err := NormalizeTagName(req.Name)
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		kind = "user"
	}
	if kind != "user" && kind != "ai" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "kind 只能为 user 或 ai")
		return
	}
	color, err := normalizeColor(req.Color)
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	tag, err := h.Store.CreateTag(c.Request.Context(), name, kind, color)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "TAG_CREATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag": tag})
}

// PatchTag PATCH /tags/:id {name?, color?}：改名/改色。
func (h *Handler) PatchTag(c *gin.Context) {
	var req struct {
		Name  *string `json:"name"`
		Color *string `json:"color"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需含 name 或 color")
		return
	}
	if req.Name == nil && req.Color == nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "至少提供 name 或 color")
		return
	}
	var name *string
	if req.Name != nil {
		n, err := NormalizeTagName(*req.Name)
		if err != nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		name = &n
	}
	var color *string
	if req.Color != nil {
		col, err := normalizeColor(*req.Color)
		if err != nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
			return
		}
		color = col
	}
	tag, err := h.Store.UpdateTag(c.Request.Context(), c.Param("id"), name, color)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag": tag})
}

// DeleteTag DELETE /tags/:id[?into=<tag_id>]：删除；带 into 时先合并关联再删。
func (h *Handler) DeleteTag(c *gin.Context) {
	id := c.Param("id")
	into := strings.TrimSpace(c.Query("into"))
	moved, err := h.Store.DeleteTagOrMerge(c.Request.Context(), id, into)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusBadRequest, "DELETE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "merged_into": into, "moved": moved})
}

// ConfirmTag POST /tags/:id/confirm {media_id?, confirmed?}
// media_id 存在 → 确认该媒体上的这一关联；否则确认标签本身（粗粒度审阅）。
func (h *Handler) ConfirmTag(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		MediaID   string `json:"media_id"`
		Confirmed *bool  `json:"confirmed"`
	}
	_ = c.ShouldBindJSON(&req)
	confirmed := true
	if req.Confirmed != nil {
		confirmed = *req.Confirmed
	}
	if req.MediaID != "" {
		if _, ok := h.checkAccess(c, req.MediaID); !ok {
			return
		}
		ok, err := h.Store.ConfirmTagForMedia(c.Request.Context(), req.MediaID, id)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"media_id": req.MediaID, "tag_id": id, "confirmed": confirmed, "updated": ok})
		return
	}
	ok, err := h.Store.MarkTagReviewed(c.Request.Context(), id)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag_id": id, "confirmed": true, "updated": ok})
}

// ConfirmMediaTags POST /media/:id/tags/confirm {tag_ids?:[]}：逐图批量接受（缺 tag_ids 即全部）。
func (h *Handler) ConfirmMediaTags(c *gin.Context) {
	mediaID := c.Param("id")
	if _, ok := h.checkAccess(c, mediaID); !ok {
		return
	}
	var req struct {
		TagIDs []string `json:"tag_ids"`
	}
	_ = c.ShouldBindJSON(&req)
	ctx := c.Request.Context()
	if len(req.TagIDs) == 0 {
		n, err := h.Store.ConfirmAllForMedia(ctx, mediaID)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "confirmed": n})
		return
	}
	n := 0
	for _, tid := range req.TagIDs {
		ok, err := h.Store.ConfirmTagForMedia(ctx, mediaID, tid)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		if ok {
			n++
		}
	}
	c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "confirmed": n})
}

// ListTagMedia GET /tags/:id/media?cursor=&limit=：按标签浏览（仅已确认关联）。
func (h *Handler) ListTagMedia(c *gin.Context) {
	tagID := c.Param("id")
	limit := 0
	if v := c.Query("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	res, err := h.Store.ListMediaByTag(c.Request.Context(), tagID, c.Query("cursor"), limit)
	if err != nil {
		errResp(c, http.StatusBadRequest, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// AITagsPreview GET /ai/tags?media_id=：AI 建议预览（Tagger 可用则实时计算，不落库；
// 否则仅返回已落库的待确认结果）。
func (h *Handler) AITagsPreview(c *gin.Context) {
	mediaID := strings.TrimSpace(c.Query("media_id"))
	if mediaID == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 media_id")
		return
	}
	if _, ok := h.checkAccess(c, mediaID); !ok {
		return
	}
	ctx := c.Request.Context()
	ts := &tags.Store{Pool: h.Store.Pool}

	live := []gin.H{}
	if h.Tagger != nil {
		vec, err := ts.LoadEmbedding(ctx, mediaID)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
			return
		}
		for _, s := range h.Tagger.Suggest(vec) {
			live = append(live, gin.H{"tag": s.Tag, "class": s.Class, "confidence": s.Confidence})
		}
	}
	pending, err := ts.ListPendingAITags(ctx, mediaID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "live": live, "pending": pending})
}

// AITagsTrigger POST /ai/tags {scope, media_id?, limit?}：手动触发自动打标（有界同步执行）。
// 常驻增量由 `taggen -mode watch` 承担，此处仅为手动 nudge，不接队列（避免多 kind 互吞）。
func (h *Handler) AITagsTrigger(c *gin.Context) {
	if h.Tagger == nil {
		errResp(c, http.StatusServiceUnavailable, "TAGGER_UNAVAILABLE", "AI 打标未启用（CLIP 不可用）")
		return
	}
	var req struct {
		Scope   string `json:"scope"`
		MediaID string `json:"media_id"`
		Limit   int    `json:"limit"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"scope\":\"all|media_id\"}")
		return
	}
	ctx := c.Request.Context()
	ts := &tags.Store{Pool: h.Store.Pool}
	app := &tags.Applier{Store: ts, Clf: h.Tagger}

	if req.Scope == "media_id" {
		if req.MediaID == "" {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "scope=media_id 需提供 media_id")
			return
		}
		if _, ok := h.checkAccess(c, req.MediaID); !ok {
			return
		}
		n, err := app.ApplyByID(ctx, req.MediaID)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "TAG_FAILED", err.Error())
			return
		}
		c.JSON(http.StatusOK, gin.H{"processed": 1, "tagged": n})
		return
	}
	list, err := ts.ListPendingAI(ctx, req.Limit, "")
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	processed, tagged, failed := 0, 0, 0
	for _, m := range list {
		vec, err := ts.LoadEmbedding(ctx, m.ID)
		if err != nil {
			failed++
			continue
		}
		n, err := app.ApplyOne(ctx, m, vec)
		if err != nil {
			failed++
			continue
		}
		processed++
		tagged += n
	}
	c.JSON(http.StatusOK, gin.H{"processed": processed, "tagged": tagged, "failed": failed})
}
