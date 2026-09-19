package ffmpeg

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultGrace 优雅停止等待：向 stdin 写 'q' 后等待 ffmpeg 自行退出，超时则 Kill。
const DefaultGrace = 3 * time.Second

// Error 结构化执行错误（验收 AC-07）：退出码 + stderr 尾部 + 执行时长。
type Error struct {
	ExitCode   int
	StderrTail string
	Duration   time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("ffmpeg 退出码 %d（耗时 %s）\nstderr 尾部:\n%s",
		e.ExitCode, e.Duration.Round(time.Millisecond), e.StderrTail)
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// lookPath 按「环境变量 → 系统 PATH」解析可执行文件（用户已确认 Q1：环境变量+PATH 回退）。
// 环境变量可指向可执行文件本身，或包含它的 bin 目录。
func lookPath(envVar, base string) (string, error) {
	if p := os.Getenv(envVar); p != "" {
		for _, cand := range []string{p, filepath.Join(p, exeName(base))} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
		}
		return "", fmt.Errorf("%s=%q 无效：既不是可执行文件，也不是包含 %s 的目录", envVar, p, exeName(base))
	}
	return exec.LookPath(exeName(base))
}

// LookPath 解析 ffmpeg（FFMPEG_PATH → PATH）。
func LookPath() (string, error) { return lookPath("FFMPEG_PATH", "ffmpeg") }

// LookProbePath 解析 ffprobe（FFPROBE_PATH → PATH）。
func LookProbePath() (string, error) { return lookPath("FFPROBE_PATH", "ffprobe") }

// Option Task 可选配置。
type Option func(*Task)

// WithDir 设置子进程工作目录。
func WithDir(dir string) Option { return func(t *Task) { t.dir = dir } }

// WithExpectedDurationUs 告知输入总时长（微秒），用于计算 Progress.RemainingUs。
func WithExpectedDurationUs(us int64) Option { return func(t *Task) { t.expectedUs = us } }

// WithGrace 调整优雅停止等待时长（默认 DefaultGrace）。
func WithGrace(d time.Duration) Option { return func(t *Task) { t.grace = d } }

// WithTailLines 调整 stderr 尾部保留行数（默认 20）。
func WithTailLines(n int) Option { return func(t *Task) { t.tailLines = n } }

// Task 一次 ffmpeg 子进程执行（异步句柄，用户已确认 Q2：Start/Wait/Cancel/SubscribeProgress）。
// 生命周期：New → Start(ctx) → [SubscribeProgress] → Wait；可 Cancel。
type Task struct {
	bin  string
	args []string

	dir        string
	expectedUs int64
	grace      time.Duration
	tailLines  int

	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	subs      []chan Progress
	tail      []string
	res       *Result
	runErr    error
	done      chan struct{}
	startedAt time.Time
	canceled  bool
	once      sync.Once
	lookErr   error
	// finished 与 sendWG 配合 broadcast 的锁外发送（见 broadcast 注释）：
	// finished 置位后不再接受新订阅、broadcast 直接返回；finish 等 sendWG 归零
	// 后再关闭订阅 channel，避免「send on closed channel」panic。
	finished bool
	sendWG   sync.WaitGroup
}

// New 创建任务。args 不含二进制名；进度标志由封装自动注入（-hide_banner -nostats -progress pipe:1）。
func New(args []string, opts ...Option) *Task {
	bin, err := LookPath()
	t := &Task{
		bin:       bin,
		args:      args,
		grace:     DefaultGrace,
		tailLines: 20,
		done:      make(chan struct{}),
		lookErr:   err,
	}
	for _, o := range opts {
		o(t)
	}
	return t
}

