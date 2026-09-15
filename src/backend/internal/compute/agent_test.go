package compute

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// agentHarness 假控制端：记录心跳/拉取/回传，并按脚本派发任务。
//
// 用真 HTTP（httptest）而不是 mock Client，是因为本交付项要证明的正是
// 「拉取 → 执行 → 回传」这条跨进程链路；把客户端也换掉就什么都没证明。
type agentHarness struct {
	mu            sync.Mutex
	heartbeats    []map[string]any
	polls         int
	results       []ResultRequest
	pendingJobs   []JobSpec
	heartbeatFail int // >0 时心跳返回 500，用于测退避
	resultFail    int // >0 时回传返回 500，用于测"回传失败只记日志不崩"
	status        int // 非 0 时所有请求返回该状态码（用于测 401 fatal）
}

func (h *agentHarness) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		h.mu.Lock()
		defer h.mu.Unlock()

		if h.status != 0 {
			w.WriteHeader(h.status)
			_, _ = w.Write([]byte(`{"error":{"code":"INVALID_AGENT_TOKEN","message":"节点令牌无效或已过期"}}`))
			return
		}

		switch r.URL.Path {
		case "/compute-nodes/agent/heartbeat":
			if h.heartbeatFail > 0 {
				h.heartbeatFail--
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL","message":"boom"}}`))
				return
			}
			h.heartbeats = append(h.heartbeats, body)
			_, _ = w.Write([]byte(`{"node_id":"n1","status":"online","heartbeat_interval_seconds":60,"offline_after_seconds":180}`))

		case "/compute-nodes/agent/poll":
			h.polls++
			// 必须尊重节点请求的 max：否则"按空闲额度拉取"这条节流就测不出来了。
			max := len(h.pendingJobs)
			if v, ok := body["max"].(float64); ok && int(v) > 0 && int(v) < max {
				max = int(v)
			}
			jobs := append([]JobSpec(nil), h.pendingJobs[:max]...)
			h.pendingJobs = h.pendingJobs[max:]
			out, _ := json.Marshal(PollResponse{Jobs: normalizeJobs(jobs), ServerTime: time.Now()})
			_, _ = w.Write(out)

		case "/compute-nodes/agent/result":
			if h.resultFail > 0 {
				h.resultFail--
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var res ResultRequest
			_ = json.Unmarshal(raw, &res)
			h.results = append(h.results, res)
			_, _ = w.Write([]byte(`{"ok":true}`))

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func normalizeJobs(in []JobSpec) []JobSpec {
	if in == nil {
		return []JobSpec{}
	}
	return in
}

func (h *agentHarness) snapshot() (hb []map[string]any, polls int, results []ResultRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]any(nil), h.heartbeats...), h.polls, append([]ResultRequest(nil), h.results...)
}

