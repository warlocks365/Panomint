package media

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
	"panoalbum/internal/pgxutil"
	"panoalbum/internal/tags"
)

// 手工标签 HTTP 处理器（Job000005）。读 media:read；写 media:write + 媒体归属校验。

// parseTagListQuery 校验 GET /tags 的查询参数。
// kind 只允许空串（不过滤）/"user"/"ai"；limit 留空或为 0 表示用默认值，需为正整数（负数/非数字 → 错误）。
func parseTagListQuery(q, kind, limit string) (string, string, int, error) {
	kind = strings.TrimSpace(kind)
	if kind != "" && kind != "user" && kind != "ai" {
		return "", "", 0, errors.New("kind 只能为 user 或 ai")
	}
	n := 0
	if v := strings.TrimSpace(limit); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			return "", "", 0, errors.New("limit 需为非负整数")
		}
		n = parsed
	}
	return q, kind, n, nil
}

// ListTags GET /tags?q=&kind=&limit=（自动补全：子串过滤 + 使用计数；手工标签优先）。
// 历史行为：硬编码 LIMIT 100 且按 usage_count 排序，会把使用次数低的手工标签整批截断/挤出，
// 现改为可调 limit（缺省 500）+ kind 过滤 + 「手工标签优先」排序。
func (h *Handler) ListTags(c *gin.Context) {
	q, kind, limit, err := parseTagListQuery(c.Query("q"), c.Query("kind"), c.Query("limit"))
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	tags, err := h.Store.ListTags(c.Request.Context(), q, kind, limit)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
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
		httperr.Fail(c, http.StatusInternalServerError, "TAG_CREATE_FAILED", "创建标签失败", err)
		return
	}
	if err := h.Store.AttachTag(c.Request.Context(), id, tag.ID); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "TAG_ATTACH_FAILED", "添加标签失败", err)
		return
	}
	// 回查真实值再返回：FindOrCreateUserTag 只取 id/name/kind/color，
	// Confirmed 会退化为 false（DDL 默认 true）、UsageCount 恒为 0，
	// 与刚建立的关联（confirmed=true、origin=user、usage_count>=1）不一致。
	real, err := h.Store.GetTag(c.Request.Context(), tag.ID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "tag": real})
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
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !exists {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	removed, err := h.Store.DetachTag(c.Request.Context(), id, tagID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
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
		httperr.Fail(c, http.StatusInternalServerError, "TAG_CREATE_FAILED", "创建标签失败", err)
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
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag": tag})
}

// DeleteTag DELETE /tags/:id[?into=<tag_id>]：删除；带 into 时先合并关联再删。
func (h *Handler) DeleteTag(c *gin.Context) {
	id := c.Param("id")
	into := strings.TrimSpace(c.Query("into"))
	moved, err := h.Store.DeleteTagOrMerge(c.Request.Context(), id, into)
	if errors.Is(err, ErrMergeTargetNotFound) {
		// 提前校验而非等 PG 外键报错：响应里不能出现 SQLSTATE 23503 之类原文。
		errResp(c, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "DELETE_FAILED", "删除失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "merged_into": into, "moved": moved})
}

// ConfirmTag POST /tags/:id/confirm {media_id?, confirmed?}
// media_id 存在 → 设置该媒体上这一关联的确认状态（media_tags.confirmed）；
// 否则设置标签本身（tags.confirmed，粗粒度审阅）。两条路径都支持 confirmed=false 取消确认。
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
	// 标签不存在一律 404（与 DELETE /tags/:id 语义一致），不再静默 200 空更新。
	exists, err := h.Store.tagExists(c.Request.Context(), id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !exists {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在")
		return
	}
	if req.MediaID != "" {
		if _, ok := h.checkAccess(c, req.MediaID); !ok {
			return
		}
		// confirmed 真正落库到 media_tags.confirmed，响应回显即落库值（false 可取消确认）。
		ok, err := h.Store.ConfirmTagForMedia(c.Request.Context(), req.MediaID, id, confirmed)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"media_id": req.MediaID, "tag_id": id, "confirmed": confirmed, "updated": ok})
		return
	}
	ok, err := h.Store.SetTagReviewed(c.Request.Context(), id, confirmed)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tag_id": id, "confirmed": confirmed, "updated": ok})
}

// parseConfirmTagIDs 解析 POST /media/:id/tags/confirm 的请求体 {"tag_ids":[...]}。
//
// 返回 (nil, nil) 表示「未提供 tag_ids」（含空请求体/`{}`/`null`），调用方按"确认该媒体全部"处理；
// 返回非 nil 空切片表示「显式给了空数组」，调用方应按确认 0 条处理，不得退化为全部确认；
// 解析失败（非法 JSON、tag_ids 类型不是数组等）返回 error，调用方须回 400。
//
// 抽成纯函数是为了能在不触库的前提下覆盖这些分支（原实现用 `_ = c.ShouldBindJSON(&req)`
// 吞掉解析错误，tag_ids 传字符串时会静默退化成"确认全部 AI 标签"）。
func parseConfirmTagIDs(raw []byte) (*[]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, nil
	}
	var req struct {
		TagIDs *[]string `json:"tag_ids"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}
	return req.TagIDs, nil
}

// ConfirmMediaTags POST /media/:id/tags/confirm {tag_ids?:[]}：逐图批量接受。
// 仅「请求体为空/未提供 tag_ids」才走"全部确认"；显式空数组 = 确认 0 条；解析失败 400。
func (h *Handler) ConfirmMediaTags(c *gin.Context) {
	mediaID := c.Param("id")
	if _, ok := h.checkAccess(c, mediaID); !ok {
		return
	}
	raw, err := c.GetRawData()
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体读取失败")
		return
	}
	ids, err := parseConfirmTagIDs(raw)
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"tag_ids\":[\"...\"]}")
		return
	}
	ctx := c.Request.Context()
	if ids == nil {
		n, err := h.Store.ConfirmAllForMedia(ctx, mediaID)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "confirmed": n})
		return
	}
	n := 0
	for _, tid := range *ids {
		ok, err := h.Store.ConfirmTagForMedia(ctx, mediaID, tid, true)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
		if ok {
			n++
		}
	}
	c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "confirmed": n})
}

// tagNotFound 标签「不存在」的统一响应。
// 「标签真的不存在」与「:id 语法非法（PG 22P02）」必须共用本函数、逐字节同形：
// 两条分支对调用方是同一件事（拿不到这个标签），分开写就会变成一个探测判据。
func tagNotFound(c *gin.Context) { errResp(c, http.StatusNotFound, "NOT_FOUND", "标签不存在") }

// writeTagLookupError 把标签存在性查询的失败映射为响应。
// 畸形 id → 与「标签不存在」同形 404；其余 DB 故障 → 500，但只把完整错误写进服务端日志，
// 客户端拿固定文案（绝不回显 PG 原文：invalid input syntax for type uuid / SQLSTATE / 类型名）。
func writeTagLookupError(c *gin.Context, err error) {
	if pgxutil.IsMalformedID(err) {
		tagNotFound(c)
		return
	}
	log.Printf("[media] 标签存在性查询失败: %v", err)
	errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
}

// ListTagMedia GET /tags/:id/media?cursor=&limit=&space=：按标签浏览（仅已确认关联）。
// 标签不存在时 404，避免与「标签存在但无已确认媒体」返回的空列表混淆。
//
// 可见性：与 GET /media 同口径（ResolveMediaScope + scopeConds）：space 缺省 = 本人个人空间
// （**不是「全部」**），枚举外取值 400 INVALID_PARAMS。tags 表全局无 owner，故按
// **media 的可见性**收窄，而不是按标签归属（见 tags.go 的 buildTagMediaWhere）。
func (h *Handler) ListTagMedia(c *gin.Context) {
	scope, err := ResolveMediaScope(c.Query("space"), c.GetString("user_id"))
	if err != nil {
		rejectScope(c, err)
		return
	}
	tagID := c.Param("id")
	// tags.id 是 UUID 列：畸形 id 会让 PG 抛 22P02。与 !exists 共用 tagNotFound，
	// 既保证 404 而非 500，也保证响应与「标签不存在」逐字节同形。
	exists, err := h.Store.tagExists(c.Request.Context(), tagID)
	if err != nil {
		writeTagLookupError(c, err)
		return
	}
	if !exists {
		tagNotFound(c)
		return
	}
	limit := 0
	if v := c.Query("limit"); v != "" {
		limit, _ = strconv.Atoi(v)
	}
	res, err := h.Store.ListMediaByTag(c.Request.Context(), scope, tagID, c.Query("cursor"), limit)
	if err != nil {
		// 状态码与 code 保持既有形状（400/QUERY_FAILED），只把 message 从 err.Error()
		// 换成固定文案：游标解析失败等本地错误与 DB 故障的来源不同，但都不该回显内部原文。
		log.Printf("[media] 标签媒体列表查询失败 tag=%s: %v", tagID, err)
		errResp(c, http.StatusBadRequest, "QUERY_FAILED", "查询失败")
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
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		for _, s := range h.Tagger.Suggest(vec) {
			live = append(live, gin.H{"tag": s.Tag, "class": s.Class, "confidence": s.Confidence})
		}
	}
	pending, err := ts.ListPendingAITags(ctx, mediaID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"media_id": mediaID, "live": live, "pending": pending})
}

// AITagsTrigger POST /ai/tags {scope, media_id?, limit?}：手动触发自动打标（有界同步执行）。
// 常驻增量由 `taggen -mode watch` 承担，此处仅为手动 nudge，不接队列（避免多 kind 互吞）。
func (h *Handler) AITagsTrigger(c *gin.Context) {
	var req struct {
		Scope   string `json:"scope"`
		MediaID string `json:"media_id"`
		Limit   int    `json:"limit"`
	}
	// 入参校验先于 Tagger 可用性检查：非法/缺失 scope 一律 400，
	// 否则 `{}` 或未知 scope 会静默按 scope=all 执行全量打标。
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"scope\":\"all|media_id\"}")
		return
	}
	if req.Scope != "all" && req.Scope != "media_id" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "scope 需为 all 或 media_id")
		return
	}
	if h.Tagger == nil {
		errResp(c, http.StatusServiceUnavailable, "TAGGER_UNAVAILABLE", "AI 打标未启用（CLIP 不可用）")
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
			httperr.Fail(c, http.StatusInternalServerError, "TAG_FAILED", "打标失败", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"processed": 1, "tagged": n})
		return
	}
	list, err := ts.ListPendingAI(ctx, req.Limit, "")
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
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
