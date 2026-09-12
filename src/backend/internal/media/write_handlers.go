package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/queue"
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

// Patch PATCH /media/:id {notes?, edits?}
// Job000005：仅允许更新 notes / edits 字段，其他字段拒收 400。
func (h *Handler) Patch(c *gin.Context) {
	var req struct {
		Notes *string         `json:"notes"`
		Edits json.RawMessage `json:"edits"`
	}
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields() // 设计裁决：非白名单字段一律拒收（显式优于静默忽略）
	if err := dec.Decode(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "仅支持更新 notes / edits 字段")
		return
	}
	if req.Notes == nil && req.Edits == nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 notes 或 edits 字段")
		return
	}
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	resp := gin.H{"id": id}
	if req.Notes != nil {
		if err := h.Store.SetNotes(c.Request.Context(), id, *req.Notes); errors.Is(err, ErrNotFound) {
			errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
			return
		} else if err != nil {
			errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		resp["notes"] = *req.Notes
	}
	if req.Edits != nil {
		// 显式 null → 清空编辑（重置）
		var edits *Edits
		if string(req.Edits) != "null" {
			e, err := NormalizeEdits(req.Edits)
			if err != nil {
				errResp(c, http.StatusBadRequest, "BAD_REQUEST", "edits 非法：rotate 需 0/90/180/270，crop 需归一化 {x,y,w,h}")
				return
			}
			edits = e
		}
		if err := h.Store.SetEdits(c.Request.Context(), id, edits); errors.Is(err, ErrNotFound) {
			errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
			return
		} else if err != nil {
			errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
			return
		}
		if edits != nil {
			resp["edits"] = edits
		} else {
			resp["edits"] = nil
		}
		h.enqueueThumbRegen(c, id)
	}
	c.JSON(http.StatusOK, resp)
}

// Rotate POST /media/:id/rotate {op:"rotate|crop|auto", angle?, rect?}（契约 §3，非破坏 sidecar）
func (h *Handler) Rotate(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var req struct {
		Op    string    `json:"op"`
		Angle int       `json:"angle"`
		Rect  *CropRect `json:"rect"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 {op, angle?, rect?}")
		return
	}

	var edits *Edits
	switch req.Op {
	case "rotate":
		switch req.Angle {
		case 90, 180, 270:
		default:
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "angle 需为 90/180/270")
			return
		}
		edits = &Edits{Rotate: req.Angle}
	case "crop":
		if req.Rect == nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 rect")
			return
		}
		raw, _ := json.Marshal(Edits{Crop: req.Rect})
		e, err := NormalizeEdits(raw)
		if err != nil {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "rect 需为归一化 {x,y,w,h} 且不越界")
			return
		}
		edits = e
	case "auto":
		// 本实现不含自动增强算法：按"重置编辑参数"处理（原文件始终不变）
		edits = nil
	default:
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "op 需为 rotate|crop|auto")
		return
	}

	if err := h.Store.SetEdits(c.Request.Context(), id, edits); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		errResp(c, http.StatusInternalServerError, "UPDATE_FAILED", err.Error())
		return
	}
	h.enqueueThumbRegen(c, id)
	c.JSON(http.StatusOK, gin.H{"id": id, "edits": edits})
}

// enqueueThumbRegen 尽力而为：编辑变更后重排缩略图任务。
// worker 侧（internal/index/worker.go 的 editFilter）会读取 media.edits 并套用
// 「先裁剪后旋转」滤镜链，使缩略图与查看器呈现一致；此处仅负责投递 "thumbnail" 任务，
// 投递失败不影响编辑参数已持久化。
func (h *Handler) enqueueThumbRegen(c *gin.Context, id string) {
	if h.Q == nil {
		return
	}
	d, err := h.Store.GetDetail(c.Request.Context(), id)
	if err != nil || d == nil {
		return
	}
	abs, ok := h.ResolvePath(d.Path)
	if !ok {
		return
	}
	payload := map[string]string{"media_id": id, "path": abs, "kind": d.Type}
	if _, err := h.Q.Enqueue(c.Request.Context(), queue.Job{Kind: "thumbnail", Payload: payload}); err != nil {
		fmt.Printf("缩略图重排失败 media=%s: %v\n", id, err)
	}
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
