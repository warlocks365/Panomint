package httpsconfig

// ForceHTTPS 中间件（Job000125）：强制 HTTPS（HTTP → HTTPS 301 跳转）。
//
// 判定依据是反代透传的 **X-Forwarded-Proto**（Caddyfile.tls 已 header_up 透传），
// 而不是 c.Request.TLS —— 应用容器自身永远不终结 TLS，后者恒为 nil。
//
// 三条刻意保守的边界（防「开了开关整个站打不开」）：
//   - XFP 缺失或非 http/https（无反代的裸 HTTP 部署、内部探针、gRPC 等奇怪值）→ **不拦**。
//     否则「前面没有 TLS 代理却开了强制跳转」会造成 301 → https 无监听 → 全站不可达死局；
//   - /health、/ready 豁免：探活方（巡检 cron / 编排）不跟随重定向，拦了会把
//     「配置写坏了」误报成「服务挂了」；
//   - 开关读取失败沿用上次值（60s 缓存 + 10s 重试），初始 false = 历史行为：
//     配置面故障时退回「不跳转」永远安全（跳转是增量行为，不是既有行为的依赖）。

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	forceTTL      = 60 * time.Second
	forceRetryTTL = 10 * time.Second
)

// forceCache force_https 开关的进程内缓存（60s）。
type forceCache struct {
	mu      sync.Mutex
	pool    *pgxpool.Pool
	value   bool
	expires time.Time
}

func (c *forceCache) get(ctx context.Context) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.value
	}
	cfg, err := Get(ctx, c.pool)
	if err != nil || cfg == nil {
		log.Printf("[httpsconfig] 读 force_https 失败，沿用 %v（%ds 后重试）: %v",
			c.value, int(forceRetryTTL.Seconds()), err)
		c.expires = time.Now().Add(forceRetryTTL)
		return c.value
	}
	c.value = cfg.ForceHTTPS
	c.expires = time.Now().Add(forceTTL)
	return c.value
}

// sharedForceCache 包级单例：进程内只有一个 api 实例、一个中间件挂载点与一个
// 写配置的 Handler —— 共享同一缓存，PutConfig 写成功后经 InvalidateForceCache
// 失效，让开关变更绕过 60s TTL **立即生效**（「热生效」的语义保证；否则管理端
// 打开开关后最长 1 分钟内中间件仍按旧值放行，e2e 实测踩出）。
var sharedForceCache forceCache

// ForceHTTPS 构造生产用中间件（pool 读库 + 缓存）。
func ForceHTTPS(pool *pgxpool.Pool) gin.HandlerFunc {
	sharedForceCache.mu.Lock()
	sharedForceCache.pool = pool
	sharedForceCache.mu.Unlock()
	return forceHTTPSWith(sharedForceCache.get)
}

// InvalidateForceCache 令共享缓存立即过期（下次请求重读库）。
// 纯时间戳操作，无 DB 依赖，可直接单测。
func InvalidateForceCache() {
	sharedForceCache.mu.Lock()
	sharedForceCache.expires = time.Time{}
	sharedForceCache.mu.Unlock()
}

// forceHTTPSWith 可注入开关读取器的中间件（测试用）。
func forceHTTPSWith(read func(ctx context.Context) bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !read(c.Request.Context()) {
			c.Next()
			return
		}
		proto := c.GetHeader("X-Forwarded-Proto")
		if proto != "http" {
			// https = 已是安全请求；空/其他 = 判定依据不可信，不拦（见文件头第 2 条边界）。
			c.Next()
			return
		}
		switch c.Request.URL.Path {
		case "/health", "/ready":
			c.Next()
			return
		}
		target := "https://" + c.Request.Host + c.Request.URL.RequestURI()
		c.Redirect(http.StatusMovedPermanently, target)
		c.Abort()
	}
}