// Start 启动子进程。ctx 取消/超时时走优雅停止（'q' → 宽限 → Kill）。
func (t *Task) Start(ctx context.Context) error {
	t.mu.Lock()
	if t.cmd != nil {
		t.mu.Unlock()
		return errors.New("ffmpeg: task 已启动")
	}
	if t.lookErr != nil {
		t.mu.Unlock()
		return t.lookErr
	}
	args := append([]string{"-hide_banner", "-nostats", "-progress", "pipe:1"}, t.args...)
	cmd := exec.Command(t.bin, args...)
	if t.dir != "" {
		cmd.Dir = t.dir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.mu.Unlock()
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.mu.Unlock()
		return err
	}
	t.startedAt = time.Now()
	if err := cmd.Start(); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("ffmpeg: 启动失败: %w", err)
	}
	t.cmd = cmd
	t.stdin = stdin
	t.mu.Unlock()

	go t.watchCtx(ctx)
	go t.scanStdout(stdout)
	go t.scanStderr(stderr)
	go func() {
		t.finish(cmd.Wait())
	}()
	return nil
}

// SubscribeProgress 订阅进度事件。chan 缓冲满时中间帧被丢弃，最终 Done 帧保证送达。
// 任务结束后 chan 关闭。可在 Start 前后调用。
func (t *Task) SubscribeProgress(buf int) <-chan Progress {
	ch := make(chan Progress, buf)
	t.mu.Lock()
	if t.finished {
		close(ch)
	} else {
		t.subs = append(t.subs, ch)
	}
	t.mu.Unlock()
	return ch
}

// Cancel 请求优雅停止（幂等）。
func (t *Task) Cancel() { t.requestStop() }

// Wait 阻塞至任务结束，返回结果；退出码非 0 时 error 为 *Error（AC-07）。
func (t *Task) Wait() (*Result, error) {
	t.mu.Lock()
	started := t.cmd != nil
	lookErr := t.lookErr
	t.mu.Unlock()
	if !started {
		if lookErr != nil {
			return nil, lookErr
		}
		return nil, errors.New("ffmpeg: task 未启动")
	}
	<-t.done
	return t.res, t.runErr
}

// Run 便捷方法：Start + Wait。
func (t *Task) Run(ctx context.Context) (*Result, error) {
	if err := t.Start(ctx); err != nil {
		return nil, err
	}
	return t.Wait()
}

func (t *Task) watchCtx(ctx context.Context) {
	select {
	case <-ctx.Done():
		t.requestStop()
	case <-t.done:
	}
}

func (t *Task) requestStop() {
	t.mu.Lock()
	if t.canceled || t.cmd == nil || t.cmd.ProcessState != nil {
		t.mu.Unlock()
		return
	}
	t.canceled = true
	stdin := t.stdin
	grace := t.grace
	t.mu.Unlock()

	if stdin != nil {
		// ffmpeg 约定：stdin 收到 'q' 即优雅退出（封装层"SIGTERM 等价物"，Windows 无信号）
		_, _ = io.WriteString(stdin, "q")
		_ = stdin.Close()
	}
	time.AfterFunc(grace, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.cmd != nil && t.cmd.ProcessState == nil {
			_ = t.cmd.Process.Kill()
		}
	})
}

func (t *Task) scanStdout(r io.Reader) {
	acc := &progressAcc{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		if p, ok := acc.feed(sc.Text()); ok {
			if t.expectedUs > 0 && p.Speed > 0 && !p.Done {
				p.RemainingUs = int64(float64(t.expectedUs-p.OutTimeUs) / p.Speed)
			} else {
				p.RemainingUs = -1
			}
			t.broadcast(p)
		}
	}
}

func (t *Task) scanStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		t.mu.Lock()
		t.tail = append(t.tail, sc.Text())
		if len(t.tail) > t.tailLines {
			t.tail = t.tail[len(t.tail)-t.tailLines:]
		}
		t.mu.Unlock()
	}
}

