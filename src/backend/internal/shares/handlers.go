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

	"panoalbum/internal/auth"
)

// Handler 分享端点（管理端点走 authed + share:create；公开端点 token 即凭证）。
type Handler struct {
	Store  *Store
	HLSDir string // HLS 输出根目录（./data/hls）
}

// errResp 统一错误格式 {"error":{"code","message"}}。
func errResp(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// Create POST /shares {kind, target_id, title?, expire_at?, password?, is_wechat?, max_views?} → 201 {id, token, url}
// is_wechat=true 时强制 allow_download=false（微信 H5 不给原文件，PRD 核心约束）。
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Kind         string     `json:"kind"`
		TargetID     string     `json:"target_id"`
		Title        *string    `json:"title"`
		ExpireAt     *time.Time `json:"expire_at"`
		Password     *string    `json:"password"`
		IsWechat     bool       `json:"is_wechat"`
		AllowDownload bool      `json:"allow_download"`
		MaxViews     *int       `json:"max_views"`
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
			errResp(c, http.StatusInternalServerError, "HASH_FAILED", err.Error())
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
		errResp(c, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	role := c.GetString("role")
	if c.GetString("user_id") != sh.OwnerID && role != "owner" && role != "admin" {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅分享创建者或管理员可吊销")
		return
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
		errResp(c, http.StatusInternalServerError, "LOG_FAILED", err.Error())
		return
	}
	items, err := h.Store.ListItems(ctx, sh)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if !ok {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "该媒体不属于此分享")
		return
	}
	has, err := h.Store.HasHLS(ctx, mediaID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
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
