package media

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// 媒体写操作 HTTP 处理器（详情/收藏/评级/软删/回收站）。

// errResp 统一错误响应 {"error":{"code","message"}}。
func errResp(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}

// checkAccess 校验媒体归属；resp 为 false 时已写出错误响应。
func (h *Handler) checkAccess(c *gin.Context, id string) (ownerID string, ok bool) {
	ownerID, _, err := h.Store.ownerOf(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在")
		return "", false
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return "", false
	}
	if !canAccess(c.GetString("user_id"), c.GetString("role"), ownerID) {
		errResp(c, http.StatusForbidden, "FORBIDDEN", "无权访问该媒体")
		return "", false
	}
	return ownerID, true
}

// Detail GET /media/:id
func (h *Handler) Detail(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, d)
}

// Favorite POST /media/:id/favorite {favorite:bool}
func (h *Handler) Favorite(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Favorite bool `json:"favorite"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {\"favorite\": true|false}")
		return
	}
	if err := h.Store.SetFavorite(c.Request.Context(), id, c.GetString("user_id"), req.Favorite); err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "favorite": req.Favorite})
}

// Rate POST /media/:id/rate {rating:0-5}
func (h *Handler) Rate(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Rating int `json:"rating"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Rating < 0 || req.Rating > 5 {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "rating 需为 0-5 整数")
		return
	}
	if err := h.Store.SetRating(c.Request.Context(), id, req.Rating); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "rating": req.Rating})
}

// Patch PATCH /media/:id {notes}（Job000005：仅允许更新 notes 字段，其他字段拒收 400）
func (h *Handler) Patch(c *gin.Context) {
	var req struct {
		Notes *string `json:"notes"`
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields() // 设计裁决：非 notes 字段一律拒收（显式优于静默忽略）
	if err := dec.Decode(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "仅支持更新 notes 字段，请求体需为 {\"notes\": \"...\"}")
		return
	}
	if req.Notes == nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 notes 字段")
		return
	}
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	if err := h.Store.SetNotes(c.Request.Context(), id, *req.Notes); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "notes": *req.Notes})
}

// Delete DELETE /media/:id（软删入回收站）
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	if err := h.Store.SoftDelete(c.Request.Context(), id); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已在回收站")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}

// Trash GET /media/trash（回收站列表，仅本人）
func (h *Handler) Trash(c *gin.Context) {
	res, err := h.Store.ListTrash(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, res)
}

// Restore POST /media/trash/:id/restore
func (h *Handler) Restore(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	if err := h.Store.Restore(c.Request.Context(), id); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "restored": true})
}

// Purge DELETE /media/trash/:id（永久删除：行删除 + 上传目录内文件清理）
func (h *Handler) Purge(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	path, err := h.Store.Purge(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	// 尽力清理磁盘文件（仅允许删除已知根目录下的文件）
	if abs, ok := h.ResolvePath(path); ok {
		_ = removeFile(abs)
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "purged": true})
}

// Pano360 GET /media/:id/360（360 播放元数据，契约 §13）
func (h *Handler) Pano360(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	resp := gin.H{
		"is_360":         d.Is360,
		"projection":     d.Projection,
		"width":          d.Width,
		"height":         d.Height,
		"gyro_supported": true,
		"vr_supported":   true,
	}
	if d.HLSMaster != nil && *d.HLSMaster != "" {
		resp["hls_master"] = *d.HLSMaster
	} else {
		resp["hls_master"] = nil
		resp["needs_transcode"] = true // 无 HLS 流，前端提示先转码
	}
	c.JSON(http.StatusOK, resp)
}
