package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/queue"
)

// RateLimit 令牌桶限流（TDD §7：登录/IP/API；复用 T0.3 RateLimiter）。
// key 维度：客户端 IP；capacity 突发上限，ratePerSec 持续速率。
func RateLimit(rl *queue.RateLimiter, capacity, ratePerSec float64) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := rl.Allow(c.Request.Context(), "api:ip:"+c.ClientIP(), capacity, ratePerSec, 1)
		if err != nil {
			// 限流器故障时放行（可用性优先），记录到上下文供日志观测
			c.Set("ratelimit_error", err.Error())
			c.Next()
			return
		}
		if !ok {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{"code": "RATE_LIMITED", "message": "请求过于频繁，请稍后重试"},
			})
			return
		}
		c.Next()
	}
}
