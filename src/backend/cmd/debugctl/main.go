// debugctl 转码远程调试通道的参考 agent（Job000121 设计 §12 A1）：
// 凭「接入地址 + 密钥」经 WSS 接入调试通道，hello 握手后进入命令 REPL，
// 可查看转码现状（snapshot/queue.stats）、暂停/恢复/取消排队任务、订阅任务状态事件。
//
// 用法：
//
//	debugctl -url wss://<host>/debug/channel/<channel_id> [-key <密钥> | -key-file <路径>] [选项]
//
// 选项：
//
//	-url            接入地址（管理端 enable/rotate 响应的 url 字段；必填，无默认值）
//	-key            接入密钥（43 字符；与 -key-file 二选一，亦可用 DEBUG_KEY 环境变量）
//	-key-file       密钥文件路径（读取首行非空内容；防进程列表泄露）
//	-agent-version  hello 上报的版本串                        默认 debugctl/1.0
//	-once           执行单条命令后退出（非交互；供脚本/e2e 驱动）
//	-cmd            -once 模式下的命令行（snapshot|queue.stats|ping|subscribe|
//	                unsubscribe|pause <job_id>|resume <job_id>|cancel <job_id>|
//	                log <job_id> [lines]）
//
// ⚠️ 本程序**不含任何地址/凭据的默认值**：-url 与密钥必须由使用者提供（项目长期红线）。
//
// 连接中断（网络抖动/服务重启）时按指数退避自动重连：1s→2s→…→30s 封顶，±20% 抖动防惊群
// （设计 §8.2）。重连成功重新 hello 后**自动拉取一次 snapshot 全量对齐现状**
// （事件流不补发，靠快照对齐；设计 §8.2）。
//
// 密钥只经 Authorization: Bearer 头承载（设计 §5.2：禁止 query/cookie/URL fragment）；
// REPL 支持 `help` / `quit`。事件推送（job.state）以 [event] 前缀异步打印。
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

// envelope 与服务端 internal/debug/session.go 同构（协议契约 v1.14 §20）。
type envelope struct {
	Seq  int             `json:"seq,omitempty"`
	Evt  int             `json:"evt,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"payload,omitempty"`
}

// reply 单条应答的解析视图。
type reply struct {
	Seq     int             `json:"seq"`
	Type    string          `json:"type"` // result | error
	Data    json.RawMessage `json:"payload"`
	connSeq uint64          // 连接代数：旧连接迟到的应答不得入新连接的 pending 表
}

// client 单连接会话状态。
type client struct {
	url    string
	key    string
	ver    string
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	pending map[int]chan reply // seq → 应答信箱
	seq     int
	connSeq uint64
	conn    *websocket.Conn
}

func main() {
	url := flag.String("url", "", "接入地址 wss://<host>/debug/channel/<id>（必填）")
	key := flag.String("key", os.Getenv("DEBUG_KEY"), "接入密钥（与 -key-file 二选一）")
	keyFile := flag.String("key-file", "", "密钥文件路径（首行非空内容；防进程列表泄露）")
	ver := flag.String("agent-version", "debugctl/1.0", "hello 上报版本串")
	once := flag.Bool("once", false, "执行单条命令后退出（非交互）")
	cmdLine := flag.String("cmd", "", "-once 模式命令行（snapshot|queue.stats|ping|subscribe|unsubscribe|pause|resume|cancel|log）")
	flag.Parse()

	if strings.TrimSpace(*url) == "" {
		fatal("缺少 -url（接入地址来自管理端 enable/rotate 响应）")
	}
	k, err := resolveKey(*key, *keyFile)
	if err != nil {
		fatal("读取密钥失败: " + err.Error())
	}
	if k == "" {
		fatal("缺少密钥：-key / -key-file / DEBUG_KEY 三选一")
	}
	if !strings.HasPrefix(*url, "wss://") && !strings.HasPrefix(*url, "ws://") {
		fatal("-url 需为 ws:// 或 wss:// 开头")
	}

	ctx, cancel := signalCtx()
	defer cancel()

	c := &client{url: *url, key: k, ver: *ver, ctx: ctx, cancel: cancel, pending: map[int]chan reply{}}

	if *once {
		if strings.TrimSpace(*cmdLine) == "" {
			fatal("-once 需要配合 -cmd 使用")
		}
		// 非交互：首连失败即退出（退避重连是交互值守语义，脚本驱动要快速失败）
		if err := c.connect(); err != nil {
			fatal("连接失败: " + err.Error())
		}
		if err := c.hello(); err != nil {
			fatal("握手失败: " + err.Error())
		}
		code := c.runOnce(*cmdLine)
		_ = c.conn.Close(websocket.StatusNormalClosure, "done")
		os.Exit(code)
	}

	// 交互值守：断线指数退避重连，直到用户 Ctrl+C 或 quit。
	backoff := time.Second
	for {
		err := c.connect()
		if err == nil {
			err = c.hello()
		}
		if err == nil {
			fmt.Println("[debugctl] 已接入，输入 help 查看命令；quit 退出")
			backoff = time.Second // 成功后退避复位
			if c.repl() {         // true = 用户主动退出（quit/Ctrl+C/Ctrl+D）
				return
			}
		} else {
			fmt.Fprintf(os.Stderr, "[debugctl] 连接中断: %v（%.0fs 后重连）\n", err, backoff.Seconds())
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(backoff)):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

// resolveKey 密钥三选一解析：-key 优先，其次 -key-file，再次 DEBUG_KEY。
// -key-file 只取首行非空内容，避免行尾回车干扰。
func resolveKey(key, keyFile string) (string, error) {
	if strings.TrimSpace(key) != "" {
		return strings.TrimSpace(key), nil
	}
	if strings.TrimSpace(keyFile) == "" {
		return "", nil
	}
	b, err := os.ReadFile(strings.TrimSpace(keyFile))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s, nil
		}
	}
	return "", errors.New("密钥文件无有效内容")
}

