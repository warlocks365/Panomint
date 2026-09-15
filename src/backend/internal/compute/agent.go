package compute

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// 节点 agent 主循环（节点侧）。
//
// 一个 tick 内的顺序：心跳 → 拉取任务 → 并发执行 → 回传结果。
// 心跳与拉取共用同一个 ticker（都在 DefaultHeartbeatInterval 这一刻发生），
// 刻意不引入第二个独立节奏——两个各自漂移的定时器只会让排障变复杂。
//
// 断线重连按 TDD §6.1 的节奏做指数退避：1s→2s→4s→8s→30s 上限。
// 凭据类错误（401/403）不重试，立即返回错误让进程退出（见 IsFatal）。

// 退避参数（TDD §6.1「指数退避：1s→2s→4s→8s→30s 上限」）。
const (
	backoffBase = time.Second
	backoffMax  = 30 * time.Second
)

// shutdownGrace 收到取消信号后等在跑任务收尾的上限。
const shutdownGrace = 5 * time.Second

// AgentConfig 节点 agent 配置。
//
// 注意 ServerURL / Token **没有默认值**：它们只能来自运行时输入（命令行/环境变量）。
// 这是项目红线的直接体现——任何具体节点的地址与凭据都不得写死进代码或默认配置。
type AgentConfig struct {
	ServerURL string        // 控制端地址，如 http://control-plane:8080
	Token     string        // 节点接入令牌（明文，仅存于进程内存）
	NodeName  string        // 节点名，仅用于日志
	Device    string        // cpu|cuda|auto
	Concurrency int         // 并发任务数
	Executor  Executor      // 任务执行器；nil 时用 NoopExecutor
	HeartbeatInterval time.Duration // 心跳周期；<= 0 时用 DefaultHeartbeatInterval
	OfflineAfter      time.Duration // 离线阈值（仅用于日志与对齐）；<= 0 时用 DefaultOfflineAfter
	Logger    *log.Logger
}

// Agent 节点 agent 运行时。
type Agent struct {
	cfg    AgentConfig
	client *Client
	caps   Capabilities
	log    *log.Logger

	// activeTasks 在跑任务数，用于心跳上报 active_tasks 与决定自报 online/busy。
	activeTasks int64
	// sem 并发额度（容量 = Concurrency）。
	sem chan struct{}

	// 首次心跳需要携带完整能力声明（TDD §6.1「重连后重新注册能力」），之后不再重复。
	// 用 atomic 而非普通 bool：心跳由主循环单 goroutine 发起，但保持"只置一次"的
	// 语义无歧义，且将来若改成并发心跳也不会退化。
	capsDeclared int32
}

// NewAgent 构造 agent。
func NewAgent(cfg AgentConfig) (*Agent, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("必须指定控制端地址（-server）")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("必须指定节点接入令牌（-token）")
	}

	device, err := ParseDevice(cfg.Device)
	if err != nil {
		return nil, err
	}
	cfg.Device = device // 空串归一为 auto，让 cfg 始终持有规范化取值（默认值断言依赖它）
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if cfg.OfflineAfter <= 0 {
		cfg.OfflineAfter = DefaultOfflineAfter
	}
	if cfg.Executor == nil {
		cfg.Executor = NewNoopExecutor()
	}
	if cfg.Logger == nil {
		cfg.Logger = log.New(os.Stderr, "[nodeagent] ", log.LstdFlags|log.Lmsgprefix)
	}
	if cfg.NodeName == "" {
		if hn, err := os.Hostname(); err == nil {
			cfg.NodeName = hn
		} else {
			cfg.NodeName = "unknown"
		}
	}

	return &Agent{
		cfg:    cfg,
		client: NewClient(cfg.ServerURL, cfg.Token),
		// 能力探测**不会失败**：无 CUDA 时只降级为 CPU 能力（见 capabilities.go）。
		caps: DetectCapabilities(device, cfg.Concurrency, DefaultNVProber{}),
		log:  cfg.Logger,
		sem:  make(chan struct{}, cfg.Concurrency),
	}, nil
}

// Capabilities 本节点探测到的能力（供命令行打印与测试断言）。
func (a *Agent) Capabilities() Capabilities { return a.caps }

// Run 运行主循环，直到 ctx 被取消（返回 nil）或遇到不可重试的错误（返回该错误）。
func (a *Agent) Run(ctx context.Context) error {
	a.log.Printf("节点 %s 启动：server=%s executor=%s device=%s codecs=%s has_nvenc=%v vram_mb=%d concurrency=%d",
		a.cfg.NodeName, a.cfg.ServerURL, a.cfg.Executor.Name(), a.cfg.Device,
		a.caps.CodecsString(), a.caps.HasNVENC, a.caps.VRAMMB, a.cfg.Concurrency)
	if !a.caps.HasNVENC {
		// 明说降级而不是沉默：运维看到这行就知道"这台机器在跑 CPU 路径"，
		// 而不会误以为 GPU 已生效（这是双接口原则下最容易踩的认知坑）。
		a.log.Printf("未探测到 NVENC，本节点以 CPU 模式工作（has_nvenc=false）；接口保留，换到有卡的机器即可自动启用")
	}

	interval := a.cfg.HeartbeatInterval
	backoff := time.Duration(0)

	for {
		if err := ctx.Err(); err != nil {
			return nil
		}

		tickStart := time.Now()
		err := a.tick(ctx)
		if err != nil {
			if IsFatal(err) {
				// 凭据无效：重试没有意义，直接把错误交给调用方去终止进程。
				return fmt.Errorf("节点凭据无效，停止运行: %w", err)
			}
			if backoff == 0 {
				backoff = backoffBase
			} else if backoff < backoffMax {
				backoff *= 2
				if backoff > backoffMax {
					backoff = backoffMax
				}
			}
			a.log.Printf("本轮失败（%v），%s 后重试", err, backoff)
		} else {
			backoff = 0
		}

		// 睡眠到下一个 tick；退避期间也照常响应 ctx 取消（用 timer 而不是 time.Sleep）。
		wait := interval
		if backoff > 0 {
			wait = backoff
		}
		if elapsed := time.Since(tickStart); elapsed < wait {
			wait -= elapsed
		} else {
			wait = 0
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				a.drain()
				return nil
			case <-timer.C:
			}
		}
	}
}

