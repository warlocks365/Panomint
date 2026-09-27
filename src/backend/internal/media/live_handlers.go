// live_handlers.go —— HEVC 兼容播放（实时转码兜底）HTTP 接线（Job000131 P1）。
// 路由挂在 /media/:id/live/*（AuthRequired 的 GET /media/ 白名单内，query token 可用）。
// 权限与文件解析复用 Handler 既有原语（checkReadAccess / ResolvePath），语义与 Download 一致：
// 无权 404/403 同形、FILE_MISSING 同形；并发上限/会话错误各自 503 语义。
package media

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/live"
)

// LiveHandler 实时转码兜底端点集。
type LiveHandler struct {
	Manager *live.Manager
	Media   *Handler // 复用 checkReadAccess / ResolvePath / Store.Pool
}


// MasterM3U8 GET /media/:id/live/master.m3u8?pos=<秒>
// 确保会话存在并等待首份 ffmpeg 清单，原样返回（hls_list_size=0 动态累积清单）。
func (h *LiveHandler) MasterM3U8(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.Media.checkReadAccess(c, id); !ok {
		return
	}
	var rel string
	err := h.Media.Store.Pool.QueryRow(c.Request.Context(),
		`SELECT path FROM media WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&rel)
	if err != nil {
		errResp(c, http.StatusNotFound, "NOT_FOUND", "媒体不存在或已删除")
		return
	}
	abs, found := h.Media.ResolvePath(rel)
	if !found {
		errResp(c, http.StatusNotFound, "FILE_MISSING", "文件不在磁盘上")
		return
	}
	pos := parsePos(c.Query("pos"))
	sess, err := h.Manager.Ensure(id, abs, pos)
	if err != nil {
		switch {
		case errors.Is(err, live.ErrBusy):
			errResp(c, http.StatusServiceUnavailable, "LIVE_BUSY", "已有实时转码任务进行中，请稍后重试")
		case errors.Is(err, live.ErrSourceGone):
			errResp(c, http.StatusNotFound, "FILE_MISSING", "文件不在磁盘上")
		default:
			errResp(c, http.StatusInternalServerError, "LIVE_START_FAILED", "转码启动失败")
		}
		return
	}
	manifest, ended, err := h.Manager.ManifestPath(sess)
	if err != nil {
		errResp(c, http.StatusServiceUnavailable, "LIVE_START_TIMEOUT", "转码启动超时，请重试")
		return
	}
	c.Header("Cache-Control", "no-store") // 动态流绝不缓存：SW/浏览器缓存旧清单会引用已失效分片
	c.Header("Content-Type", "application/vnd.apple.mpegurl")
	if ended {
		// 秒退（源损坏/极短视频）：清单可能未含 ENDLIST 或无内容——让前端走错误提示
		c.Status(http.StatusNoContent)
		return
	}
	c.File(manifest)
}

// Segment GET /media/:id/live/seg/:name —— 分片原样服务（video/mp2t）。
func (h *LiveHandler) Segment(c *gin.Context) {
	id := c.Param("id")
	if _, ok := h.Media.checkReadAccess(c, id); !ok {
		return
	}
	sess, ok := h.Manager.Get(id) // 分片请求只取现有会话，绝不重建
	if !ok {
		errResp(c, http.StatusNotFound, "LIVE_GONE", "转码会话已结束")
		return
	}
	p, ok := h.Manager.SegmentPath(sess, c.Param("name"))
	if !ok {
		errResp(c, http.StatusNotFound, "SEGMENT_NOT_READY", "分片未就绪")
		return
	}
	c.Header("Cache-Control", "no-store") // 动态流绝不缓存：SW/浏览器缓存旧清单会引用已失效分片
	c.Header("Content-Type", "video/mp2t")
	c.File(p)
}

func parsePos(q string) float64 {
	f, err := strconv.ParseFloat(q, 64)
	if err != nil || f < 0 {
		return 0
	}
	return f
}