// signalCtx Ctrl+C / SIGTERM 取消（交互退出与重连循环的终止信号）。
func signalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// jitter ±20% 抖动（设计 §8.2 防惊群）。
func jitter(d time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(4001))
	if err != nil {
		return d
	}
	f := 0.8 + float64(n.Int64())/10000.0 // [0.8, 1.2]
	return time.Duration(float64(d) * f)
}

// connect 建立 WSS 连接（Bearer 仅走头；设计 §5.2）。
func (c *client) connect() error {
	h := map[string][]string{"Authorization": {"Bearer " + c.key}}
	ctx, cancel := context.WithTimeout(c.ctx, 15*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.connSeq++
	connSeq := c.connSeq
	c.conn = ws
	c.mu.Unlock()
	go c.reader(ws, connSeq)
	return nil
}

// reader 收帧协程：应答按 seq 投递信箱；事件（evt>0）异步打印。
// 连接代数 connSeq 防止旧连接迟到的帧污染新连接状态。
func (c *client) reader(ws *websocket.Conn, connSeq uint64) {
	for {
		_, raw, err := ws.Read(c.ctx)
		if err != nil {
			c.mu.Lock()
			if c.connSeq == connSeq {
				c.conn = nil
			}
			pend := c.pending
			c.pending = map[int]chan reply{}
			c.mu.Unlock()
			for _, ch := range pend {
				close(ch) // 信箱关闭 = 连接已断，等待方报错退出
			}
			return
		}
		var env envelope
		if json.Unmarshal(raw, &env) != nil {
			continue
		}
		if env.Evt > 0 {
			fmt.Printf("[event] %s\n", pretty(env.Data))
			continue
		}
		c.mu.Lock()
		if c.connSeq != connSeq {
			c.mu.Unlock()
			continue
		}
		ch := c.pending[env.Seq]
		c.mu.Unlock()
		if ch != nil {
			ch <- reply{Seq: env.Seq, Type: env.Type, Data: env.Data, connSeq: connSeq}
		}
	}
}

// send 发送命令并返回该 seq 的信箱。
func (c *client) send(env envelope) chan reply {
	c.mu.Lock()
	c.seq++
	env.Seq = c.seq
	seq := c.seq
	connSeq := c.connSeq
	ch := make(chan reply, 1)
	c.pending[seq] = ch
	c.mu.Unlock()
	raw, _ := json.Marshal(env)
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		ch <- reply{Type: "error", Data: json.RawMessage(`{"code":"DISCONNECTED","message":"连接已断开"}`), connSeq: connSeq}
		return ch
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		c.mu.Lock()
		delete(c.pending, seq)
		c.mu.Unlock()
		ch <- reply{Type: "error", Data: json.RawMessage(`{"code":"WRITE_FAILED","message":"发送失败"}`), connSeq: connSeq}
	}
	return ch
}

// call 发送命令并等待应答（连接中断时信箱关闭 → 报错）。
func (c *client) call(env envelope) reply {
	ch := c.send(env)
	r, ok := <-ch
	if !ok {
		return reply{Type: "error", Data: json.RawMessage(`{"code":"DISCONNECTED","message":"连接已断开"}`)}
	}
	return r
}

