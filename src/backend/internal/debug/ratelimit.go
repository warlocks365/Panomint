package debug

// 握手限流与失败锁定（设计 §6.5）。
//
// 双控制：
//   - 握手专用令牌桶 rl:debug:hs:<ip>：突发 10、持续 0.2/s —— 与全站 60 桶完全隔离
//     （复用 queue.RateLimiter，键前缀自带 rl:）；
//   - 认证失败计数 rl:debug:fail:<ip>：5 次/10 分钟触顶 → 锁 rl:debug:lock:<ip> 15 分钟。
//
// ⚠️ fail-open 决策（设计 §6.5）：Valkey 不可用时一律放行认证/限流检查——256-bit 密钥使
// 在线爆破无意义，限流只是纵深一层，不为限流可用性牺牲接入可用性。

import (
	"context"
	"time"

	"panoalbum/internal/queue"
)

// 失败锁定参数（设计 §6.5 表）。
const (
	failMaxAttempts = 5                // 触顶次数
	failWindow      = 10 * time.Minute // 计数窗口
	lockDuration    = 15 * time.Minute // 锁定时长
)

// FailLocker 认证失败锁定（Valkey 计数；不可用 fail-open）。
type FailLocker struct {
	Q *queue.Queue
}

func failCountKey(ip string) string { return "debug:fail:" + ip }
func failLockKey(ip string) string  { return "debug:lock:" + ip }

// Locked 报告该 IP 是否处于锁定期。Valkey 故障返回 (false, err)——调用方 fail-open。
func (f *FailLocker) Locked(ctx context.Context, ip string) (bool, error) {
	return f.Q.Locked(ctx, failLockKey(ip))
}

// RegisterFailure 失败计数 +1；触顶置锁。返回 (是否触顶置锁, error)。
// Valkey 故障返回 (false, err)——调用方 fail-open（不阻断握手，也不误锁）。
func (f *FailLocker) RegisterFailure(ctx context.Context, ip string) (bool, error) {
	n, err := f.Q.Counter(ctx, failCountKey(ip), failWindow)
	if err != nil {
		return false, err
	}
	if n < failMaxAttempts {
		return false, nil
	}
	// 触顶：置锁（幂等；重复置锁刷新 TTL 无妨——本来就要求锁定期内全拒）
	return f.Q.SetLocked(ctx, failLockKey(ip), lockDuration)
}

// CheckHandshake 握手专用桶：突发 burst、持续 rate/s。返回 allowed。
// Valkey 故障返回 (true, err)——调用方 fail-open。
func CheckHandshake(ctx context.Context, rl *queue.RateLimiter, ip string) (bool, error) {
	return rl.Allow(ctx, "debug:hs:"+ip, 10, 0.2, 1)
}