// newTestAgent 构造一个心跳周期很短、日志丢弃的 agent。
func newTestAgent(t *testing.T, srvURL string, cfg AgentConfig) *Agent {
	t.Helper()
	cfg.ServerURL = srvURL
	if cfg.Token == "" {
		cfg.Token = "test-token"
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 20 * time.Millisecond
	}
	if cfg.Device == "" {
		cfg.Device = DeviceCPU // 单测固定 CPU：不依赖测试机是否有显卡
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	a, err := NewAgent(cfg)
	if err != nil {
		t.Fatalf("NewAgent 出错: %v", err)
	}
	return a
}

// TestAgentHappyPath 闭环：心跳（首个带能力声明）→ 拉取 → 执行 → 回传 done。
func TestAgentHappyPath(t *testing.T) {
	h := &agentHarness{pendingJobs: []JobSpec{
		{JobID: "job-1", Kind: "noop", MediaID: "media-1"},
		{JobID: "job-2", Kind: "noop", MediaID: "media-2"},
	}}
	srv := h.server(t)

	a := newTestAgent(t, srv.URL, AgentConfig{Concurrency: 2})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// 等到回传了 2 个结果（或超时）
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, _, results := h.snapshot()
		if len(results) >= 2 {
			break
		}
		if time.Now().After(deadline) {
			_, polls, results := h.snapshot()
			t.Fatalf("超时未收到回传：polls=%d results=%d", polls, len(results))
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run 在 ctx 取消后应返回 nil，实际 %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在取消后退出（可能忽略了 ctx）")
	}

	hb, polls, results := h.snapshot()
	if len(hb) == 0 {
		t.Fatal("未收到任何心跳")
	}
	if polls == 0 {
		t.Fatal("未发起过拉取")
	}

	// 首个心跳必须携带完整能力声明（TDD §6.1「重连后重新注册能力」）
	first := hb[0]
	for _, k := range []string{"codecs", "has_nvenc", "vram_mb", "concurrency"} {
		if _, ok := first[k]; !ok {
			t.Fatalf("首个心跳必须携带能力字段 %q，实际 %v", k, first)
		}
	}
	if first["has_nvenc"] != false {
		t.Fatalf("device=cpu 时应上报 has_nvenc=false，实际 %v", first["has_nvenc"])
	}
	if first["codecs"] != "h264" {
		t.Fatalf("device=cpu 时应上报 codecs=h264，实际 %v", first["codecs"])
	}

	// 后续心跳不必重复能力（服务端 COALESCE 保留旧值），但必须仍在上报 status
	if len(hb) >= 2 {
		if _, ok := hb[1]["codecs"]; ok {
			t.Fatalf("非首次心跳不该重复携带能力字段（避免噪声）：%v", hb[1])
		}
	}

	// 两个任务都必须回传 done，且 job_id 对得上
	got := map[string]string{}
	for _, r := range results {
		got[r.JobID] = r.Status
	}
	for _, want := range []string{"job-1", "job-2"} {
		if got[want] != JobStatusDone {
			t.Fatalf("任务 %s 的回传状态应为 done，实际 %q（全部：%v）", want, got[want], got)
		}
	}
}

// TestAgentFatalStopsImmediately 401 必须让 Run 立刻返回错误，不做指数退避空转。
func TestAgentFatalStopsImmediately(t *testing.T) {
	h := &agentHarness{status: http.StatusUnauthorized}
	srv := h.server(t)

	a := newTestAgent(t, srv.URL, AgentConfig{})

	start := time.Now()
	err := a.Run(context.Background())
	if err == nil {
		t.Fatal("401 时 Run 必须返回错误（而不是无限重试）")
	}
	if !IsFatal(err) {
		t.Fatalf("返回的错误应被判为 fatal（供 main 决定退出码）：%v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fatal 应立即返回，实际耗时 %s（说明进入了退避重试）", elapsed)
	}
}

// TestAgentRetriesWithBackoff 500 不应终止，而应退避后继续（且不空转刷屏）。
func TestAgentRetriesWithBackoff(t *testing.T) {
	h := &agentHarness{heartbeatFail: 1}
	srv := h.server(t)

	a := newTestAgent(t, srv.URL, AgentConfig{HeartbeatInterval: 10 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	// 前两次心跳失败后应最终成功（心跳被记录）
	deadline := time.Now().Add(4 * time.Second)
	for {
		hb, _, _ := h.snapshot()
		if len(hb) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("退避重试后仍未成功心跳（可能把 500 也当成 fatal 直接退出了）")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ctx 超时/取消后应返回 nil，实际 %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未退出")
	}
}

// TestAgentResultFailureDoesNotCrash 回传失败只记日志，进程继续（下一轮仍会心跳）。
func TestAgentResultFailureDoesNotCrash(t *testing.T) {
	h := &agentHarness{
		pendingJobs: []JobSpec{{JobID: "job-x", Kind: "noop", MediaID: "m"}},
		resultFail:  1,
	}
	srv := h.server(t)

	a := newTestAgent(t, srv.URL, AgentConfig{})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		hb, _, _ := h.snapshot()
		if len(hb) >= 2 { // 至少两轮心跳 → 回传失败没有拖垮循环
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("回传失败后循环未能继续（心跳轮次不足）")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	_, _, results := h.snapshot()
	if len(results) != 0 {
		t.Fatalf("第一次回传被判失败，不该出现在成功记录里：%v", results)
	}
}

// TestAgentConcurrencyLimit 并发上限不得被突破，且确实并行（峰值应恰为 2）。
func TestAgentConcurrencyLimit(t *testing.T) {
	jobs := []JobSpec{}
	for i := 0; i < 16; i++ {
		jobs = append(jobs, JobSpec{JobID: "j" + strconv.Itoa(i), Kind: "noop", MediaID: "m"})
	}
	h := &agentHarness{pendingJobs: jobs}
	srv := h.server(t)

	// 指针接收者：Execute 里要对 cur/peak 做原子增减，值接收者会各自拿到一份副本。
	slow := &slowExecutor{delay: 30 * time.Millisecond}

	a := newTestAgent(t, srv.URL, AgentConfig{Concurrency: 2, Executor: slow})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = a.Run(ctx) }()

	// 等到至少 4 个任务完成，确保观察窗口覆盖到"多个任务同时在跑"的时刻。
	deadline := time.Now().Add(4 * time.Second)
	for {
		_, _, results := h.snapshot()
		if len(results) >= 4 {
			break
		}
		if time.Now().After(deadline) {
			_, polls, results := h.snapshot()
			t.Fatalf("超时未收到足够结果：polls=%d results=%d", polls, len(results))
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	time.Sleep(60 * time.Millisecond) // 等在跑的任务收尾，避免读到中间态

	peak := atomic.LoadInt64(&slow.peak)
	if peak > 2 {
		t.Fatalf("并发上限为 2，实际观测到峰值 %d（额度控制失效）", peak)
	}
	if peak != 2 {
		t.Fatalf("并发峰值应为 2（否则说明任务是串行跑的，额度未被打满），实际 %d", peak)
	}
}

// slowExecutor 记录并发峰值并睡一下，用来暴露额度控制是否生效。
// 必须用指针接收者，否则每次 Execute 拿到的是结构体副本，峰值统计失真。
type slowExecutor struct {
	delay time.Duration
	cur   int64
	peak  int64
}

func (*slowExecutor) Name() string { return "slow" }

func (s *slowExecutor) Execute(ctx context.Context, job JobSpec) ResultRequest {
	cur := atomic.AddInt64(&s.cur, 1)
	defer atomic.AddInt64(&s.cur, -1)
	for {
		peak := atomic.LoadInt64(&s.peak)
		if cur <= peak || atomic.CompareAndSwapInt64(&s.peak, peak, cur) {
			break
		}
	}
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
	}
	return ResultRequest{JobID: job.JobID, Status: JobStatusDone}
}

// TestAgentConfigValidation 构造期的参数校验与默认值。
func TestAgentConfigValidation(t *testing.T) {
	t.Run("缺 server / token → 报错", func(t *testing.T) {
		if _, err := NewAgent(AgentConfig{Token: "t"}); err == nil {
			t.Fatal("缺 server 应报错")
		}
		if _, err := NewAgent(AgentConfig{ServerURL: "http://x"}); err == nil {
			t.Fatal("缺 token 应报错")
		}
	})

	t.Run("非法 device → 报错", func(t *testing.T) {
		if _, err := NewAgent(AgentConfig{ServerURL: "http://x", Token: "t", Device: "tpu"}); err == nil {
			t.Fatal("非法 device 应报错")
		}
	})

	t.Run("默认值：device=auto、并发 1、心跳 60s、executor=noop", func(t *testing.T) {
		a, err := NewAgent(AgentConfig{ServerURL: "http://x", Token: "t", Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if a.cfg.Device != DeviceAuto {
			t.Fatalf("device 应默认为 auto，实际 %q", a.cfg.Device)
		}
		if a.cfg.Concurrency != 1 {
			t.Fatalf("并发应默认为 1，实际 %d", a.cfg.Concurrency)
		}
		if a.cfg.HeartbeatInterval != DefaultHeartbeatInterval {
			t.Fatalf("心跳应默认为 %s，实际 %s", DefaultHeartbeatInterval, a.cfg.HeartbeatInterval)
		}
		if a.cfg.Executor == nil || a.cfg.Executor.Name() != "noop" {
			t.Fatalf("executor 应默认为 noop，实际 %v", a.cfg.Executor)
		}
		if a.cfg.NodeName == "" {
			t.Fatal("节点名应回落为主机名而不是空")
		}
	})

	t.Run("device=cpu 时能力声明为 CPU（与机器有无显卡无关）", func(t *testing.T) {
		a, err := NewAgent(AgentConfig{ServerURL: "http://x", Token: "t", Device: DeviceCPU,
			Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		c := a.Capabilities()
		if c.HasNVENC || c.CodecsString() != "h264" {
			t.Fatalf("device=cpu 应为 CPU 能力，实际 %+v", c)
		}
	})
}

// TestAgentContextAlreadyCancelled ctx 已取消时 Run 应立即返回 nil。
func TestAgentContextAlreadyCancelled(t *testing.T) {
	a := newTestAgent(t, "http://127.0.0.1:1", AgentConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Run(ctx); err != nil {
		t.Fatalf("ctx 已取消应返回 nil，实际 %v", err)
	}
}

// TestExecutorContract 两个执行器都必须尊重 ctx，且不丢 job_id。
func TestExecutorContract(t *testing.T) {
	job := JobSpec{JobID: "j1", Kind: "noop", MediaID: "m1"}
	for _, ex := range []Executor{NewNoopExecutor(), NewLocalExecutor(DeviceCPU)} {
		t.Run(ex.Name()+"/正常", func(t *testing.T) {
			r := ex.Execute(context.Background(), job)
			if r.JobID != "j1" {
				t.Fatalf("不得丢 job_id，实际 %q", r.JobID)
			}
			if r.Status != JobStatusDone {
				t.Fatalf("本交付项的执行器应返回 done，实际 %q", r.Status)
			}
		})
		t.Run(ex.Name()+"/ctx 取消 → failed", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := ex.Execute(ctx, job)
			if r.Status != JobStatusFailed {
				t.Fatalf("取消时应返回 failed（避免停机时上报假成功），实际 %q", r.Status)
			}
			if r.Error == "" {
				t.Fatal("failed 应带原因")
			}
		})
	}
}

// TestAgentRunOneKeepsJobID Executor 弄丢 job_id 时由 runOne 兜底补齐。
func TestAgentRunOneKeepsJobID(t *testing.T) {
	h := &agentHarness{pendingJobs: []JobSpec{{JobID: "job-real", Kind: "noop", MediaID: "m"}}}
	srv := h.server(t)

	a := newTestAgent(t, srv.URL, AgentConfig{Executor: forgetfulExecutor{}})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = a.Run(ctx) }()

	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		_, _, results := h.snapshot()
		if len(results) > 0 {
			if results[0].JobID != "job-real" {
				t.Fatalf("job_id 应被兜底补回 job-real，实际 %q", results[0].JobID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("未收到回传")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// forgetfulExecutor 故意不回填 job_id，用来验证 runOne 的兜底。
type forgetfulExecutor struct{}

func (forgetfulExecutor) Name() string { return "forgetful" }
func (forgetfulExecutor) Execute(context.Context, JobSpec) ResultRequest {
	return ResultRequest{Status: JobStatusDone}
}
