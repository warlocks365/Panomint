package shares

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/auth"
	"panoalbum/internal/httperr"
)

// Handler 分享端点（管理端点走 authed + share:create；公开端点 token 即凭证）。
type Handler struct {
	Store  *Store
	HLSDir string // HLS 输出根目录（./data/hls）
	// Audit 审计写入器；可为 nil（测试/灰度时静默跳过，见 record）。
	Audit *audit.Recorder
}

// record 写一条审计（**尽力而为**）。Audit 为 nil 时静默跳过。
//
// 三个坑与 media / auth / geo 的同名方法一致：公开端点要显式传 actor（本包的公开
// 端点 token 即凭证、无 user_id，**不要**给它们写这里的 action）、detail 键名要避开
// audit.RedactDetail 的敏感子串、不要为了清理删审计行。
//
// ⚠️ 本包特有的红线：**share token 绝不能进 detail**。它本身就是访问凭证
// （公开端点无鉴权，token 即凭证），写进永久保留的审计表等于复制一份凭证；
// 而且键名含 "token" 会被 RedactDetail 整键剔除、静默变成 {}。detail 只记非敏感元信息。
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

// errResp 统一错误格式 {"error":{"code","message"}}。
// 形状委托给 internal/httperr（单一真源）。
func errResp(c *gin.Context, status int, code, msg string) {
	httperr.Envelope(c, status, code, msg)
}

