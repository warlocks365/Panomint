package media

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
	"panoalbum/internal/pgxutil"
	"panoalbum/internal/queue"
)

// 媒体写操作 HTTP 处理器（详情/收藏/评级/软删/回收站）。

// errResp 统一错误响应 {"error":{"code","message"}}。
// 形状委托给 internal/httperr（单一真源），本包不再各写一份 c.JSON(gin.H{...})。
func errResp(c *gin.Context, status int, code, msg string) {
	httperr.Envelope(c, status, code, msg)
}

// writeLookupError 把「取媒体元信息/归属」失败的 DB 错误映射为响应。
//
// ★ 安全不变量：**畸形 id 与「记录不存在」必须逐字节同形**（同一个 404 NOT_FOUND /
// 同一个 message）。media.id 是 UUID 列，`GET /media/not-a-uuid` 会让 PG 抛 22P02；
// 若把它当 500，既伪造了"服务故障"，又会把 PG 原文（invalid input syntax for type uuid、
// SQLSTATE、列类型）回给客户端。把两条分支收敛到同一个响应，是因为对调用方而言
// "id 语法非法"与"这条不存在"是同一件事；任何可区分的响应都会变成探测判据。
//
// 其余 DB 错误（连不上库、超时、语法正确的 UUID 查询失败）仍是 500，但 message 固定：
// 服务端 log 保留完整错误供排障，客户端不该看到内部实现细节。
func writeLookupError(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) || pgxutil.IsMalformedID(err) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在")
		return
	}
	log.Printf("[media] 媒体归属/存在性查询失败: %v", err)
	errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
}

// checkAccess 校验媒体归属；resp 为 false 时已写出错误响应。
//
// 错误映射统一走 writeLookupError —— 本函数的调用方遍布 /media/** 的读写端点
// （Detail/Favorite/Rating/Delete/Restore/Purge/…），故畸形 id 的 404 口径与
// 「不回显 PG 原文」在这里一次性生效，不需要每个端点各判一遍（单一真源）。
func (h *Handler) checkAccess(c *gin.Context, id string) (ownerID string, ok bool) {
	ownerID, _, err := h.Store.ownerOf(c.Request.Context(), id)
	if err != nil {
		writeLookupError(c, err)
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
		log.Printf("[media] 媒体详情查询失败 id=%s: %v", id, err)
		errResp(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败")
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
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
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
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "rating": req.Rating})
}

// Patch PATCH /media/:id {notes?, edits?}
// Job000005：仅允许更新 notes / edits 字段，其他字段拒收 400。
// 注意语义差异：PATCH 的 edits 是**整体提交**（整对象替换；显式 null 即清空），
// 不做 MergeRotateEdit/MergeCropEdit 的单维度合并——前端整体提交契约不变。
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
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
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
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
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
//
// Phase 4 修复：rotate/crop 改为「读-改-写」合并语义——原实现是整对象覆盖，
// 先 PATCH 写入 {rotate,crop} 后再 POST op=rotate 会抹掉 crop，再 POST op=crop 会把 rotate 重置为 0。
// op:"auto" 维持「清空编辑参数」的既有语义（本实现不含自动增强算法，原文件始终不变）。
//
// 注意：PATCH /media/:id 走的是整体提交（"{edits:{...}}" 或显式 null 清空），
// 语义与这里不同，源码见下方 Patch，未做改动。
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

	// 1) 入参校验（不触库，先失败先返回）
	var crop *CropRect
	switch req.Op {
	case "rotate":
		switch req.Angle {
		case 90, 180, 270:
		default:
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "angle 需为 90/180/270")
			return
		}
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
		crop = e.Crop
	case "auto":
		// 无自动增强算法：按「重置编辑参数」处理（rotate 与 crop 一并清空）
	default:
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "op 需为 rotate|crop|auto")
		return
	}

	// 2) 读现有编辑参数
	cur, err := h.Store.GetEdits(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}

	// 3) 合并：单维度 op 只改自己那一段，保留另一段
	var edits *Edits
	switch req.Op {
	case "rotate":
		edits = MergeRotateEdit(cur, req.Angle)
	case "crop":
		edits = MergeCropEdit(cur, crop)
	case "auto":
		edits = nil // 清空编辑参数
	}

	// 4) 落库
	if err := h.Store.SetEdits(c.Request.Context(), id, edits); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
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
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	if err := h.Store.SoftDelete(c.Request.Context(), id); errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已在回收站")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// 审计在**成功落库之后**：软删失败（404/500）不写，否则审计里会出现
	// "谁删了 X" 而 X 其实还在 —— 记录错误的审计比不记录更危险（见 audit.Recorder 的注释）。
	// detail 只记 owner_id（中性键名，不带任何敏感子串），用于按"某人名下被删的媒体"追查。
	h.record(c, audit.ActionMediaDelete, audit.TargetMedia, id, map[string]any{"owner_id": ownerID})
	c.JSON(http.StatusOK, gin.H{"id": id, "deleted": true})
}

// Trash GET /media/trash（回收站列表，仅本人）
func (h *Handler) Trash(c *gin.Context) {
	res, err := h.Store.ListTrash(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, res)
}

// Restore POST /media/trash/:id/restore
func (h *Handler) Restore(c *gin.Context) {
	id := c.Param("id")
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	path, err := h.Store.Restore(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// 审计在**成功落库之后**：对**不在回收站**的 id（未软删或不存在）返回 404 且**不写**
	// —— 记一条「已恢复」而它其实从未被删，与记一条假的「已删除」同样危险（§二十四.1）。
	// 这条负对照必须在真库上跑：只验成功路径时，"进了 handler 就写"与"成功后写"表现完全相同。
	//
	// detail 记 path 的理由与 Purge 相同（§二十四.7）：恢复之后这行仍可能被 purge，
	// 届时 target_id 查不回任何东西；path 是这条媒体在库里最后的可解析线索，
	// 而只有在恢复的当口才取得到。键名 owner_id / path 均不含敏感子串，不会被 RedactDetail 剔除。
	h.record(c, audit.ActionMediaRestore, audit.TargetMedia, id,
		map[string]any{"owner_id": ownerID, "path": path})
	c.JSON(http.StatusOK, gin.H{"id": id, "restored": true})
}

// Purge DELETE /media/trash/:id（永久删除：行删除 + 上传目录内文件清理）
func (h *Handler) Purge(c *gin.Context) {
	id := c.Param("id")
	ownerID, ok := h.checkAccess(c, id)
	if !ok {
		return
	}
	path, err := h.Store.Purge(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或不在回收站")
		return
	} else if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	// ⭐ 本动作**不可恢复**，是全部动作里唯一"事后无从补救"的一个，所以审计行必须落在
	// DELETE 之后、且在下面删磁盘文件**之前** —— 反过来的话，进程若在写审计前挂掉，
	// 文件已经没了而审计里没有这条记录。
	//
	// detail 记 path 的理由：purge 会把 media 行**整行删除**，此后 target_id 再也查不回
	// 任何东西（GetDetail 只会说"不存在或已删除"）。path 是这条媒体在库里最后的痕迹，
	// 也是事后唯一能回答"到底销毁了什么"的线索 —— 行没了，审计还在，这才是审计的意义。
	// 键名 path / owner_id 均不含敏感子串，不会被 RedactDetail 剔除。
	h.record(c, audit.ActionMediaPurge, audit.TargetMedia, id,
		map[string]any{"owner_id": ownerID, "path": path})
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
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
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
