package shares

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// ogData OG 页面渲染数据。
//
// 社交平台的抓取器（微信/微博/Twitter/Slack…）**不执行 JS**，只在**初始 HTML** 里读
// og:* 标签，因此分享页的富卡片必须由服务端渲染，不能依赖 Vue SPA。
type ogData struct {
	Title       string
	Description string
	ImageURL    string // 空串 = 不输出 og:image / twitter:image
	PageURL     string // 对外页面地址（/share/<token>），用于 og:url
	NoIndex     bool
}

// ogHTML 最简 OG 宿主页：无 JS、无外链，仅为抓取器与「误点进来的人」服务。
const ogHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Title}}</title>
{{- if .NoIndex}}
<meta name="robots" content="noindex">
{{- end}}
<meta property="og:type" content="website">
<meta property="og:site_name" content="全景相册">
<meta property="og:title" content="{{.Title}}">
<meta property="og:description" content="{{.Description}}">
<meta property="og:url" content="{{.PageURL}}">
{{- if .ImageURL}}
<meta property="og:image" content="{{.ImageURL}}">
{{- end}}
<meta name="twitter:card" content="{{if .ImageURL}}summary_large_image{{else}}summary{{end}}">
<meta name="twitter:title" content="{{.Title}}">
<meta name="twitter:description" content="{{.Description}}">
{{- if .ImageURL}}
<meta name="twitter:image" content="{{.ImageURL}}">
{{- end}}
</head>
<body style="font-family:system-ui,-apple-system,'Segoe UI',sans-serif;padding:24px;line-height:1.7">
<h1 style="font-size:18px;margin:0 0 8px">{{.Title}}</h1>
<p style="margin:0 0 16px;color:#666">{{.Description}}</p>
<p style="margin:0"><a href="{{.PageURL}}">在浏览器中打开</a></p>
</body>
</html>
`

var ogTmpl = template.Must(template.New("share-og").Parse(ogHTML))

// originOf 由请求头拼出绝对站点前缀（scheme://host）。
//
// 分享链接有三种访问入口（内网域名 panomint.warlocks.cn / 局域网 IP:8088 / 隧道
// *.trycloudflare.com），**绝不能写死**：host 一律取请求 Host，scheme 优先取反向代理
// 透传的 X-Forwarded-Proto（Caddy/cloudflared 会置为 https），缺失时才退回连接本身。
func originOf(c *gin.Context) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	return scheme + "://" + c.Request.Host
}

// renderOG 渲染并写出 OG HTML。
func (h *Handler) renderOG(c *gin.Context, status int, d ogData) {
	if d.PageURL == "" {
		d.PageURL = originOf(c) + "/share/" + c.Param("token")
	}
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=300")
	c.Status(status)
	_ = ogTmpl.Execute(c.Writer, d)
}

// PublicOG GET /public/shares/:token/og
//
// 分享页 OG 封面（服务端渲染）。nginx 按 User-Agent 把抓取器分流到本端点，
// 普通浏览器仍走 SPA（/share/<token> → index.html），互不影响。
//
// 两条与安全/统计相关的硬约束：
//  1. **不写 share_access_log、不自增 access_count** —— 抓取器拉一次 OG 不是一次真实访问，
//     否则 max_views 分享会被爬虫提前烧完。
//  2. **受密码保护的分享一律返回中性 OG**（无标题、无缩略图）。理由：OG HTML 会被社交平台
//     缓存/公开分发，若靠 ?password= 解锁则等于把密码写进 URL 并交给第三方 CDN；
//     中性卡片是唯一不绕过密码的做法。
func (h *Handler) PublicOG(c *gin.Context) {
	ctx := c.Request.Context()
	token := c.Param("token")
	pageURL := originOf(c) + "/share/" + token

	sh, err := h.Store.GetByToken(ctx, token)
	if errors.Is(err, ErrNotFound) {
		h.renderOG(c, http.StatusNotFound, ogData{
			Title:       "分享不存在",
			Description: "该分享链接不存在或已被分享者吊销。",
			PageURL:     pageURL,
			NoIndex:     true,
		})
		return
	}
	if err != nil {
		h.renderOG(c, http.StatusInternalServerError, ogData{
			Title:       "全景相册分享",
			Description: "暂时无法加载分享信息，请在浏览器中重试。",
			PageURL:     pageURL,
			NoIndex:     true,
		})
		return
	}

	// 有效期 / 访问次数上限：与公开端点同规则（但不计一次访问）
	now := time.Now()
	if sh.ExpireAt != nil && !now.Before(*sh.ExpireAt) {
		h.renderOG(c, http.StatusOK, ogData{
			Title:       "分享已过期",
			Description: "该链接已超过有效期，请联系分享者重新分享。",
			PageURL:     pageURL,
			NoIndex:     true,
		})
		return
	}
	if sh.MaxViews != nil && sh.AccessCount >= *sh.MaxViews {
		h.renderOG(c, http.StatusOK, ogData{
			Title:       "访问次数已达上限",
			Description: "该分享的浏览次数已用完，请联系分享者。",
			PageURL:     pageURL,
			NoIndex:     true,
		})
		return
	}

	// 密码保护 → 中性 OG（不泄露标题与缩略图）
	if sh.PasswordHash != nil && *sh.PasswordHash != "" {
		h.renderOG(c, http.StatusOK, ogData{
			Title:       "全景相册分享",
			Description: "该分享受访问密码保护，请在浏览器中打开并输入密码查看。",
			PageURL:     pageURL,
		})
		return
	}

	coverID, total, hasPano, err := h.Store.OGCover(ctx, sh)
	if err != nil {
		h.renderOG(c, http.StatusOK, ogData{
			Title:       "全景相册分享",
			Description: "暂时无法加载分享内容，请在浏览器中打开。",
			PageURL:     pageURL,
			NoIndex:     true,
		})
		return
	}

	desc := fmt.Sprintf("%d 项内容 · 在线浏览", total)
	switch {
	case total == 0:
		desc = "分享内容为空"
	case hasPano:
		desc = fmt.Sprintf("%d 项内容 · 含 360 全景 · 在线浏览", total)
	}

	// og:image 必须是**绝对 URL**，且由请求 Host 动态拼出；封面取分享内第一个可展示媒体的缩略图
	imageURL := ""
	if coverID != "" {
		imageURL = originOf(c) + "/public/shares/" + token + "/media/" + coverID + "/thumb?size=md"
	}

	h.renderOG(c, http.StatusOK, ogData{
		Title:       h.shareTitle(ctx, sh),
		Description: desc,
		ImageURL:    imageURL,
		PageURL:     pageURL,
	})
}

// shareTitle OG 标题：分享自定义标题 → 相册名 → 通用兜底。
func (h *Handler) shareTitle(ctx context.Context, sh *Share) string {
	if sh.Title != nil {
		if t := strings.TrimSpace(*sh.Title); t != "" {
			return t
		}
	}
	if sh.Kind == "album" {
		if name, err := h.Store.AlbumName(ctx, sh.TargetID); err == nil && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	return "全景相册分享"
}

// OGCover 分享封面：返回第一个「有缩略图」的媒体 id，另给出条目总数与是否含 360。
// 已删除媒体由 ListItems 过滤，不会成为封面。
func (s *Store) OGCover(ctx context.Context, sh *Share) (coverID string, total int, hasPano bool, err error) {
	items, err := s.ListItems(ctx, sh)
	if err != nil {
		return "", 0, false, err
	}
	total = len(items)
	for _, it := range items {
		if it.Is360 {
			hasPano = true
		}
	}
	// 优先中图（1280 宽，微信卡片足够且体积可控），退回小图
	for _, it := range items {
		if it.ThumbnailMD != nil && *it.ThumbnailMD != "" {
			return it.ID, total, hasPano, nil
		}
	}
	for _, it := range items {
		if it.ThumbnailSM != nil && *it.ThumbnailSM != "" {
			return it.ID, total, hasPano, nil
		}
	}
	return "", total, hasPano, nil
}

// AlbumName 相册名（OG 标题兜底；只取 name，不拉整册条目）。
func (s *Store) AlbumName(ctx context.Context, albumID string) (string, error) {
	var name string
	err := s.Pool.QueryRow(ctx, `SELECT name FROM albums WHERE id = $1`, albumID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTargetLost
	}
	return name, err
}
