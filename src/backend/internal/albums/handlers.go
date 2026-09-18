package albums

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/pgxutil"
)

// Handler 相册端点。
type Handler struct {
	Store *Store
}

// errResp 统一错误格式 {"error":{"code","message"}}。
func errResp(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// canManage 相册写操作权限：本人或 owner/admin 角色。
func canManage(c *gin.Context, ownerID string) bool {
	if c.GetString("user_id") == ownerID {
		return true
	}
	role := c.GetString("role")
	return role == "owner" || role == "admin"
}

// Create POST /albums {name, kind?, description?, cover_media_id?, criteria?} → 201 {id}
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Name         string    `json:"name"`
		Kind         string    `json:"kind"`
		Description  string    `json:"description"`
		CoverMediaID *string   `json:"cover_media_id"`
		Criteria     *Criteria `json:"criteria"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "name 不能为空")
		return
	}
	if req.Kind == "" {
		req.Kind = "normal"
	}
	if req.Kind != "normal" && req.Kind != "smart" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "kind 仅支持 normal|smart")
		return
	}
	id, err := h.Store.Create(c.Request.Context(), c.GetString("user_id"),
		req.Name, req.Description, req.Kind, req.CoverMediaID, req.Criteria)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// List GET /albums
func (h *Handler) List(c *gin.Context) {
	albums, err := h.Store.List(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"albums": albums})
}

// writeAlbumLookupError 把 getAlbumMeta 的失败映射为响应。
//
// 畸形 id（albums.id 是 UUID 列 → PG 22P02）与「相册不存在」共用同一 404（同状态码、
// 同 code、同 message）。这与 0ec9a78 把「无权」也收敛成 404 是同一个口径：任何可与
// 「不存在」区分的响应都是探测判据，而 500 还会顺带把 PG 原文（invalid input syntax
// for type uuid / SQLSTATE）回给客户端，属信息泄漏。
//
// 其余 DB 故障仍 500，但 message 固定：排障看服务端日志，客户端不见内部细节。
func writeAlbumLookupError(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) || pgxutil.IsMalformedID(err) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	log.Printf("[albums] 相册元信息查询失败: %v", err)
	errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
}

// Get GET /albums/:id
//
// 归属校验与同文件的 Patch/Delete 同一套 helper（getAlbumMeta + canManage）。
// 无权时返回 **404 而不是 403**：403 会告诉调用方"这个相册存在但不属于你"，
// 那就是一个可枚举的探测判据。与"相册不存在"共用同一形状。
//
// 校验刻意放在 Store.Get **之前**：smart 相册的 Get 会跑一次 criteria 全表查询，
// 无权请求不该有机会触发它。
//
// ⚠️ 属主校验只能放在这里（HTTP 层），**不能**下沉进 Store.Get ——
// internal/shares 的公开分享链路（shares.Store.ListItems → Store.Get）是匿名可达的，
// 把鉴权塞进 Store.Get 会把分享功能整条打死。
func (h *Handler) Get(c *gin.Context) {
	id := c.Param("id")
	ownerID, _, err := h.Store.getAlbumMeta(c.Request.Context(), id)
	if err != nil {
		writeAlbumLookupError(c, err)
		return
	}
	if !canManage(c, ownerID) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	d, err := h.Store.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	if err != nil {
		log.Printf("[albums] 相册详情查询失败 id=%s: %v", id, err)
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
		return
	}
	c.JSON(http.StatusOK, d)
}

// Patch PATCH /albums/:id {name?, description?, cover_media_id?, criteria?}
func (h *Handler) Patch(c *gin.Context) {
	id := c.Param("id")
	ownerID, _, err := h.Store.getAlbumMeta(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if !canManage(c, ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅相册所有者或管理员可修改")
		return
	}
	var req struct {
		Name         *string   `json:"name"`
		Description  *string   `json:"description"`
		CoverMediaID *string   `json:"cover_media_id"`
		Criteria     *Criteria `json:"criteria"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "name 不能为空")
		return
	}
	if err := h.Store.Patch(c.Request.Context(), id, req.Name, req.Description, req.CoverMediaID, req.Criteria); err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// Delete DELETE /albums/:id（favorites 相册禁止删除）
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	ownerID, typ, err := h.Store.getAlbumMeta(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if typ == "favorites" {
		errResp(c, http.StatusBadRequest, "FAVORITES_LOCKED", "收藏相册禁止删除")
		return
	}
	if !canManage(c, ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅相册所有者或管理员可删除")
		return
	}
	if err := h.Store.Delete(c.Request.Context(), id); err != nil {
		errResp(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// AddItems POST /albums/:id/items {media_ids:[...]} → {added}
func (h *Handler) AddItems(c *gin.Context) {
	id := c.Param("id")
	ownerID, typ, err := h.Store.getAlbumMeta(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if typ == "smart" {
		errResp(c, http.StatusBadRequest, "SMART_READONLY", "智能相册禁止手动添加媒体")
		return
	}
	if !canManage(c, ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅相册所有者或管理员可添加媒体")
		return
	}
	var req struct {
		MediaIDs []string `json:"media_ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.MediaIDs) == 0 {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "media_ids 不能为空")
		return
	}
	added, err := h.Store.AddItems(c.Request.Context(), id, req.MediaIDs)
	if err != nil {
		errResp(c, http.StatusBadRequest, "ADD_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"added": added})
}

// RemoveItem DELETE /albums/:id/items/:media_id
func (h *Handler) RemoveItem(c *gin.Context) {
	id := c.Param("id")
	ownerID, typ, err := h.Store.getAlbumMeta(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	if typ == "smart" {
		errResp(c, http.StatusBadRequest, "SMART_READONLY", "智能相册禁止手动移除媒体")
		return
	}
	if !canManage(c, ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅相册所有者或管理员可移除媒体")
		return
	}
	err = h.Store.RemoveItem(c.Request.Context(), id, c.Param("media_id"))
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不在该相册中")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}

// ListComments GET /albums/:id/comments
func (h *Handler) ListComments(c *gin.Context) {
	comments, err := h.Store.ListComments(c.Request.Context(), c.Param("id"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"comments": comments})
}

// AddComment POST /albums/:id/comments {content, parent_id?} → 201 {id}
func (h *Handler) AddComment(c *gin.Context) {
	albumID := c.Param("id")
	if _, _, err := h.Store.getAlbumMeta(c.Request.Context(), albumID); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "相册不存在")
		return
	}
	var req struct {
		Content  string  `json:"content"`
		ParentID *string `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	if strings.TrimSpace(req.Content) == "" {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "content 不能为空")
		return
	}
	id, err := h.Store.AddComment(c.Request.Context(), albumID, c.GetString("user_id"), req.Content, req.ParentID)
	switch {
	case errors.Is(err, ErrParentMissing):
		errResp(c, http.StatusBadRequest, "PARENT_MISSING", "父评论不存在")
	case errors.Is(err, ErrThirdLevel):
		errResp(c, http.StatusBadRequest, "THIRD_LEVEL", "仅支持两级评论，不能回复回复")
	case err != nil:
		errResp(c, http.StatusInternalServerError, "CREATE_FAILED", err.Error())
	default:
		c.JSON(http.StatusCreated, gin.H{"id": id})
	}
}

// DeleteComment DELETE /albums/:id/comments/:cid（本人或 owner/admin 角色）
func (h *Handler) DeleteComment(c *gin.Context) {
	albumID, cid := c.Param("id"), c.Param("cid")
	author, err := h.Store.GetCommentAuthor(c.Request.Context(), albumID, cid)
	if errors.Is(err, ErrCommentNoRows) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "评论不存在")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	role := c.GetString("role")
	if c.GetString("user_id") != author && role != "owner" && role != "admin" {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "仅评论本人或管理员可删除")
		return
	}
	if err := h.Store.DeleteComment(c.Request.Context(), cid); err != nil {
		errResp(c, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
		return
	}
	c.Status(http.StatusNoContent)
}
