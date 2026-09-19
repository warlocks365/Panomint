// Package queue 基于 Valkey（Redis 协议）的任务队列封装。
// 设计依据：TDD v1.1 §3.3 转码管线 / §7 限额；许可：Valkey BSD-3-Clause 替代 Redis（README §三.5 P0）。
// 语义对齐 BullMQ：waiting(列表) / delayed(ZSET) / processing(可靠队列) / failed(死信)。
package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config 队列配置。
type Config struct {
	Addr        string        // VALKEY_ADDR，缺省 localhost:6379
	Password    string        // VALKEY_PASSWORD，可空
	DB          int           // 默认 0
	MaxRetries  int           // 最大重试次数（默认 3，对齐 AC-04）
	BackoffBase time.Duration // 指数退避基数（默认 1s：1s→2s→4s→死信）
}

func (c *Config) withDefaults() {
	if c.Addr == "" {
		c.Addr = os.Getenv("VALKEY_ADDR")
	}
	if c.Addr == "" {
		c.Addr = "localhost:6379"
	}
	if c.Password == "" {
		c.Password = os.Getenv("VALKEY_PASSWORD")
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = 3
	}
	if c.BackoffBase <= 0 {
		c.BackoffBase = time.Second
	}
}

// Job 一个任务。
type Job struct {
	ID       string            `json:"id"`
	Kind     string            `json:"kind"`    // thumbnail|hls|index|ai...
	Payload  map[string]string `json:"payload"` // 如 media_id / profile / input_path
	Attempts int               `json:"attempts"`
	DelayMs  int64             `json:"delay_ms,omitempty"` // 延迟执行
}

// Queue 命名队列。
type Queue struct {
	name string
	rdb  *redis.Client
	cfg  Config
}

// New 创建队列客户端。
func New(name string, cfg Config) *Queue {
	cfg.withDefaults()
	return &Queue{
		name: name,
		cfg:  cfg,
		rdb: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
	}
}

func (q *Queue) key(part string) string { return fmt.Sprintf("q:%s:%s", q.name, part) }

// Ping 连通性检查（AC-01/AC-02 的入口验证）。
func (q *Queue) Ping(ctx context.Context) error { return q.rdb.Ping(ctx).Err() }

// Close 关闭底层连接。
func (q *Queue) Close() error { return q.rdb.Close() }

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Enqueue 入队；DelayMs>0 时进延迟队列。
func (q *Queue) Enqueue(ctx context.Context, job Job) (string, error) {
	if job.ID == "" {
		job.ID = newID()
	}
	data, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	if job.DelayMs > 0 {
		at := time.Now().UnixMilli() + job.DelayMs
		return job.ID, q.rdb.ZAdd(ctx, q.key("delayed"), redis.Z{Score: float64(at), Member: data}).Err()
	}
	return job.ID, q.rdb.LPush(ctx, q.key("waiting"), data).Err()
}

// ErrStop 处理器返回它表示任务永久失败（直接进死信，不重试）。
var ErrStop = errors.New("queue: 永久失败，不重试")

// deadLetterScript 死信路径原子化（P1-03）：按载荷精确 LREM 本任务，成功才 RPUSH 进死信。
// 不能用 LMOVE(processing, failed, LEFT, RIGHT)：BRPOPLPUSH 把本任务压入 processing 头部，
// 但多 worker 并发时别的任务可能随后压到头部，LMOVE 会把**别人的任务**移进死信，
// 而本任务永远卡在 processing。
var deadLetterScript = redis.NewScript(`
local removed = redis.call('LREM', KEYS[1], 1, ARGV[1])
if removed > 0 then
  redis.call('RPUSH', KEYS[2], ARGV[1])
end
return removed
`)

// promoteDelayedScript 到期延迟任务提升原子化（P2-14）：
// 逐条 ZREM→LPUSH 在进程崩溃窗口内会丢任务（ZREM 成功、LPUSH 未执行即退出），
// 单条 Lua 保证「取出+移除+入队」要么全做要么不做。
var promoteDelayedScript = redis.NewScript(`
local items = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
for _, it in ipairs(items) do
  redis.call('ZREM', KEYS[1], it)
  redis.call('LPUSH', KEYS[2], it)
end
return #items
`)

// Handler 任务处理函数；返回 nil=成功(Ack)，ErrStop=死信，其他 error=按指数退避重试。
type Handler func(ctx context.Context, job Job) error

// ConsumeOnce 拉取一个任务并处理（BRPOPLPUSH 可靠模式；block 为阻塞等待上限）。
// 返回是否取到了任务。
func (q *Queue) ConsumeOnce(ctx context.Context, block time.Duration, h Handler) (bool, error) {
	if err := q.PromoteDelayed(ctx); err != nil {
		return false, err
	}
	res, err := q.rdb.BRPopLPush(ctx, q.key("waiting"), q.key("processing"), block).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	job := parseJob(res)
	herr := h(ctx, job)
	switch {
	case herr == nil:
		return true, q.rdb.LRem(ctx, q.key("processing"), 1, res).Err()
	case errors.Is(herr, ErrStop) || job.Attempts >= q.cfg.MaxRetries:
		// 死信（AC-04：重试 ≤ MaxRetries 次后进入死信）；按载荷精确移除，见 deadLetterScript。
		return true, deadLetterScript.Run(ctx, q.rdb,
			[]string{q.key("processing"), q.key("failed")}, res).Err()
	default:
		// 指数退避重试：Attempts+1 后入延迟队列
		job.Attempts++
		delay := q.cfg.BackoffBase << (job.Attempts - 1)
		job.DelayMs = delay.Milliseconds()
		if _, err := q.Enqueue(ctx, job); err != nil {
			return true, err
		}
		return true, q.rdb.LRem(ctx, q.key("processing"), 1, res).Err()
	}
}

// PromoteDelayed 将到期的延迟任务提升回 waiting（Worker 每次拉取前调用）。
func (q *Queue) PromoteDelayed(ctx context.Context) error {
	return promoteDelayedScript.Run(ctx, q.rdb,
		[]string{q.key("delayed"), q.key("waiting")},
		time.Now().UnixMilli()).Err()
}

// Len 各状态任务数（监控用）。
func (q *Queue) Len(ctx context.Context) (waiting, processing, delayed, failed int64, err error) {
	if waiting, err = q.rdb.LLen(ctx, q.key("waiting")).Result(); err != nil {
		return
	}
	if processing, err = q.rdb.LLen(ctx, q.key("processing")).Result(); err != nil {
		return
	}
	if delayed, err = q.rdb.ZCard(ctx, q.key("delayed")).Result(); err != nil {
		return
	}
	failed, err = q.rdb.LLen(ctx, q.key("failed")).Result()
	return
}

// parseJob 反序列化。
func parseJob(s string) Job {
	var j Job
	if err := json.Unmarshal([]byte(s), &j); err != nil {
		return Job{ID: "unparseable", Payload: map[string]string{"raw": s}}
	}
	if j.Payload == nil {
		j.Payload = map[string]string{}
	}
	return j
}