// tick 一个周期：心跳 + 拉取 + 执行 + 回传。返回本轮的第一个错误。
func (a *Agent) tick(ctx context.Context) error {
	hbErr := a.sendHeartbeat(ctx)
	if hbErr != nil && IsFatal(hbErr) {
		return hbErr // 凭据问题立即上报，不浪费一次 poll
	}

	pollErr := a.pollAndRun(ctx)

	// 心跳失败（网络类）与拉取失败（网络类）本质是同一个连接问题，
	// 只报一个即可，避免同一原因在日志里出现两次。
	if hbErr != nil {
		return hbErr
	}
	return pollErr
}

// sendHeartbeat 发一次心跳。首次（或重连后的首次）携带完整能力声明。
//
// 心跳节奏以**本地配置为准**：服务端响应里的 heartbeat_interval_seconds /
// offline_after_seconds 是服务端公布自己的口径（供 curl 排障与其它客户端使用），
// 节点不拿它覆盖运维显式设置的 -heartbeat——否则"服务端返回 0"或"服务端改了默认值"
// 都会静默改变节点行为，属于最难查的一类问题。
func (a *Agent) sendHeartbeat(ctx context.Context) error {
	hb := HeartbeatRequest{
		Status:      string(StatusOnline),
		ActiveTasks: int(atomic.LoadInt64(&a.activeTasks)),
	}
	if hb.ActiveTasks > 0 {
		hb.Status = string(StatusBusy)
	}

	if atomic.CompareAndSwapInt32(&a.capsDeclared, 0, 1) {
		codecs := a.caps.CodecsString()
		hasNV, vram, conc := a.caps.HasNVENC, a.caps.VRAMMB, a.caps.Concurrency
		hb.Codecs, hb.HasNVENC, hb.VRAMMB, hb.Concurrency = &codecs, &hasNV, &vram, &conc
		a.log.Printf("首次心跳上报能力声明：codecs=%s has_nvenc=%v vram_mb=%d concurrency=%d",
			codecs, hasNV, vram, conc)
	}

	_, err := a.client.Heartbeat(ctx, hb)
	return err
}

// pollAndRun 拉取任务并（受并发额度限制地）执行与回传。
//
// 并发模型：sem 是容量 = Concurrency 的信号量。拉取量取"当前空闲额度"，
// 保证不会领到超过本节点处理能力的任务（领了却不执行会让任务卡在 running，
// 而 running 的任务不会被任何其他节点重领）。
func (a *Agent) pollAndRun(ctx context.Context) error {
	quota := a.pollQuota()
	if quota <= 0 {
		return nil // 已跑满，本轮只心跳
	}

	resp, err := a.client.Poll(ctx, quota)
	if err != nil {
		return err
	}
	if len(resp.Jobs) == 0 {
		return nil
	}
	a.log.Printf("领取 %d 个任务", len(resp.Jobs))

	var wg sync.WaitGroup
	for _, job := range resp.Jobs {
		select {
		case a.sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil
		}
		atomic.AddInt64(&a.activeTasks, 1)
		wg.Add(1)
		go func(j JobSpec) {
			defer wg.Done()
			defer func() {
				<-a.sem
				atomic.AddInt64(&a.activeTasks, -1)
			}()
			a.runOne(ctx, j)
		}(job)
	}
	wg.Wait()
	return nil
}

// pollQuota 本次可拉取的任务数 = 空闲额度，且不超过协议上限 MaxPollJobs。
func (a *Agent) pollQuota() int {
	free := cap(a.sem) - len(a.sem)
	if free <= 0 {
		return 0
	}
	if free > MaxPollJobs {
		return MaxPollJobs
	}
	return free
}

// runOne 执行一个任务并回传结果。
//
// 回传失败只记日志：任务已在服务端被标为 running 且 node_id 已落库，
// 此处重试整轮即可（下一轮若仍失败，运维可从 running 且心跳超时的节点上人工介入）。
// 刻意不在本地做重试队列——那等于在节点侧再实现一遍调度，属越界。
func (a *Agent) runOne(ctx context.Context, job JobSpec) {
	res := a.cfg.Executor.Execute(ctx, job)
	// Executor 的实现不应弄丢 job_id（协议靠它对账）；这里兜底。
	if res.JobID == "" {
		res.JobID = job.JobID
	}

	if err := a.client.Submit(ctx, res); err != nil {
		a.log.Printf("任务 %s 结果回传失败: %v", res.JobID, err)
		return
	}
	a.log.Printf("任务 %s 已完成（status=%s kind=%s）", res.JobID, res.Status, job.Kind)
}

// drain 取消后等在跑任务收尾，最多 shutdownGrace。
func (a *Agent) drain() {
	deadline := time.Now().Add(shutdownGrace)
	for atomic.LoadInt64(&a.activeTasks) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := atomic.LoadInt64(&a.activeTasks); n > 0 {
		a.log.Printf("停机：仍有 %d 个任务在执行，超过 %s 未收尾，直接退出", n, shutdownGrace)
	}
}
