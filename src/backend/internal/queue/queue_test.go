package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

// 集成测试：直连服务器 Valkey（VALKEY_ADDR=192.168.1.115:6379）。
// Valkey 不可达时整体 SKIP（不影响无网环境跑单测）。

func testQueue(t *testing.T) *Queue {
	t.Helper()
	addr := os.Getenv("VALKEY_ADDR")
	if addr == "" {
		addr = "192.168.1.115:6379"
	}
	q := New("test:"+fmt.Sprint(time.Now().UnixNano()), Config{Addr: addr, BackoffBase: 50 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := q.Ping(ctx); err != nil {
		t.Skipf("Valkey 不可达（%s）: %v", addr, err)
	}
	t.Cleanup(func() {
		// 清理测试键
		q.rdb.Del(context.Background(),
			q.key("waiting"), q.key("processing"), q.key("delayed"), q.key("failed"))
		q.Close()
	})
	return q
}

// TestProtocolCompat AC-02：基础命令与 Redis 协议一致（PING/SET/GET/DEL/HSET/HGET）。
func TestProtocolCompat(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	if err := q.rdb.Set(ctx, q.key("proto"), "v1", 0).Err(); err != nil {
		t.Fatalf("SET: %v", err)
	}
	got, err := q.rdb.Get(ctx, q.key("proto")).Result()
	if err != nil || got != "v1" {
		t.Fatalf("GET = %q, %v", got, err)
	}
	if err := q.rdb.HSet(ctx, q.key("proto_h"), "f", "1").Err(); err != nil {
		t.Fatalf("HSET: %v", err)
	}
	hv, _ := q.rdb.HGet(ctx, q.key("proto_h"), "f").Result()
	if hv != "1" {
		t.Fatalf("HGET = %q", hv)
	}
	q.rdb.Del(ctx, q.key("proto"), q.key("proto_h"))
}

// TestEnqueueConsume AC-03：入队后 2s 内被消费。
func TestEnqueueConsume(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	id, err := q.Enqueue(ctx, Job{Kind: "thumbnail", Payload: map[string]string{"media_id": "m1"}})
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	start := time.Now()
	got, err := q.ConsumeOnce(ctx, 2*time.Second, func(_ context.Context, j Job) error {
		if j.ID != id || j.Kind != "thumbnail" || j.Payload["media_id"] != "m1" {
			t.Errorf("任务内容不符: %+v", j)
		}
		return nil
	})
	if err != nil || !got {
		t.Fatalf("ConsumeOnce got=%v err=%v", got, err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("消费超时 >2s")
	}
	// ack 后 processing 应为空
	_, proc, _, _, _ := q.Len(ctx)
	if proc != 0 {
		t.Fatalf("processing 残留 %d", proc)
	}
}

// TestRetryThenDead AC-04：失败后指数退避重试，超过 MaxRetries 进死信。
func TestRetryThenDead(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	q.cfg.MaxRetries = 2

	if _, err := q.Enqueue(ctx, Job{Kind: "boom"}); err != nil {
		t.Fatal(err)
	}

	var attempts int64
	consume := func() {
		ok, err := q.ConsumeOnce(ctx, 300*time.Millisecond, func(_ context.Context, j Job) error {
			atomic.AddInt64(&attempts, 1)
			return errors.New("模拟失败")
		})
		if err != nil {
			t.Fatalf("ConsumeOnce: %v", err)
		}
		if !ok {
			t.Log("本轮无任务（延迟中）")
		}
	}

	consume()                     // 第 1 次：失败 → attempts=1，延迟 50ms
	time.Sleep(120 * time.Millisecond) // 等延迟到期
	consume()                     // 第 2 次：失败 → attempts=2，延迟 100ms
	time.Sleep(200 * time.Millisecond)
	consume()                     // 第 3 次：attempts=2 >= MaxRetries=2 → 死信

	if got := atomic.LoadInt64(&attempts); got != 3 {
		t.Fatalf("处理次数 = %d，期望 3（首试 + 2 次重试）", got)
	}
	_, _, _, failed, _ := q.Len(ctx)
	if failed != 1 {
		t.Fatalf("死信数 = %d，期望 1", failed)
	}
}

// TestDelayedJob 延迟任务到期后才可被消费。
func TestDelayedJob(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()

	if _, err := q.Enqueue(ctx, Job{Kind: "later", DelayMs: 200}); err != nil {
		t.Fatal(err)
	}
	// 立即拉取应无任务（200ms 内阻塞返回空）
	ok, err := q.ConsumeOnce(ctx, 50*time.Millisecond, func(context.Context, Job) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("延迟任务被提前消费")
	}
	// 到期后应可消费
	time.Sleep(250 * time.Millisecond)
	ok, err = q.ConsumeOnce(ctx, time.Second, func(context.Context, Job) error { return nil })
	if err != nil || !ok {
		t.Fatalf("延迟任务到期未被消费: got=%v err=%v", ok, err)
	}
}

// TestRateLimiter 令牌桶：容量内放行，耗尽后拒绝，等待 refill 后恢复。
func TestRateLimiter(t *testing.T) {
	q := testQueue(t)
	ctx := context.Background()
	rl := q.NewRateLimiter()
	key := fmt.Sprintf("test:%d", time.Now().UnixNano())
	defer q.rdb.Del(ctx, "rl:"+key)

	// 容量 5，每秒补 5
	for i := 0; i < 5; i++ {
		ok, err := rl.Allow(ctx, key, 5, 5, 1)
		if err != nil || !ok {
			t.Fatalf("第 %d 次应放行: ok=%v err=%v", i+1, ok, err)
		}
	}
	if ok, _ := rl.Allow(ctx, key, 5, 5, 1); ok {
		t.Fatal("第 6 次应拒绝（桶已空）")
	}
	time.Sleep(300 * time.Millisecond) // 补 1.5 个
	if ok, _ := rl.Allow(ctx, key, 5, 5, 1); !ok {
		t.Fatal("refill 后应放行")
	}
}
