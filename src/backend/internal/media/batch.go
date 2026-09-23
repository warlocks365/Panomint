package media

// Job000066 批量操作统一端点：POST /media/batch {ids, op, ...}
// 七模块（时间轴/相册/地图/标签/人物/地点/文件夹/空间）的批量行为在此收敛——
// 前端各模块只负责"选了哪些"，语义（归属校验/软删/移动/标签/元数据/复制）在此一处实现。

import (
	"context"
	"encoding/json"
	"errors"
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
	FolderPath *string  `json:"folder_path,omitempty"` // move：目标目录（nil=根目录）；copy：nil=继承源目录，非 nil（含""）=置为目标目录
	AlbumID    string   `json:"album_id"`              // Job000100 move/copy：相册目标（move 与 folder_path 二选一，copy 可同给）
	TagIDs     []string `json:"tag_ids"`               // add_tags/remove_tags
	TakenAt    *string  `json:"taken_at,omitempty"`    // set_meta：RFC3339；null=清空
	Place      *string  `json:"place,omitempty"`       // set_meta：空串=清空
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

	// Job000100 相册目标守卫（fail-fast：先于归属过滤/文件复制，避免复制后才失败）。
	// 口径与 POST /albums/:id/items 逐字一致——404 同形 / SMART_READONLY / canManage。
	var albumTarget *albumTargetInfo
	if req.AlbumID != "" {
		info, found, err := h.Store.getAlbumTarget(c.Request.Context(), req.AlbumID)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "操作失败", err)
			return
		}
		if st, code, msg := albumTargetGuard(!found, info.typ, userID, info.ownerID, c.GetString("role")); st != 0 {
			errResp(c, st, code, msg)
			return
		}
		albumTarget = &info
	}

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
		if albumTarget != nil {
			// 移动→相册 = 加入相册成员（相册是虚拟集合，folder_path 不变）。
			n, err := h.Store.BatchAddToAlbum(c.Request.Context(), req.AlbumID, albumTarget.ownerID, allowed)
			if err != nil {
				if errors.Is(err, errBatchMediaNotAccessible) {
					errResp(c, http.StatusForbidden, "FORBIDDEN", "部分媒体不在相册所有者的可见范围内")
					return
				}
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "移动失败", err)
				return
			}
			succeeded = n
			break
		}
		folder := ""
		if req.FolderPath != nil {
			folder = sanitizeFolder(*req.FolderPath)
		}
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
		// 复制=新行+磁盘文件副本（与 ingest 同落盘规则：日期目录+新 id 文件名）。
		// Job000100 目标选择：folder_path 目录目标（CanWrite 守卫同 move）与 album_id
		// 相册目标（守卫已前置）可同时给；都未给时保持旧行为（副本继承源目录）。
		setFolder := req.FolderPath != nil
		folder := ""
		if setFolder {
			folder = sanitizeFolder(*req.FolderPath)
			ok, cwErr := folders.CanWrite(c.Request.Context(), h.Store.Pool, folder, userID)
			if cwErr != nil {
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "复制失败", cwErr)
				return
			}
			if !ok {
				errResp(c, http.StatusForbidden, "FORBIDDEN", "无目标目录的写入权限")
				return
			}
		}
		n, errs := h.batchCopy(c, allowed, userID, folder, setFolder, req.AlbumID, albumTarget)
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

// albumTargetGuard 相册目标的纯守卫判定（status=0 表示通过）。
// 口径与 internal/albums 的 AddItems 逐字一致：404 同形（不泄露存在性）/
// SMART_READONLY 智能相册只读 / FORBIDDEN 仅属主或 owner/admin 角色。
func albumTargetGuard(notFound bool, typ, userID, ownerID, role string) (status int, code, msg string) {
	if notFound {
		return http.StatusNotFound, "NOT_FOUND", "相册不存在"
	}
	if typ == "smart" {
		return http.StatusBadRequest, "SMART_READONLY", "智能相册禁止手动添加媒体"
	}
	if userID != ownerID && role != "owner" && role != "admin" {
		return http.StatusForbidden, "FORBIDDEN", "仅相册所有者或管理员可添加媒体"
	}
	return 0, "", ""
}

// batchCopy 逐条复制（行+文件）。文件复制失败不回滚已复制的行（响应里逐条报告）。
// Job000100：folder/setFolder 为目录目标（副本 folder_path 置为 folder，否则继承源目录）；
// albumID/albumTarget 非 nil 时副本行全部生成后一次性入册 album_items（整笔可见性校验，
// 失败时副本保留在个人空间、按源 id 逐条报失败）。
func (h *Handler) batchCopy(c *gin.Context, ids []string, userID, folder string, setFolder bool, albumID string, albumTarget *albumTargetInfo) (int, []batchCopyErr) {
	var succeeded int
	var errs []batchCopyErr
	type pair struct{ src, newID string }
	var copied []pair
	for _, id := range ids {
		newID, err := h.copyOne(c.Request.Context(), id, userID, folder, setFolder)
		if err != nil {
			errs = append(errs, batchCopyErr{id: id, reason: "复制失败"})
			continue
		}
		succeeded++
		if albumTarget != nil {
			copied = append(copied, pair{src: id, newID: newID})
		}
	}
	if albumTarget != nil && len(copied) > 0 {
		newIDs := make([]string, 0, len(copied))
		for _, p := range copied {
			newIDs = append(newIDs, p.newID)
		}
		if _, err := h.Store.BatchAddToAlbum(c.Request.Context(), albumID, albumTarget.ownerID, newIDs); err != nil {
			// 副本已落地但入册失败：按源 id 逐条报失败（副本仍留在个人空间，不静默丢失）。
			succeeded -= len(copied)
			for _, p := range copied {
				errs = append(errs, batchCopyErr{id: p.src, reason: "已复制但加入相册失败（副本保留在个人空间）"})
			}
		}
	}
	return succeeded, errs
}

// copyOne 复制一行媒体：DB 行拷贝（新 id/同 filename/新 path/目标目录）+ 磁盘文件字节拷贝。
func (h *Handler) copyOne(ctx context.Context, id string, userID string, folder string, setFolder bool) (string, error) {
	row, rel, err := h.Store.copySourceRow(ctx, id)
	if err != nil {
		return "", err
	}
	dstFolder := row.folder
	if setFolder {
		dstFolder = folder
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
	newID, err := h.Store.insertCopiedRow(ctx, row, userID, newRel, storedName, dstFolder)
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
