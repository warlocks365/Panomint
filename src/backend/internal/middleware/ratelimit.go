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