// Create POST /shares {kind, target_id, title?, expire_at?, password?, is_wechat?, max_views?} → 201 {id, token, url}
// is_wechat=true 时强制 allow_download=false（微信 H5 不给原文件，PRD 核心约束）。
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Kind          string     `json:"kind"`
		TargetID      string     `json:"target_id"`
		Title         *string    `json:"title"`
		ExpireAt      *time.Time `json:"expire_at"`
		Password      *string    `json:"password"`
		IsWechat      bool       `json:"is_wechat"`
		AllowDownload bool       `json:"allow_download"`
		MaxViews      *int       `json:"max_views"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	if req.Kind != "album" && req.Kind != "media" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "kind 仅支持 album|media")
		return
	}
	if strings.TrimSpace(req.TargetID) == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "target_id 不能为空")
		return
	}
	if req.ExpireAt != nil && !req.ExpireAt.After(time.Now()) {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "expire_at 必须是未来时间")
		return
	}
	if req.MaxViews != nil && *req.MaxViews <= 0 {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "max_views 必须为正整数")
		return
	}

	ctx := c.Request.Context()
	userID := c.GetString("user_id")
	// 目标归属：本人资源才可分享（owner/admin 角色可代管）
	owned, err := h.Store.TargetOwnedBy(ctx, req.Kind, req.TargetID, userID)
	if errors.Is(err, ErrTargetLost) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "分享目标不存在")
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	role := c.GetString("role")
	if !owned && role != "owner" && role != "admin" {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅可分享本人资源")
		return
	}

	// 密码 bcrypt（空则 null）
	var pwdHash *string
	if req.Password != nil && *req.Password != "" {
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "HASH_FAILED", "密码加密失败", err)
			return
		}
		pwdHash = &hash
	}

	allowDownload := req.AllowDownload
	if req.IsWechat {
		allowDownload = false // 微信 H5 强制禁原文件
	}

	id, token, err := h.Store.Create(ctx, userID, CreateInput{
		Kind:          req.Kind,
		TargetID:      req.TargetID,
		Title:         req.Title,
		ExpireAt:      req.ExpireAt,
		PasswordHash:  pwdHash,
		AllowDownload: allowDownload,
		IsWechat:      req.IsWechat,
		MaxViews:      req.MaxViews,
	})
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	// 审计在创建成功之后。target 是**分享本身**（share/<id>）；被分享的相册/媒体 id
	// 放在 detail 的 shared_id 里 —— 两者刻意不混用，否则"查这个分享被做了哪些操作"
	// 与"查这个媒体被分享过几次"两个问句会互相污染。
	//
	// detail 里**没有 token**（它是访问凭证，且键名会命中脱敏名单被整键剔除）；
	// passcode 用中性键名，不叫 has_password —— "password" 是脱敏子串，撞上就整键消失。
	h.record(c, audit.ActionShareCreate, audit.TargetShare, id, map[string]any{
		"kind":           req.Kind,
		"shared_id":      req.TargetID,
		"wechat":         req.IsWechat,
		"allow_download": allowDownload,
		"passcode":       pwdHash != nil,
		"owner_id":       userID,
	})
	c.JSON(http.StatusCreated, gin.H{"id": id, "token": token, "url": shareURL(c, token)})
}

// shareURL 拼装公开访问 URL（尊重反向代理 X-Forwarded-Proto；originOf 见 og.go）。
func shareURL(c *gin.Context, token string) string {
	return fmt.Sprintf("%s/public/shares/%s", originOf(c), token)
}

// List GET /shares → 我的分享列表（含 access_count/状态）。
func (h *Handler) List(c *gin.Context) {
	shares, err := h.Store.ListByOwner(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	now := time.Now()
	type item struct {
		Share
		Status string `json:"status"`
		URL    string `json:"url"`
	}
	out := make([]item, 0, len(shares))
	for _, sh := range shares {
		out = append(out, item{Share: sh, Status: sh.Status(now), URL: shareURL(c, sh.Token)})
	}
	c.JSON(http.StatusOK, gin.H{"shares": out})
}

// Delete DELETE /shares/:id → 吊销（删除行；仅本人或 owner/admin）。
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	sh, err := h.Store.GetByID(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "分享不存在")
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	role := c.GetString("role")
	if c.GetString("user_id") != sh.OwnerID && role != "owner" && role != "admin" {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅分享创建者或管理员可吊销")
		return
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	// 审计在吊销成功之后（失败不写）。
	// 吊销 = 删除 share_links 行，此后 target_id 查不回任何东西，所以 detail 要带上
	// 能说明"吊销了谁的什么分享"的非敏感字段；**token 一律不记**（凭证 + 会被脱敏剔除）。
	h.record(c, audit.ActionShareRevoke, audit.TargetShare, id, map[string]any{
		"kind":      sh.Kind,
		"shared_id": sh.TargetID,
		"owner_id":  sh.OwnerID,
	})
	c.Status(http.StatusNoContent)
}

// guardPublic 公开端点公共校验：token 存在 → 未过期/未超量/密码正确。
// 通过返回 *Share；否则已写错误响应并返回 nil。
func (h *Handler) guardPublic(c *gin.Context) *Share {
	sh, err := h.Store.GetByToken(c.Request.Context(), c.Param("token"))
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "分享不存在或已吊销")
		return nil
	}
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return nil
	}
	switch code := checkAccess(sh, c.Query("password"), time.Now()); code {
	case "":
		return sh
	case "EXPIRED":
		errResp(c, http.StatusForbidden, "EXPIRED", "分享已过期")
	case "MAX_VIEWS":
		errResp(c, http.StatusForbidden, "MAX_VIEWS", "分享已达最大访问次数")
	case "PASSWORD_REQUIRED":
		errResp(c, http.StatusForbidden, "PASSWORD_REQUIRED", "该分享需要密码（?password=）")
	default: // WRONG_PASSWORD
		errResp(c, http.StatusForbidden, "WRONG_PASSWORD", "访问密码错误")
	}
	return nil
}

// PublicGet GET /public/shares/:token → {kind, title, items, require_password}
// 成功访问写 share_access_log（ip/ua）+ access_count 原子自增。
func (h *Handler) PublicGet(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	ctx := c.Request.Context()
	if err := h.Store.RecordAccess(ctx, sh.ID, c.ClientIP(), c.Request.UserAgent()); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "LOG_FAILED", "记录访问失败", err)
		return
	}
	items, err := h.Store.ListItems(ctx, sh)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"kind":             sh.Kind,
		"title":            sh.Title,
		"items":            items,
		"require_password": sh.PasswordHash != nil && *sh.PasswordHash != "",
	})
}

// PublicThumb GET /public/shares/:token/media/:id/thumb?size=sm|md|lg
// 仅当该媒体属于此分享目标；同做有效期/密码校验。
func (h *Handler) PublicThumb(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	ctx := c.Request.Context()
	mediaID := c.Param("id")
	ok, err := h.Store.MediaInShare(ctx, sh, mediaID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !ok {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "该媒体不属于此分享")
		return
	}
	name, err := h.Store.ThumbFile(ctx, mediaID, c.DefaultQuery("size", "md"))
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_SIZE", "size 仅支持 sm|md|lg")
		return
	}
	if name == nil || *name == "" {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "缩略图不存在")
		return
	}
	// 防路径穿越（与 internal/media/thumb.go 同模式）
	base := filepath.Base(*name)
	dir := os.Getenv("THUMB_DIR")
	if dir == "" {
		dir = "./testdata/thumbnails"
	}
	path := filepath.Join(dir, base)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)) {
		errResp(c, http.StatusBadRequest, "BAD_PATH", "非法路径")
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(path)
}

// PublicHLS GET /public/shares/:token/media/:id/hls/*file
// HLS 流代理：同校验；仅当 media.hls_master 存在，读 HLSDir/<id>/ 下文件。
func (h *Handler) PublicHLS(c *gin.Context) {
	sh := h.guardPublic(c)
	if sh == nil {
		return
	}
	ctx := c.Request.Context()
	mediaID := c.Param("id")
	ok, err := h.Store.MediaInShare(ctx, sh, mediaID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !ok {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "该媒体不属于此分享")
		return
	}
	has, err := h.Store.HasHLS(ctx, mediaID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !has {
		errResp(c, http.StatusNotFound, "NO_HLS", "该媒体暂无 HLS 流")
		return
	}
	// 清理相对路径，禁止穿越（与 internal/transcode ServeHLS 同模式）
	rel := filepath.Clean(strings.TrimPrefix(c.Param("file"), "/"))
	if rel == "." || strings.HasPrefix(rel, "..") {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "非法路径")
		return
	}
	base, _ := filepath.Abs(filepath.Join(h.HLSDir, mediaID))
	full, _ := filepath.Abs(filepath.Join(base, rel))
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "非法路径")
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "HLS 文件不存在")
		return
	}
	switch strings.ToLower(filepath.Ext(full)) {
	case ".m3u8":
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-cache")
	case ".ts":
		c.Header("Content-Type", "video/mp2t")
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	}
	c.File(full)
}

// PublicDownload 原文件下载占位：公开路由禁止提供原文件（PRD 核心约束）。
// allow_download=true 的分享本期也不开放，统一 403，P1 再实现签名下载。
func (h *Handler) PublicDownload(c *gin.Context) {
	errResp(c, http.StatusForbidden, "NOT_SUPPORTED", "公开分享暂不支持原文件下载")
}