// broadcast 锁内快照订阅者、**锁外发送**（P1-03 管线）：
// Done 帧为「终帧必达」是阻塞发送——若持 mu 发送，某个缓冲满且不再读取的失联订阅者
// 会让 scanStdout 永远卡在锁内 → finish 拿不到 mu → t.done 永不关闭 → 任务整体死锁。
// 锁外发送后，失联订阅者最多泄漏其自身这一路发送（由 sendWG 兜底关 channel），
// 不再拖死任务。sendWG + finished 保证 finish 不会在发送途中 close 这些 channel
// （否则就是 send on closed channel 的 panic）。
func (t *Task) broadcast(p Progress) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.sendWG.Add(1)
	subs := append([]chan Progress(nil), t.subs...)
	t.mu.Unlock()
	defer t.sendWG.Done()
	for _, ch := range subs {
		if p.Done {
			ch <- p // 终帧必达
		} else {
			select {
			case ch <- p:
			default: // 订阅者过慢，丢中间帧
			}
		}
	}
}

func (t *Task) finish(waitErr error) {
	t.once.Do(func() {
		t.mu.Lock()
		exitCode := 0
		if waitErr != nil {
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				exitCode = ee.ExitCode()
			} else {
				exitCode = -1
			}
		}
		t.res = &Result{
			ExitCode:   exitCode,
			StderrTail: strings.Join(t.tail, "\n"),
			Duration:   time.Since(t.startedAt),
			Canceled:   t.canceled,
		}
		if exitCode != 0 {
			t.runErr = &Error{
				ExitCode:   exitCode,
				StderrTail: t.res.StderrTail,
				Duration:   t.res.Duration,
			}
		}
		// finished 与 close(done) 都必须在 mu 内完成之前的「关订阅 channel」之前：
		// finished 置位后 SubscribeProgress 不再 append、broadcast 不再读取 t.subs，
		// 因此此刻 subs 快照就是全部需要关闭的 channel。
		t.finished = true
		subs := t.subs
		t.mu.Unlock()
		close(t.done)
		// 等在途 broadcast 发送完毕再关闭 channel；失联订阅者的阻塞发送
		// 只会卡住这个收尾 goroutine，不再拖死 Wait/Cancel。
		go func() {
			t.sendWG.Wait()
			for _, ch := range subs {
				close(ch)
			}
		}()
	})
}

// ProbeSize 用 ffprobe 获取首个视频流的分辨率（像素）。
// media 表缺 width/height 时作为兜底，供 LadderForSource 裁剪阶梯。
func ProbeSize(ctx context.Context, input string) (int, int, error) {
	bin, err := LookProbePath()
	if err != nil {
		return 0, 0, err
	}
	out, err := exec.CommandContext(ctx, bin,
		"-v", "error", "-select_streams", "v:0", "-show_entries", "stream=width,height",
		"-of", "csv=p=0", input).Output()
	if err != nil {
		return 0, 0, fmt.Errorf("ffprobe: %w", err)
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	line = strings.TrimSpace(line)
	ws, hs, ok := strings.Cut(line, ",")
	if !ok {
		return 0, 0, fmt.Errorf("ffprobe: 解析分辨率失败: %q", line)
	}
	w, errW := strconv.Atoi(strings.TrimSpace(ws))
	h, errH := strconv.Atoi(strings.TrimSpace(hs))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("ffprobe: 解析分辨率失败: %q", line)
	}
	return w, h, nil
}

// ProbeDurationUs 用 ffprobe 获取媒体总时长（微秒）。
func ProbeDurationUs(ctx context.Context, input string) (int64, error) {
	bin, err := LookProbePath()
	if err != nil {
		return 0, err
	}
	out, err := exec.CommandContext(ctx, bin,
		"-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", input).Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe: %w", err)
	}
	var sec float64
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%g", &sec); err != nil {
		return 0, fmt.Errorf("ffprobe: 解析时长失败: %q", strings.TrimSpace(string(out)))
	}
	return int64(sec * 1e6), nil
}
