// Package transcode 转码任务端点与 HLS 静态服务（API v1.1 §12/§13）。
package transcode

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/ffmpeg"
	"panoalbum/internal/queue"
)

// Handler 转码端点。
type Handler struct {
	Pool   *pgxpool.Pool
	Q      *queue.Queue // 转码队列（"transcode"，transcodectl worker 消费）
	HLSDir string       // HLS 输出根目录（./data/hls）
}

// LadderForProfile 按档位名选 HLS 码率阶梯（1080p|2k|4k）。
func LadderForProfile(profile string) []ffmpeg.HLSRendition {
	ladder := ffmpeg.DefaultHLSLadder()
	switch profile {
	case "2k":
		return ladder[:2]
	case "4k":
		return ladder
	default: // 1080p
		return ladder[:1]
	}
}

// profileMaxHeight 档位名 → 允许的最大档位高度（0 = 不额外限制）。
func profileMaxHeight(profile string) int {
	switch profile {
	case "1080p":
		return 1080
	case "2k":
		return 1440
	default: // 4k 及未知档位不额外限高（仍受源分辨率约束）
		return 0
	}
}

// LadderForSourceProfile 结合源分辨率与档位上限裁剪码率阶梯，**绝不上采样**：
// 先按 srcWidth/srcHeight 裁剪（ffmpeg.LadderForSource），再按档位名限高。
// srcWidth/srcHeight ≤ 0（分辨率未知）时返回 nil，调用方应回退 LadderForProfile。
func LadderForSourceProfile(profile string, srcWidth, srcHeight int) []ffmpeg.HLSRendition {
	ladder := ffmpeg.LadderForSource(srcWidth, srcHeight)
	maxH := profileMaxHeight(profile)
	if maxH <= 0 {
		return ladder
	}
	out := make([]ffmpeg.HLSRendition, 0, len(ladder))
	for _, r := range ladder {
		if r.Height <= maxH {
			out = append(out, r)
		}
	}
	// 阶梯本身已按源裁剪，理论上不会全被限高剔除；兜底保留最高档
	if len(out) == 0 {
		return ladder
	}
	return out
}

// ProfileForSource 按源分辨率推荐档位名（写入 transcode_jobs.profile）。
func ProfileForSource(srcWidth, srcHeight int) string {
	switch {
	case srcWidth > 2560 || srcHeight > 1440:
		return "4k"
	case srcWidth > 1920 || srcHeight > 1080:
		return "2k"
	default: // 含分辨率未知的保守档
		return "1080p"
	}
}

// validProfile 校验档位名。
func validProfile(p string) bool {
	return p == "1080p" || p == "2k" || p == "4k"
}

// CreateJob POST /transcode/job {media_id, profile} → {job_id}
// 写 transcode_jobs 并入队 kind=transcode。
func (h *Handler) CreateJob(c *gin.Context) {
	var req struct {
		MediaID string `json:"media_id"`
		Profile string `json:"profile"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.MediaID == "" {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需含 media_id")
		return
	}
	if req.Profile == "" {
		req.Profile = "1080p"
	}
	if !validProfile(req.Profile) {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "profile 需为 1080p|2k|4k")
		return
	}

	ctx := c.Request.Context()
	// 校验媒体存在且为视频（归属校验：本人或 owner/admin）
	var ownerID, typ string
	err := h.Pool.QueryRow(ctx,
		`SELECT owner_id, type::text FROM media WHERE id = $1 AND deleted_at IS NULL`, req.MediaID).
		Scan(&ownerID, &typ)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	role := c.GetString("role")
	if c.GetString("user_id") != ownerID && role != "owner" && role != "admin" {
		errJSON(c, http.StatusForbidden, "FORBIDDEN", "无权操作该媒体")
		return
	}
	if typ != "video" {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "仅视频可转 HLS")
		return
	}

	var jobID string
	if err := h.Pool.QueryRow(ctx,
		`INSERT INTO transcode_jobs (media_id, kind, profile, status)
		 VALUES ($1, 'hls', $2, 'pending') RETURNING id`, req.MediaID, req.Profile).Scan(&jobID); err != nil {
		errJSON(c, http.StatusInternalServerError, "INSERT_FAILED", err.Error())
		return
	}
	_, err = h.Q.Enqueue(ctx, queue.Job{Kind: "transcode", Payload: map[string]string{
		"job_id":   jobID,
		"media_id": req.MediaID,
		"profile":  req.Profile,
	}})
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "ENQUEUE_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

// JobStatus GET /transcode/job/:id（查任务状态，前端轮询用）
func (h *Handler) JobStatus(c *gin.Context) {
	var j struct {
		ID         string  `json:"id"`
		MediaID    string  `json:"media_id"`
		Status     string  `json:"status"`
		Profile    *string `json:"profile,omitempty"`
		ResultPath *string `json:"result_path,omitempty"`
	}
	err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT id, media_id, status, profile, result_path FROM transcode_jobs WHERE id = $1`,
		c.Param("id")).Scan(&j.ID, &j.MediaID, &j.Status, &j.Profile, &j.ResultPath)
	if errors.Is(err, pgx.ErrNoRows) {
		errJSON(c, http.StatusNotFound, "NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		errJSON(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
		return
	}
	c.JSON(http.StatusOK, j)
}

// uuidRe 路径段白名单（防穿越）。
var uuidRe = regexp.MustCompile(`^[0-9a-fA-F-]{32,36}$`)

// ServeHLS GET /transcode/hls/:id/*file：带鉴权的 HLS 静态服务（master.m3u8 与分片）。
func (h *Handler) ServeHLS(c *gin.Context) {
	id := c.Param("id")
	if !uuidRe.MatchString(id) {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "非法媒体 id")
		return
	}
	// 清理相对路径，禁止穿越
	rel := filepath.Clean(strings.TrimPrefix(c.Param("file"), "/"))
	if rel == "." || strings.HasPrefix(rel, "..") {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "非法路径")
		return
	}
	abs := filepath.Join(h.HLSDir, id, rel)
	// 双保险：解析后必须仍位于 HLSDir/id 之下
	base, _ := filepath.Abs(filepath.Join(h.HLSDir, id))
	full, _ := filepath.Abs(abs)
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		errJSON(c, http.StatusBadRequest, "BAD_REQUEST", "非法路径")
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		errJSON(c, http.StatusNotFound, "NOT_FOUND", "HLS 文件不存在")
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

func errJSON(c *gin.Context, status int, code, msg string) {
	c.JSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}
