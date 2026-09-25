package debug

// 调试会话与 JSON 信封命令分发（设计 §5.4）。
//
// 信封：
//
//	入站（agent→srv）：{"seq":1,"type":"hello","payload":{...}}
//	应答（srv→agent）：{"seq":1,"type":"result","payload":{...}}
//	                 | {"seq":1,"type":"error","payload":{"code","message"}}
//	事件（srv→agent）：{"evt":1,"type":"job.state","payload":{...}}   // 订阅推送
//
// 命令面 = 设计 §5.4 表内 8 类白名单；未知 type 一律 error/UNKNOWN_COMMAND。
// 每条命令审计 debug.cmd{channel_fp, cmd, job_id?, ok, latency_ms}（设计 §7.1）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"panoalbum/internal/audit"
	"panoalbum/internal/version"
)

// 命令名（入站 type 白名单）。
const (
	msgHello       = "hello"
	msgSnapshot    = "snapshot"
	msgSubscribe   = "subscribe"
	msgUnsubscribe = "unsubscribe"
	msgJobPause    = "job.pause"
	msgJobResume   = "job.resume"
	msgJobCancel   = "job.cancel"
	msgJobLogTail  = "job.log.tail"
	msgQueueStats  = "queue.stats"
	msgPing        = "ping"
)

// protoVer 协议版本（信封结构变更时 +1）。
const protoVer = 1

// maxLogTailLines job.log.tail 的 lines 上界（设计 §5.4：≤500，不提供 follow）。
const maxLogTailLines = 500

// envelope 入站/应答共用骨架。
type envelope struct {
	Seq  int             `json:"seq,omitempty"`
	Evt  int             `json:"evt,omitempty"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"payload,omitempty"`
}

// helloPayload hello 载荷。
type helloPayload struct {
	AgentVersion string `json:"agent_version"`
	Nonce        string `json:"nonce,omitempty"`
}

// session 单连接会话状态。
type session struct {
	h    *Handler
	ws   *websocket.Conn
	fp   string
	ip   string
	ctx  context.Context // 连接生命周期（命令执行不随单条消息取消）

	mu        sync.Mutex
	subJobs   bool // jobs:"*" 订阅
	evtSeq    int
	closeCode int
	dead      bool
}

func newSession(h *Handler, ws *websocket.Conn, fp, ip string, ctx context.Context) *session {
	return &session{h: h, ws: ws, fp: fp, ip: ip, ctx: ctx, closeCode: int(websocket.StatusNormalClosure)}
}

func (s *session) markDead(code int, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dead = true
	s.closeCode = code
}

// write 文本帧（并发安全）。
func (s *session) write(v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.ws.Write(ctx, websocket.MessageText, mustJSON(v))
}

// respondResult 命令成功应答。
func (s *session) respondResult(seq int, payload any) {
	s.write(envelope{Seq: seq, Type: "result", Data: rawJSON(payload)})
}

// respondError 命令失败应答（机器可读 code）。
func (s *session) respondError(seq int, code, msg string) {
	s.write(envelope{Seq: seq, Type: "error", Data: rawJSON(map[string]any{"code": code, "message": msg})})
}

// pushEvent 订阅推送（evt 自增）。
func (s *session) pushEvent(payload any) {
	s.mu.Lock()
	s.evtSeq++
	evt := envelope{Evt: s.evtSeq, Type: "job.state", Data: rawJSON(payload)}
	s.mu.Unlock()
	s.write(evt)
}

func rawJSON(v any) json.RawMessage {
	b, err := jsonMarshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(b)
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// envDuration 读秒数环境变量为 Duration（默认 def；非法回落 def）。
func envDuration(key string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return time.Duration(n) * time.Second
}

// closeWS 关闭 WS（int → StatusCode 适配；自定义关闭码 3000-4999 合法）。
func closeWS(ws *websocket.Conn, code int, reason string) {
	_ = ws.Close(websocket.StatusCode(code), reason)
}

// sayHello 应答 hello_ack。
func (s *session) sayHello(hp helloPayload) {
	s.respondResult(1, map[string]any{
		"server":      version.String(),
		"proto_ver":   protoVer,
		"server_time": time.Now().UTC().Format(time.RFC3339),
	})
}

// dispatch 命令分发（每命令独立审计 + 应答）。
func (s *session) dispatch(data []byte) {
	var env envelope
	if err := jsonUnmarshal(data, &env); err != nil || env.Type == "" {
		s.respondError(0, "BAD_ENVELOPE", "消息需为含 type 的 JSON 对象")
		return
	}
	started := time.Now()
	payload, jobID, err := s.exec(env)
	lat := time.Since(started).Milliseconds()

	detail := map[string]any{"channel_fp": s.fp, "cmd": env.Type, "ok": err == nil, "latency_ms": lat}
	if jobID != "" {
		detail["job_id"] = jobID
	}
	s.recordCmd(detail)

	if err != nil {
		s.respondError(env.Seq, errCode(err), err.Error())
		return
	}
	s.respondResult(env.Seq, payload)
}

// recordCmd debug.cmd 审计（尽力而为）。
func (s *session) recordCmd(detail map[string]any) {
	if s.h.Audit == nil {
		return
	}
	s.h.Audit.Record(context.Background(), audit.Entry{
		Action:     audit.ActionDebugCmd,
		IP:         s.ip,
		UserAgent:  "debug-agent",
		TargetType: audit.TargetDebugChannel,
		TargetID:   s.fp,
		Detail:     detail,
	})
}

// cmdError 命令错误（带机器可读 code）。
type cmdError struct {
	code string
	msg  string
}

func (e *cmdError) Error() string { return e.msg }

func errCode(err error) string {
	var ce *cmdError
	if errors.As(err, &ce) {
		return ce.code
	}
	return "INTERNAL"
}

func errf(code, format string, args ...any) error {
	return &cmdError{code: code, msg: fmt.Sprintf(format, args...)}
}