// hello 首消息握手（服务端 15s 超时约束）。
func (c *client) hello() error {
	r := c.call(envelope{Type: "hello", Data: mustJSON(map[string]any{"agent_version": c.ver})})
	if r.Type == "error" {
		return errors.New(errMsg(r.Data))
	}
	fmt.Printf("[debugctl] hello_ack %s\n", pretty(r.Data))
	// 设计 §8.2：重连/接入后拉 snapshot 全量对齐（事件流不补发）
	if s := c.call(envelope{Type: "snapshot"}); s.Type == "result" {
		fmt.Printf("[snapshot] %s\n", pretty(s.Data))
	}
	return nil
}

// runOnce 非交互单命令。exit 0=成功；1=命令失败/连接断。
func (c *client) runOnce(line string) int {
	env, err := parseCommand(line)
	if err != nil {
		fmt.Fprintln(os.Stderr, "命令解析失败:", err)
		return 2
	}
	r := c.call(env)
	if r.Type == "error" {
		fmt.Println(pretty(r.Data))
		return 1
	}
	fmt.Println(pretty(r.Data))
	return 0
}

// repl 交互命令循环。返回 true = 用户主动退出（quit / Ctrl+C / Ctrl+D）；
// 返回 false = 连接中断（调用方走退避重连）。
func (c *client) repl() bool {
	sc := bufio.NewScanner(os.Stdin)
	for {
		if c.ctx.Err() != nil {
			return true
		}
		fmt.Print("debug> ")
		if !sc.Scan() {
			c.cancel()
			return true
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		switch line {
		case "quit", "exit":
			c.cancel()
			if c.conn != nil {
				_ = c.conn.Close(websocket.StatusNormalClosure, "bye")
			}
			return true
		case "help":
			printHelp()
			continue
		}
		env, err := parseCommand(line)
		if err != nil {
			fmt.Println("命令解析失败:", err)
			continue
		}
		r := c.call(env)
		if r.Type == "error" {
			fmt.Println(pretty(r.Data))
			if errCode(r.Data) == "DISCONNECTED" {
				return false // 断线：交回外层退避重连
			}
			continue
		}
		fmt.Println(pretty(r.Data))
	}
}

// parseCommand REPL 命令行 → 信封。
func parseCommand(line string) (envelope, error) {
	fields := strings.Fields(line)
	cmd := fields[0]
	arg := func(i int) string {
		if len(fields) > i {
			return fields[i]
		}
		return ""
	}
	switch cmd {
	case "snapshot", "queue.stats", "ping", "subscribe", "unsubscribe":
		return envelope{Type: cmd}, nil
	case "pause", "resume", "cancel":
		jobID := arg(1)
		if jobID == "" {
			return envelope{}, fmt.Errorf("%s 需要 job_id（pause <job_id>）", cmd)
		}
		return envelope{Type: "job." + cmd, Data: mustJSON(map[string]any{"job_id": jobID})}, nil
	case "log":
		jobID := arg(1)
		if jobID == "" {
			return envelope{}, errors.New("log 需要 job_id（log <job_id> [lines]）")
		}
		lines := 100
		if v := arg(2); v != "" {
			if _, err := fmt.Sscanf(v, "%d", &lines); err != nil {
				return envelope{}, errors.New("lines 需为数字")
			}
		}
		return envelope{Type: "job.log.tail", Data: mustJSON(map[string]any{"job_id": jobID, "lines": lines})}, nil
	default:
		return envelope{}, fmt.Errorf("未知命令 %q（help 查看全部）", cmd)
	}
}

func printHelp() {
	fmt.Print(`命令（设计 §5.4 白名单）：
  snapshot                 转码现状全量（活动任务 + 队列深度）
  queue.stats              队列深度
  ping                     探活
  subscribe                订阅全部任务状态事件（job.state 推送）
  unsubscribe              取消订阅
  pause <job_id>           暂停排队任务（状态先行原子占坑）
  resume <job_id>          恢复排队任务
  cancel <job_id>          取消排队任务
  log <job_id> [lines]     任务档案 + 相关审计尾部（默认 100 行，≤500）
  quit                     退出
`)
}

func pretty(b json.RawMessage) string {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}

func errMsg(b json.RawMessage) string {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &e) == nil {
		return e.Code + ": " + e.Message
	}
	return string(b)
}

func errCode(b json.RawMessage) string {
	var e struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(b, &e) == nil {
		return e.Code
	}
	return ""
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(b)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "[debugctl]", msg)
	os.Exit(2)
}
