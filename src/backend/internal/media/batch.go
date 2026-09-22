package media

// Job000066 批量操作统一端点：POST /media/batch {ids, op, ...}
// 七模块（时间轴/相册/地图/标签/人物/地点/文件夹/空间）的批量行为在此收敛——
// 前端各模块只负责"选了哪些"，语义（归属校验/软删/移动/标签/元数据/复制）在此一处实现。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/folders"
	"panoalbum/internal/httperr"
	"panoalbum/internal/queue"
)

const batchMaxIDs = 200

var batchOps = map[string]bool{
	"delete":      true,
	"move":        true,
	"copy":        true,
	"add_tags":    true,
	"remove_tags": true,
	"set_meta":    true,
	"share_space": true, // 个人空间 → 共享空间
}

type batchReq struct {
	IDs        []string `json:"ids"` // 1..200，越界 400
	Op         string   `json:"op"`
	FolderPath string   `json:"folder_path"`        // move/copy：sanitizeFolder 后落库
	TagIDs     []string `json:"tag_ids"`            // add_tags/remove_tags
	TakenAt    *string  `json:"taken_at,omitempty"` // set_meta：RFC3339；null=清空
	Place      *string  `json:"place,omitempty"`    // set_meta：空串=清空
}

// Batch POST /media/batch——统一批量操作。
// 归属：一次查询收敛（id=ANY + owner + 未删），无权/不存在同入 failed（与单条 404 同形政策一致）。
// 响应 {succeeded, failed:[{id, reason}]}——部分成功不回滚（软删/移动天然幂等可重试）。
func (h *Handler) Batch(c *gin.Context) {
	var req batchReq
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields() // 与 Patch 同口径：非白名单字段拒收
	if err := dec.Decode(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体非法")
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > batchMaxIDs {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", fmt.Sprintf("ids 数量须 1..%d", batchMaxIDs))
		return
	}
	if !batchOps[req.Op] {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "op 非法（delete|move|copy|add_tags|remove_tags|set_meta|share_space）")
		return
	}
	userID := c.GetString("user_id")
	allowed, failed := h.Store.batchOwnedIDs(c.Request.Context(), req.IDs, userID)
	if len(allowed) == 0 {
		c.JSON(http.StatusOK, gin.H{"succeeded": 0, "failed": failed})
		return
	}

	var succeeded int
	switch req.Op {
	case "delete":
		n, err := h.Store.BatchSoftDelete(c.Request.Context(), allowed)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "删除失败", err)
			return
		}
		succeeded = n
	case "move":
		folder := sanitizeFolder(req.FolderPath)
		// 目录写权限（Job000069）：目标目录为注册目录时校验 write 授权。
		if folder != "" {
			ok, cwErr := folders.CanWrite(c.Request.Context(), h.Store.Pool, folder, c.GetString("user_id"))
			if cwErr != nil {
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "移动失败", cwErr)
				return
			}
			if !ok {
				errResp(c, http.StatusForbidden, "FORBIDDEN", "无目标目录的写入权限")
				return
			}
		}
		n, err := h.Store.BatchSetFolder(c.Request.Context(), allowed, folder)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "移动失败", err)
			return
		}
		succeeded = n
	case "add_tags", "remove_tags":
		if len(req.TagIDs) == 0 || len(req.TagIDs) > 50 {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "tag_ids 须 1..50")
			return
		}
		add := req.Op == "add_tags"
		n, err := h.Store.BatchSetTags(c.Request.Context(), allowed, req.TagIDs, add)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "标签更新失败", err)
			return
		}
		succeeded = n
	case "set_meta":
		var takenAt any
		if req.TakenAt != nil {
			t, err := time.Parse(time.RFC3339, *req.TakenAt)
			if err != nil {
				errResp(c, http.StatusBadRequest, "BAD_REQUEST", "taken_at 须为 RFC3339")
				return
			}
			takenAt = t
		}
		var place any
		if req.Place != nil {
			p := strings.TrimSpace(*req.Place)
			if p == "" {
				place = nil
			} else {
				place = p
			}
		}
		n, err := h.Store.BatchSetMeta(c.Request.Context(), allowed, takenAt, req.TakenAt != nil, place, req.Place != nil)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "元数据更新失败", err)
			return
		}
		succeeded = n
	case "share_space":
		n, err := h.Store.BatchSetSpace(c.Request.Context(), allowed)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "共享失败", err)
			return
		}
		succeeded = n
	case "copy":
		// 复制=新行+磁盘文件副本（与 ingest 同落盘规则：日期目录+新 id 文件名）
		n, errs := h.batchCopy(c, allowed, userID)
		for _, e := range errs {
			failed = append(failed, gin.H{"id": e.id, "reason": e.reason})
		}
		succeeded = n
	}

	h.record(c, "media.batch", audit.TargetMedia, fmt.Sprintf("%s x%d", req.Op, succeeded),
		map[string]any{"op": req.Op, "count": succeeded, "total": len(req.IDs)})
	c.JSON(http.StatusOK, gin.H{"succeeded": succeeded, "failed": failed})
}

type batchCopyErr struct {
	id     string
	reason string
}

// batchCopy 逐条复制（行+文件）。文件复制失败不回滚已复制的行（响应里逐条报告）。
func (h *Handler) batchCopy(c *gin.Context, ids []string, userID string) (int, []batchCopyErr) {
	var succeeded int
	var errs []batchCopyErr
	for _, id := range ids {
		newID, err := h.copyOne(c.Request.Context(), id, userID)
		if err != nil {
			errs = append(errs, batchCopyErr{id: id, reason: "复制失败"})
			continue
		}
		succeeded++
		_ = newID
	}
	return succeeded, errs
}

// copyOne 复制一行媒体：DB 行拷贝（新 id/同 filename/新 path）+ 磁盘文件字节拷贝。
func (h *Handler) copyOne(ctx context.Context, id string, userID string) (string, error) {
	row, rel, err := h.Store.copySourceRow(ctx, id)
	if err != nil {
		return "", err
	}
	// 新存储路径：日期目录 + 新 id 前缀文件名（与 upload.ingest 同规则）
	dateDir := time.Now().Format("2006/01")
	storedName := fmt.Sprintf("%s_%s", row.id, row.filename)
	if storedName == "_"+row.filename {
		storedName = row.id + "_copy"
	}
	newRel := dateDir + "/" + storedName
	srcAbs := filepath.Join(h.UploadDir, filepath.FromSlash(rel))
	dstAbs := filepath.Join(h.UploadDir, filepath.FromSlash(newRel))
	if err := os.MkdirAll(filepath.Dir(dstAbs), 0o755); err != nil {
		return "", err
	}
	if err := copyFile(srcAbs, dstAbs); err != nil {
		return "", err
	}
	newID, err := h.Store.insertCopiedRow(ctx, row, userID, newRel, storedName)
	if err != nil {
		os.Remove(dstAbs)
		return "", err
	}
	// 副本行缩略图为空（INSERT 不复制 thumbnail_* 列）→ 入队重建
	if h.Q != nil {
		payload := map[string]string{"media_id": newID, "path": dstAbs, "kind": row.typ}
		_, _ = h.Q.Enqueue(ctx, queue.Job{Kind: "thumbnail", Payload: payload})
	}
	return newID, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
