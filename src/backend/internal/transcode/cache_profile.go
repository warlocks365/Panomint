package transcode

// HLS 缓存策略消费（Job000125）：ServeHLS 按系统配置的 hls_cache_profile 输出 Cache-Control。
//
// 配置读取带 **60s 进程内缓存**：HLS 分片是播放热路径（一次播放拉几十上百个 ts），
// 逐请求查库不可接受；60s 的生效延迟对「缓存档切换」这类低频运维动作完全可接受。
// 查询失败沿用上次值（缓存永久续期 10s 重试）：配置读不到不应让分片 500——
// 与 GetSystemConfig 的 fail-closed（写侧闸门）不同，读侧此处**可用性优先**，
// 因为 balanced 兜底值就是历史行为，退回它永远是安全的。

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	cacheProfileTTL      = 60 * time.Second
	cacheProfileRetryTTL = 10 * time.Second
)

// hlsCacheProfileCache 系统配置 hls_cache_profile 的进程内缓存（单行表的读侧镜像）。
type hlsCacheProfileCache struct {
	mu      sync.Mutex
	pool    *pgxpool.Pool
	value   string
	expires time.Time
}

// newHLSCacheProfileCache 构造缓存；初始值 = 缺省档（与 DDL DEFAULT 一致）。
func newHLSCacheProfileCache(pool *pgxpool.Pool) *hlsCacheProfileCache {
	return &hlsCacheProfileCache{pool: pool, value: DefaultHLSCacheProfile}
}

// get 返回当前缓存档；到期后回库刷新，失败沿用旧值并缩短下次重试间隔。
func (c *hlsCacheProfileCache) get(ctx context.Context) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.expires) {
		return c.value
	}
	v, err := GetSystemConfig(ctx, c.pool)
	if err != nil || v == nil {
		log.Printf("[transcode] 读 hls_cache_profile 失败，沿用 %q（%ds 后重试）: %v",
			c.value, int(cacheProfileRetryTTL.Seconds()), err)
		c.expires = time.Now().Add(cacheProfileRetryTTL)
		return c.value
	}
	if validHLSCacheProfiles[v.HLSCacheProfile] {
		c.value = v.HLSCacheProfile
	} else {
		log.Printf("[transcode] 库中 hls_cache_profile=%q 非法，回退 %q", v.HLSCacheProfile, DefaultHLSCacheProfile)
		c.value = DefaultHLSCacheProfile
	}
	c.expires = time.Now().Add(cacheProfileTTL)
	return c.value
}

// HLSCacheControl 纯函数：缓存档 + 是否 master 清单 → Cache-Control 头值。
//
//	balanced（默认/未知值兜底）= 历史行为：m3u8 no-cache、ts 一年 immutable；
//	aggressive = 只读归档：m3u8 可短缓存 60s（清单仍会到期刷新）、ts 一年 immutable；
//	no_cache   = 调试：m3u8 no-cache、ts 仅 300s（产物可能被覆盖重转）。
//
// ts 的 immutable 语义依据：分片按媒体 id + 序号寻址，转码重来会先清目录再全量重写，
// 同名分片内容稳定的假设在 no_cache 档被明确放弃（这正是它存在的意义）。
func HLSCacheControl(profile string, isMaster bool) string {
	switch profile {
	case "aggressive":
		if isMaster {
			return "public, max-age=60"
		}
		return "public, max-age=31536000, immutable"
	case "no_cache":
		if isMaster {
			return "no-cache"
		}
		return "public, max-age=300"
	default: // balanced 与一切未知值
		if isMaster {
			return "no-cache"
		}
		return "public, max-age=31536000, immutable"
	}
}
