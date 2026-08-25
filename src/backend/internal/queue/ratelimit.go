package queue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// 令牌桶限流（TDD §7 限额：登录/IP/API）。
// Lua 原子执行：读- refill- 扣减- 写回，避免并发竞态。
var tokenBucketScript = redis.NewScript(`
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])       -- 每秒补充令牌数
local requested = tonumber(ARGV[3])
local now = tonumber(ARGV[4])        -- 毫秒时间戳

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = capacity
local ts = now
if data[1] then
  tokens = tonumber(data[1])
  ts = tonumber(data[2])
end

-- 按时间差补充令牌
tokens = math.min(capacity, tokens + (now - ts) / 1000 * rate)

local allowed = 0
if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
end

redis.call('HMSET', key, 'tokens', tokens, 'ts', now)
redis.call('PEXPIRE', key, math.ceil(capacity / rate * 1000 * 2))
return {allowed, math.floor(tokens)}
`)

// RateLimiter 分布式令牌桶。
type RateLimiter struct {
	rdb *redis.Client
}

// NewRateLimiter 基于既有队列连接创建限流器。
func (q *Queue) NewRateLimiter() *RateLimiter { return &RateLimiter{rdb: q.rdb} }

// Allow 尝试消耗 n 个令牌；key 例如 "login:ip:1.2.3.4"。
// capacity=桶容量（突发上限），ratePerSec=每秒补充速率。
func (rl *RateLimiter) Allow(ctx context.Context, key string, capacity, ratePerSec, n float64) (bool, error) {
	res, err := tokenBucketScript.Run(ctx, rl.rdb, []string{"rl:" + key},
		capacity, ratePerSec, n, time.Now().UnixMilli()).Int64Slice()
	if err != nil {
		return false, err
	}
	return res[0] == 1, nil
}
