package watch

import (
	"sync"
	"testing"
	"time"
)

// TestDebouncerCoalesces 去抖：静默期内连续 Add 只触发一次 flush，且为路径并集。
func TestDebouncerCoalesces(t *testing.T) {
	var mu sync.Mutex
	var batches [][]string
	d := NewDebouncer(80*time.Millisecond, func(paths []string) {
		mu.Lock()
		batches = append(batches, paths)
		mu.Unlock()
	})
	defer d.Stop()

	// 模拟写入风暴：同一文件多次事件 + 其他文件，间隔小于静默期
	d.Add("/data/a.jpg")
	d.Add("/data/a.jpg")
	time.Sleep(30 * time.Millisecond)
	d.Add("/data/a.jpg") // 重置计时
	d.Add("/data/b.mp4")

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(batches)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 {
		t.Fatalf("静默期内多次事件应只 flush 一次，实际 %d 批: %v", len(batches), batches)
	}
	got := batches[0]
	if len(got) != 2 || got[0] != "/data/a.jpg" || got[1] != "/data/b.mp4" {
		t.Errorf("flush 应为排序后路径并集 [a.jpg b.mp4]，实际 %v", got)
	}
}

// TestDebouncerSecondBatch 静默期过后的新事件触发新一轮 flush。
func TestDebouncerSecondBatch(t *testing.T) {
	var mu sync.Mutex
	var batches [][]string
	d := NewDebouncer(50*time.Millisecond, func(paths []string) {
		mu.Lock()
		batches = append(batches, paths)
		mu.Unlock()
	})
	defer d.Stop()

	waitN := func(n int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			got := len(batches)
			mu.Unlock()
			if got >= n || time.Now().After(deadline) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	d.Add("/x/1.jpg")
	waitN(1)
	d.Add("/x/2.jpg")
	waitN(2)

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 2 {
		t.Fatalf("应有两批 flush，实际 %d", len(batches))
	}
	if len(batches[1]) != 1 || batches[1][0] != "/x/2.jpg" {
		t.Errorf("第二批应只含新路径，实际 %v", batches[1])
	}
}
