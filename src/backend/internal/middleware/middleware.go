// Package middleware gin 中间件五件套 + JWT 占位。
// 依据：TDD §7（链路追踪 request_id / 限额）§8.2（结构化 JSON 日志字段）。
package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"panoalbum/internal/httperr"
)

// RequestID 注入/透传 X-Request-ID（TDD §8.2 链路追踪贯穿 Caddy→API→日志）。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			b := make([]byte, 8)
			_, _ = rand.Read(b)
			id = hex.EncodeToString(b)
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// Recovery panic 恢复 + 结构化错误日志。
func Recovery(log *zap.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		log.Error("panic recovered",
			zap.Any("error", err),
			zap.String("request_id", c.GetString("request_id")),
			zap.String("path", c.Request.URL.Path),
		)
		httperr.Abort(c, http.StatusInternalServerError, "INTERNAL", "服务器内部错误")
	})
}

// Logging 结构化访问日志（TDD §8.2 字段：level/msg/request_id/user_id/module/latency）。
func Logging(log *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http",
			zap.String("request_id", c.GetString("request_id")),
			zap.String("user_id", c.GetString("user_id")), // T1.3 鉴权后填充
			zap.String("module", "api"),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Int64("latency_ms", time.Since(start).Milliseconds()),
			zap.String("ip", c.ClientIP()),
		)
	}
}

// CORS 跨域（白名单来自 CORS_ORIGINS）。
func CORS(origins []string) gin.HandlerFunc {
	allow := map[string]bool{}
	for _, o := range origins {
		allow[strings.TrimSpace(o)] = true
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if allow[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-ID,Content-Range")
			c.Header("Access-Control-Max-Age", "86400")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// AuthPlaceholder JWT 占位中间件（T1.3 实现完整验证；本轮仅透传并预留 user_id 注入点）。
// 说明：不拦截任何请求；仅在存在 Bearer 头时记录其存在，供日志字段对齐。
func AuthPlaceholder() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
			c.Set("has_bearer", true) // T1.3：解析 JWT → c.Set("user_id", sub)
		}
		c.Next()
	}
}
