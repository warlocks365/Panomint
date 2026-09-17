package audit

// 测试替身：实现 RecorderStore + QueryStore。
//
// 存在的理由：本包最核心的两条硬约束（「审计写入失败不得影响业务」与
// 「handler 不得把查询错误包装成 200」）**无法用真库断言** ——
// 真库不会按命令失败或 panic。只能靠可控的假实现把失败注入进去。

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// observerNew 构造可断言的 zap core，用于验证「审计失败只记 warn」这一类行为。
func observerNew() (zapcore.Core, *observer.ObservedLogs) {
	return observer.New(zap.WarnLevel)
}

// mustTime 解析 RFC3339 时间字面量，非法即 Fatal（避免测试里写错时间却静默跳过断言）。
func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("测试时间字面量 %q 非法: %v", s, err)
	}
	return ts
}

// fakeStore 可注入各种失败模式的 store。
type fakeStore struct {
	mu sync.Mutex

	// Insert 行为
	inserted     []Entry
	insertErr    error
	insertPanics bool
	insertDelay  time.Duration

	// Query / Stats / Jobs 行为
	queryErr   error
	statsErr   error
	jobsErr    error
	lastFilter Filter
	lastJobQ   JobQuery
	// queryCalls 记录 Query 被调用的次数。
	// 存在的理由：若干用例要断言的是「handler 在校验失败时**提前返回**、根本没触库」——
	// 只断言状态码证明不了这一点（提前返回与"查完再报错"在响应上可能长得一样）。
	queryCalls int
	// lastJobID 记录 GetJob 收到的 id，供单条端点测试断言参数传递。
	lastJobID string

	page  *Page
	stats *Stats
	jobs  []Job

	// 记录 Insert 收到的 context 是否已被取消（用于断言 Log 与请求生命周期解耦）。
	lastInsertCtxErr error
	// 记录 Insert 收到的 context 是否带 deadline（用于断言写入有耗时上界）。
	lastInsertHadDeadline bool
}

func (f *fakeStore) Insert(ctx context.Context, e Entry) (int64, error) {
	_, hasDeadline := ctx.Deadline()

	f.mu.Lock()
	f.lastInsertHadDeadline = hasDeadline
	f.mu.Unlock()

	if f.insertDelay > 0 {
		select {
		case <-time.After(f.insertDelay):
		case <-ctx.Done():
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if f.insertPanics {
		panic("boom: 模拟审计写入内部 panic（例如驱动层 bug）")
	}
	// 真实驱动（pgx）在 ctx 结束时返回 context 错误，假实现同样如此 ——
	// 否则测不出"上界生效后调用方会看到什么"。
	if err := ctx.Err(); err != nil {
		f.lastInsertCtxErr = err
		return 0, err
	}
	f.lastInsertCtxErr = nil
	if f.insertErr != nil {
		return 0, f.insertErr
	}
	f.inserted = append(f.inserted, e)
	return int64(len(f.inserted)), nil
}

func (f *fakeStore) Query(_ context.Context, flt Filter) (*Page, error) {
	f.mu.Lock()
	f.lastFilter = flt
	f.queryCalls++
	f.mu.Unlock()
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	if f.page != nil {
		return f.page, nil
	}
	return &Page{Items: []Entry{}, Limit: flt.Limit}, nil
}

func (f *fakeStore) Stats(_ context.Context) (*Stats, error) {
	if f.statsErr != nil {
		return nil, f.statsErr
	}
	if f.stats != nil {
		return f.stats, nil
	}
	return &Stats{IndexStatus: IndexInfo{State: "unknown"}}, nil
}

func (f *fakeStore) Jobs(_ context.Context, q JobQuery) ([]Job, error) {
	f.mu.Lock()
	f.lastJobQ = q
	f.mu.Unlock()
	if f.jobsErr != nil {
		return nil, f.jobsErr
	}
	if f.jobs != nil {
		return f.jobs, nil
	}
	return []Job{}, nil
}

// GetJob 单条任务（GET /admin/jobs/:id）。与 Jobs 共用 jobsErr/jobs 注入点，
// 便于同一套假数据同时驱动"列表"与"单条"两条路径。
func (f *fakeStore) GetJob(_ context.Context, id string) (*Job, error) {
	f.mu.Lock()
	f.lastJobID = id
	f.mu.Unlock()
	if f.jobsErr != nil {
		return nil, f.jobsErr
	}
	for i := range f.jobs {
		if f.jobs[i].ID == id {
			return &f.jobs[i], nil
		}
	}
	return nil, ErrJobNotFound
}

// jobID 取最近一次 GetJob 的入参。
func (f *fakeStore) jobID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastJobID
}

func (f *fakeStore) entries() []Entry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Entry, len(f.inserted))
	copy(out, f.inserted)
	return out
}

func (f *fakeStore) jobQuery() JobQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastJobQ
}

func (f *fakeStore) filter() Filter {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFilter
}

// calls 返回 Query 被调用过的次数。
func (f *fakeStore) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.queryCalls
}

var errBoom = errors.New("模拟数据库不可用")
