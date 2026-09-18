package media

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
	"panoalbum/internal/index"
	"panoalbum/internal/queue"
)

// 上传（API v1.1 §3 POST /media/upload）：
//   - 单请求整文件：multipart 字段 file，不带 Content-Range；
//   - 分块续传：带 Content-Range: bytes start-end/total，首块不带 upload_id（服务端生成），
//     后续块带 upload_id 表单字段；块必须按序追加（start == 已收字节数）。

// contentRangeRe 解析 Content-Range: bytes 0-99/500。
var contentRangeRe = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+)$`)

// uploadIDRe 分块会话 ID 白名单（防路径穿越）。
var uploadIDRe = regexp.MustCompile(`^[a-f0-9]{16,32}$`)

// chunkRange 分块范围 [Start, End]，Total 为文件总字节数。
type chunkRange struct {
	Start int64
	End   int64
	Total int64
}

// parseContentRange 解析并校验 Content-Range 头；无头返回 nil（整文件上传）。
func parseContentRange(h string) (*chunkRange, error) {
	if h == "" {
		return nil, nil
	}
	m := contentRangeRe.FindStringSubmatch(strings.TrimSpace(h))
	if m == nil {
		return nil, errors.New("Content-Range 格式错误，应为 bytes start-end/total")
	}
	start, _ := strconv.ParseInt(m[1], 10, 64)
	end, _ := strconv.ParseInt(m[2], 10, 64)
	total, _ := strconv.ParseInt(m[3], 10, 64)
	if total <= 0 || start > end || end >= total {
		return nil, errors.New("Content-Range 范围非法")
	}
	return &chunkRange{Start: start, End: end, Total: total}, nil
}

// uploadMeta 分块会话元数据（存 <upload_id>.json）。
type uploadMeta struct {
	UploadID   string `json:"upload_id"`
	OwnerID    string `json:"owner_id"`
	Filename   string `json:"filename"`
	FolderPath string `json:"folder_path"`
	Space      string `json:"space"`
	TakenAt    string `json:"taken_at,omitempty"`
	Total      int64  `json:"total"`
}

// sanitizeFilename 文件名清洗：去路径成分，防穿越。
func sanitizeFilename(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if base == "." || base == ".." || base == "/" {
		return ""
	}
	return base
}

// sanitizeFolder 目录清洗：按栈语义解析 . / ..（.. 弹出前一段，无法逃逸为相对路径之外）。
func sanitizeFolder(p string) string {
	var segs []string
	for _, s := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch s {
		case "", ".":
			continue
		case "..":
			if len(segs) > 0 {
				segs = segs[:len(segs)-1]
			}
		default:
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// ResolvePath 将 media.path（相对路径）解析为磁盘绝对路径：
// 先查上传目录，再回退既有索引根目录；绝对路径原样校验。
func (h *Handler) ResolvePath(rel string) (string, bool) {
	if filepath.IsAbs(rel) {
		if _, err := os.Stat(rel); err == nil {
			return rel, true
		}
		return "", false
	}
	for _, root := range []string{h.UploadDir, h.MediaRoot} {
		if root == "" {
			continue
		}
		p := filepath.Join(root, filepath.FromSlash(rel))
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", false
}

// removeFile 删除文件（Purge 用，os.Remove 包装便于测试替换）。
var removeFile = func(p string) error { return os.Remove(p) }

// Upload POST /media/upload
func (h *Handler) Upload(c *gin.Context) {
	userID := c.GetString("user_id")

	fh, err := c.FormFile("file")
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "缺少 multipart 字段 file")
		return
	}
	cr, err := parseContentRange(c.GetHeader("Content-Range"))
	if err != nil {
		errResp(c, http.StatusBadRequest, "BAD_RANGE", err.Error())
		return
	}

	src, err := fh.Open()
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "READ_FAILED", "读取失败", err)
		return
	}
	defer src.Close()

	if cr == nil {
		// 整文件单请求上传
		meta := uploadMeta{
			OwnerID:    userID,
			Filename:   sanitizeFilename(fh.Filename),
			FolderPath: sanitizeFolder(c.PostForm("folder_path")),
			Space:      c.DefaultPostForm("space", "personal"),
			TakenAt:    c.PostForm("taken_at"),
			Total:      fh.Size,
		}
		if meta.Filename == "" {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "文件名非法")
			return
		}
		id, dup, err := h.ingest(c.Request.Context(), src, meta)
		h.respondIngest(c, id, dup, err)
		return
	}

	// 分块上传
	if cr.End-cr.Start+1 != fh.Size {
		errResp(c, http.StatusBadRequest, "BAD_RANGE",
			fmt.Sprintf("块大小 %d 与 Content-Range 声明 %d 不符", fh.Size, cr.End-cr.Start+1))
		return
	}
	uploadID := c.PostForm("upload_id")
	if uploadID == "" {
		if cr.Start != 0 {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "非首块必须携带 upload_id")
			return
		}
		uploadID = newUploadID()
	}
	if !uploadIDRe.MatchString(uploadID) {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "upload_id 非法")
		return
	}
	if err := os.MkdirAll(h.UploadTmp, 0o755); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "WRITE_FAILED", "写入失败", err)
		return
	}
	partPath := filepath.Join(h.UploadTmp, uploadID+".part")
	metaPath := filepath.Join(h.UploadTmp, uploadID+".json")

	var meta uploadMeta
	if cr.Start == 0 {
		// 首块：初始化会话（覆盖同名残留）
		meta = uploadMeta{
			UploadID:   uploadID,
			OwnerID:    userID,
			Filename:   sanitizeFilename(fh.Filename),
			FolderPath: sanitizeFolder(c.PostForm("folder_path")),
			Space:      c.DefaultPostForm("space", "personal"),
			TakenAt:    c.PostForm("taken_at"),
			Total:      cr.Total,
		}
		if meta.Filename == "" {
			errResp(c, http.StatusBadRequest, "BAD_REQUEST", "文件名非法")
			return
		}
		data, _ := json.Marshal(meta)
		if err := os.WriteFile(metaPath, data, 0o600); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "WRITE_FAILED", "写入失败", err)
			return
		}
	} else {
		data, err := os.ReadFile(metaPath)
		if err != nil {
			errResp(c, http.StatusNotFound, "NO_SESSION", "分块会话不存在，请从首块重传")
			return
		}
		if err := json.Unmarshal(data, &meta); err != nil {
			errResp(c, http.StatusInternalServerError, "SESSION_CORRUPT", "分块会话元数据损坏")
			return
		}
		if meta.OwnerID != userID {
			errResp(c, http.StatusForbidden, "FORBIDDEN", "分块会话不属于当前用户")
			return
		}
		if meta.Total != cr.Total {
			errResp(c, http.StatusBadRequest, "BAD_RANGE", "total 与会话不一致")
			return
		}
	}

	// 断点校验：块起点必须等于已收字节数（幂等重传已完成块视为成功）
	received := int64(0)
	if st, err := os.Stat(partPath); err == nil {
		received = st.Size()
	}
	if cr.Start < received && cr.End < received {
		// 该块已完整写入过（重传），直接回报进度
		c.JSON(http.StatusOK, gin.H{"upload_id": uploadID, "received": received, "complete": false})
		return
	}
	if cr.Start != received {
		errResp(c, http.StatusConflict, "RANGE_MISMATCH",
			fmt.Sprintf("断点不符：期望起点 %d，实际 %d", received, cr.Start))
		return
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "WRITE_FAILED", "写入失败", err)
		return
	}
	if _, err := io.Copy(f, src); err != nil {
		f.Close()
		httperr.Fail(c, http.StatusInternalServerError, "WRITE_FAILED", "写入失败", err)
		return
	}
	f.Close()
	received += fh.Size

	if received < cr.Total {
		c.JSON(http.StatusOK, gin.H{"upload_id": uploadID, "received": received, "complete": false})
		return
	}

	// 最后一块：合并入库
	pf, err := os.Open(partPath)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "READ_FAILED", "读取失败", err)
		return
	}
	defer pf.Close()
	id, dup, err := h.ingest(c.Request.Context(), pf, meta)
	if err == nil {
		_ = os.Remove(partPath)
		_ = os.Remove(metaPath)
	}
	h.respondIngest(c, id, dup, err)
}

// respondIngest 统一入库结果响应。
func (h *Handler) respondIngest(c *gin.Context, id string, dup bool, err error) {
	if err != nil {
		var ue *uploadError
		if errors.As(err, &ue) {
			errResp(c, ue.status, ue.code, ue.msg)
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "INGEST_FAILED", "入库失败", err)
		return
	}
	if dup {
		c.JSON(http.StatusOK, gin.H{"id": id, "status": "duplicate"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "indexing"})
}

// uploadError 带 HTTP 状态的上传错误。
type uploadError struct {
	status int
	code   string
	msg    string
}

func (e *uploadError) Error() string { return e.msg }

// ingest 完成文件入库：落盘 → hash 去重 → 元数据 → 写 media → 缩略图入队。
// r 为完整文件内容（单次上传的 part 或分块合并后的 .part 文件）。
func (h *Handler) ingest(ctx context.Context, r io.Reader, meta uploadMeta) (id string, dup bool, err error) {
	kind, ok := index.ClassifyExt(meta.Filename)
	if !ok {
		return "", false, &uploadError{http.StatusBadRequest, "UNSUPPORTED", "不支持的文件类型"}
	}

	// 先写到最终目录（按 yyyy/mm 分目录），文件名加随机前缀防碰撞
	now := time.Now()
	dateDir := fmt.Sprintf("%04d/%02d", now.Year(), now.Month())
	storedName := newUploadID() + "_" + meta.Filename
	rel := dateDir + "/" + storedName
	abs := filepath.Join(h.UploadDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", false, err
	}
	out, err := os.Create(abs)
	if err != nil {
		return "", false, err
	}
	size, err := io.Copy(out, r)
	out.Close()
	if err != nil {
		_ = os.Remove(abs)
		return "", false, err
	}

	// hash 去重（同人同内容直接返回已有 id）
	hash, err := index.HashFile(abs)
	if err != nil {
		_ = os.Remove(abs)
		return "", false, err
	}
	var existID string
	err = h.Store.Pool.QueryRow(ctx,
		`SELECT id FROM media WHERE hash = $1 AND owner_id = $2 AND duplicate_of IS NULL AND deleted_at IS NULL LIMIT 1`,
		hash, meta.OwnerID).Scan(&existID)
	if err == nil {
		_ = os.Remove(abs)
		return existID, true, nil
	}

	// 元数据提取（照片 EXIF / 视频 ffprobe）
	var m *index.Meta
	if kind == index.KindVideo {
		m, err = index.ExtractVideoMeta(ctx, abs)
	} else {
		m, err = index.ExtractPhotoMeta(abs)
	}
	if err != nil {
		_ = os.Remove(abs)
		return "", false, fmt.Errorf("元数据: %w", err)
	}

	// taken_at 优先级：EXIF/creation_time → 表单 → 文件 mtime
	takenAt := m.TakenAt
	if takenAt == nil && meta.TakenAt != "" {
		if t, perr := time.Parse(time.RFC3339, meta.TakenAt); perr == nil {
			takenAt = &t
		}
	}
	if takenAt == nil {
		if st, serr := os.Stat(abs); serr == nil {
			t := st.ModTime()
			takenAt = &t
		} else {
			t := now
			takenAt = &t
		}
	}

	// folder_path：表单指定优先，否则用日期目录
	folder := meta.FolderPath
	if folder == "" {
		folder = dateDir
	}
	space := meta.Space
	if space != "shared" {
		space = "personal"
	}

	var duration any
	if m.DurationSec != nil {
		duration = *m.DurationSec
	}
	err = h.Store.Pool.QueryRow(ctx, `
		INSERT INTO media (type, space, owner_id, path, folder_path, filename,
			taken_at, width, height, duration, codec, fps,
			gps, hash, filesize, camera_make, camera_model)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
			CASE WHEN $13::float8 IS NOT NULL AND $14::float8 IS NOT NULL
			     THEN ST_SetSRID(ST_MakePoint($14, $13), 4326) END,
			$15, $16, $17, $18)
		RETURNING id`,
		string(kind), space, meta.OwnerID, rel, folder, meta.Filename,
		*takenAt, nilIfZero(m.Width), nilIfZero(m.Height), duration, nilIfEmpty(m.Codec), nilIfZeroF(m.FPS),
		m.Lat, m.Lng,
		hash, size, nilIfEmpty(m.CameraMake), nilIfEmpty(m.CameraModel),
	).Scan(&id)
	if err != nil {
		_ = os.Remove(abs)
		return "", false, err
	}

	// 缩略图任务入队（indexctl worker 消费；path 用绝对路径）
	if h.Q != nil {
		payload := map[string]string{
			"media_id": id,
			"path":     abs,
			"kind":     string(kind),
		}
		if m.DurationSec != nil {
			payload["duration_sec"] = fmt.Sprint(*m.DurationSec)
		}
		if _, err := h.Q.Enqueue(ctx, queue.Job{Kind: "thumbnail", Payload: payload}); err != nil {
			// 入队失败不回滚入库（索引 worker 可补扫），仅记录
			fmt.Printf("缩略图入队失败 media=%s: %v\n", id, err)
		}
	}
	return id, false, nil
}

func newUploadID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nilIfZero(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
func nilIfZeroF(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Download GET /media/:id/download（原文件流，attachment）
func (h *Handler) Download(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.checkAccess(c, id); !ok {
		return
	}
	var rel string
	var filename *string
	err := h.Store.Pool.QueryRow(c.Request.Context(),
		`SELECT path, filename FROM media WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&rel, &filename)
	if err != nil {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	abs, found := h.ResolvePath(rel)
	if !found {
		errResp(c, http.StatusNotFound, "FILE_MISSING", "文件不在磁盘上")
		return
	}
	name := id
	if filename != nil && *filename != "" {
		name = *filename
	}
	c.FileAttachment(abs, name)
}
