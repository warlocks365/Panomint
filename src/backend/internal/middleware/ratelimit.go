package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
	"panoalbum/internal/queue"
)

// RateLimit 令牌桶限流（TDD §7：登录/IP/API；复用 T0.3 RateLimiter）。
// key 维度：客户端 IP；capacity 突发上限，ratePerSec 持续速率。
// 1002 用户裁决：浏览类缩略图（GET */thumb）豁免限流——地图/网格页单屏并发
// 拉数十张缩略图会耗尽全局桶（实测 429×43）；thumb 仍有 JWT 鉴权与 mediascope
// 可见性校验，豁免仅放宽频控、不降低鉴权强度。
func RateLimit(rl *queue.RateLimiter, capacity, ratePerSec float64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// ⚠️ 豁免名单是**安全边界**，改动必须走 review（Job000141 加护栏）。
		// 缩略图有 JWT + mediascope 双重鉴权，豁免的只是频控（实测地图/网格页
		// 单屏并发拉数十张会把 60 令牌桶打满，429×43，1002 用户裁决放宽）。
		//
		// **禁止**把 /dav 加入豁免：WebDAV 走 HTTP Basic（internal/media/dav.go:63），
		// 按 IP 限流是它唯一的暴力破解防护——DAV 每请求做一次 bcrypt（本就很慢），
		// 一旦豁免就等于对公网开放无速率限制的口令尝试面。
		//
		// 若将来某端点确需豁免（如实测被误伤），判据是：该端点有**等强度的
		// 鉴权**且**不涉及凭据尝试**，并在此处写明理由。
		if c.Request.Method == http.MethodGet && strings.HasSuffix(c.Request.URL.Path, "/thumb") {
			c.Next()
			return
		}
		ok, err := rl.Allow(c.Request.Context(), "api:ip:"+c.ClientIP(), capacity, ratePerSec, 1)
		if err != nil {
			// 限流器故障时放行（可用性优先），记录到上下文供日志观测
			c.Set("ratelimit_error", err.Error())
			c.Next()
			return
		}
		if !ok {
			httperr.Abort(c, http.StatusTooManyRequests, "RATE_LIMITED", "请求过于频繁，请稍后重试")
			return
		}
		c.Next()
	}
}
