package media

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

// Thumb GET /media/:id/thumb?size=sm|md|lg —— 缩略图服务（G2 缺口补齐）。
// 从 DB 读缩略图文件名，THUMB_DIR（默认 ./testdata/thumbnails）解析落盘文件。
//
// 归属校验（越权修复）：缩略图是**二进制媒体内容**，必须走与 GET /media/:id 同一套
// 可见性判定；此前这里只按 id 取列、不看归属，于是它成了绕过既有保护的侧门
// （已由第二个账号实测：viewer 对他人 media id 拿到 200 + image/webp 7474 字节，
// 而同一个 id 走 GET /media/:id 是 403「无权访问该媒体」；media id 可从 /geo/items
// 批量获取，构成完整的「枚举 → 取图」链路）。判定复用 checkAccess 的同一对原语：
// Store.ownerOf（属主） + canAccess（本人/owner/admin），不另造一套。
func (h *Handler) Thumb(c *gin.Context) {
	size := c.DefaultQuery("size", "md")
	col := map[string]string{"sm": "thumbnail_sm", "md": "thumbnail_md", "lg": "thumbnail_lg"}[size]
	if col == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "BAD_SIZE", "message": "size 仅支持 sm|md|lg"}})
		return
	}
	// 先判归属：不存在与无权一律落到**同一个** 404（见 thumbAccess / writeThumbAccessError）。
	if !h.thumbAccess(c, c.Param("id")) {
		return
	}
	var name *string
	if err := h.Store.Pool.QueryRow(c.Request.Context(),
		`SELECT `+col+` FROM media WHERE id = $1 AND deleted_at IS NULL`, c.Param("id")).Scan(&name); err != nil || name == nil || *name == "" {
		thumbNotFound(c)
		return
	}
	// 防路径穿越
	base := filepath.Base(*name)
	dir := os.Getenv("THUMB_DIR")
	if dir == "" {
		dir = "./testdata/thumbnails"
	}
	path := filepath.Join(dir, base)
	if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(dir)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "BAD_PATH", "message": "非法路径"}})
		return
	}
	// 文件缺失时 c.File 会回落 net/http 默认纯文本 404；先 Stat 返回统一 JSON（与 download 的 FILE_MISSING 对齐）
	if _, err := os.Stat(path); err != nil {
		errResp(c, http.StatusNotFound, "FILE_MISSING", "文件不在磁盘上")
		return
	}
	c.Header("Cache-Control", "public, max-age=86400")
	c.File(path)
}

// mediaOwnerLookup 缩略图归属判定所需的唯一 DB 能力。
// 签名与 Store.ownerOf 逐字相同，故 *Store 的方法值可直接传入；抽成函数类型只是为了让
// 「不存在 与 无权 不可区分」这条安全性质能被单测直接钉住（无需真库）。
type mediaOwnerLookup func(ctx context.Context, id string) (ownerID string, deleted bool, err error)

// thumbAccessCheck 缩略图归属判定。返回 nil 表示放行；**ErrNotFound 表示「不存在或无权」**。
//
// 口径与 checkAccess 完全一致（同一个 ownerOf + 同一个 canAccess），唯一差别是把
// 「无权」也收敛成 ErrNotFound —— 缩略图是二进制资源，若对无权返回 403（或任何可与
// 「不存在」区分的响应），就等于给攻击者一个「该 id 存在且属于他人」的存在性预言机。
// DB 错误原样透出、不折叠成 404，由调用方映射为 500（否则故障会被伪装成「不存在」）。
func thumbAccessCheck(ctx context.Context, lookup mediaOwnerLookup, userID, role, id string) error {
	ownerID, _, err := lookup(ctx, id)
	if err != nil {
		return err
	}
	if !canAccess(userID, role, ownerID) {
		return ErrNotFound
	}
	return nil
}

// thumbNotFound 缩略图统一的「不存在」响应。
//
// **不存在**、**无权**、**有行但无缩略图**三条分支必须共用本函数，保证逐字节同形；
// 任何一条改成别的状态码或文案都会重新打开存在性预言机。
func thumbNotFound(c *gin.Context) {
	errResp(c, http.StatusNotFound, "NOT_FOUND", "缩略图不存在")
}

// thumbAccess 判定调用者能否读该媒体的缩略图；不可读时已写出响应，返回 false。
func (h *Handler) thumbAccess(c *gin.Context, id string) bool {
	err := thumbAccessCheck(c.Request.Context(), h.Store.ownerOf, c.GetString("user_id"), c.GetString("role"), id)
	if err == nil {
		return true
	}
	writeThumbAccessError(c, err)
	return false
}

// writeThumbAccessError 把归属判定的失败映射为响应：ErrNotFound → 404（与「不存在」同形）；
// 其余（DB 错误）→ 500。**绝不产出 403。**
func writeThumbAccessError(c *gin.Context, err error) {
	if errors.Is(err, ErrNotFound) {
		thumbNotFound(c)
		return
	}
	errResp(c, http.StatusInternalServerError, "QUERY_FAILED", err.Error())
}
