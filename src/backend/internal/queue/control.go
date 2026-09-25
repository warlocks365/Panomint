package queue

// 排障/控制向的 Valkey 原语（Job000121 调试通道绑定用）。
// 与 ConsumeOnce 的既有语义同一真源：移出 waiting 用「按载荷精确 LREM」原子脚本，
// 绝不引入第二套队列语义。

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// removeWherePayloadScript 从 waiting 列表移除**第一个** payload[field]==value 的任务。
// 逐条扫描 + 精确 LREM 单条 Lua 原子执行：解析失败/不匹配的条目原样保留。
// 返回 1 表示移除成功，0 表示未找到（调用方按"已被 worker 领走"处理）。
var removeWherePayloadScript = redis.NewScript(`
local items = redis.call('LRANGE', KEYS[1], 0, -1)
for _, it in ipairs(items) do
  local ok, obj = pcall(cjson.decode, it)
  if ok and type(obj) == 'table' and type(obj.payload) == 'table'
     and obj.payload[ARGV[1]] == ARGV[2] then
    redis.call('LREM', KEYS[1], 1, it)
    return 1
  end
end
return 0
`)

// RemoveFromWaiting 从本队列 waiting 列表移除第一个 payload[field]==value 的任务。
// 供调试通道 pause/cancel 使用（设计 §1.1：绑定既有队列语义，不平行造第二套控制面）。
// 返回 removed=false 表示列表中已无该任务（典型情形：worker 已领走进入 processing）。
func (q *Queue) RemoveFromWaiting(ctx context.Context, field, value string) (bool, error) {
	n, err := removeWherePayloadScript.Run(ctx, q.rdb,
		[]string{q.key("waiting")}, field, value).Int64()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// Counter 计数器（INCR + 首次 EXPIRE 窗口），返回窗口内累计值。
// 专供"失败 N 次锁 M 分钟"类语义（调试握手失败锁定）；键空间由调用方自负（rl: 前缀）。
// 窗口 TTL 设置失败时删除键并返回错误——宁可计数作废也不留"永不过期的计数器"。
func (q *Queue) Counter(ctx context.Context, key string, window time.Duration) (int64, error) {
	n, err := q.rdb.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		if err := q.rdb.Expire(ctx, key, window).Err(); err != nil {
			_ = q.rdb.Del(ctx, key).Err()
			return 0, err
		}
	}
	return n, nil
}

// SetLocked 设置锁键（SET key 1 NX EX ttl），返回是否设置成功。
// 与 Counter 配套：触顶后置锁，锁定期内一切握手直接拒绝。
func (q *Queue) SetLocked(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return q.rdb.SetNX(ctx, key, 1, ttl).Result()
}

// Locked 查询锁键是否存在（锁定期内 true）。键不存在不视为错误。
func (q *Queue) Locked(ctx context.Context, key string) (bool, error) {
	n, err := q.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
