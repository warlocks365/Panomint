package watch

import (
	"sort"
	"sync"
	"time"
)

// Debouncer 事件去抖：路径聚合到集合，静默 delay 后一次性 flush。
// 解决编辑器/NAS 写入产生的 Create+多次 Write 风暴，也聚合 Windows
// ReadDirectoryChangesW 的 Rename 拆分事件（旧/新路径都入集合，导入时 stat 判存）。
type Debouncer struct {
	delay time.Duration
	flush func(paths []string)

	mu      sync.Mutex
	pending map[string]struct{}
	timer   *time.Timer
	stopped bool
}

// NewDebouncer 创建去抖器；flush 在静默 delay 后被调用（单 goroutine，串行）。
func NewDebouncer(delay time.Duration, flush func(paths []string)) *Debouncer {
	return &Debouncer{delay: delay, flush: flush, pending: map[string]struct{}{}}
}

// Add 登记一个变更路径并重置静默计时器。
func (d *Debouncer) Add(path string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	d.pending[path] = struct{}{}
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.fire)
}

// PendingCount 当前待 flush 的路径数（测试/观测用）。
func (d *Debouncer) PendingCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.pending)
}

// Stop 停止计时器；未 flush 的变更直接丢弃（进程退出场景）。
func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.stopped = true
	if d.timer != nil {
		d.timer.Stop()
	}
}

func (d *Debouncer) fire() {
	d.mu.Lock()
	if d.stopped || len(d.pending) == 0 {
		d.mu.Unlock()
		return
	}
	paths := make([]string, 0, len(d.pending))
	for p := range d.pending {
		paths = append(paths, p)
	}
	d.pending = map[string]struct{}{}
	d.mu.Unlock()

	sort.Strings(paths) // 排序保证可重放，与 index.ScanDir 一致
	d.flush(paths)
}
