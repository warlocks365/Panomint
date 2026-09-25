package debug

// WSS 调试通道（设计 §5）：握手前置矩阵 + 单连接槽 + JSON 信封命令循环 + 心跳 + 到期 reaper。
//
// 为什么选 coder/websocket（设计附录 A·Q2）：context 一等公民（取消/超时天然正确）、
// 并发读写约束在文档层清晰、MIT、无 cgo。
//
// 单连接策略（Q3）：进程内 Hub 一个槽；第二个连接在 upgrade 前 409。
// ⚠️ 单 api 副本前提（设计 §8.3）：槽是进程内易失态，compose 现状=单 api 容器成立；
// 未来多副本须引入 Valkey 分布式连接锁（契约已列改造项）。

import (
	"context"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
)

// channelIDRe 路径段白名单（设计 §5.2 步骤 1）。
var channelIDRe = regexp.MustCompile(`^[a-f0-9]{24}$`)

// maxFrameBytes 单帧上限 1MB（设计 §9 消息超大→1007）。
const maxFrameBytes = 1 << 20

// ---------------------------------------------------------------------------
// Hub：进程内单连接槽
// ---------------------------------------------------------------------------

type slot struct {
	ws      *websocket.Conn // nil = 已预留未 upgrade
	fp      string
	since   time.Time
	writeMu sync.Mutex
}

// Hub 单连接槽。
type Hub struct {
	mu  sync.Mutex
	cur *slot
}

// NewHub 创建连接槽。
func NewHub() *Hub { return &Hub{} }

// Reserve 预留槽位（upgrade 前调用；第二个连接 409）。返回释放函数。
func (h *Hub) Reserve() (release func(), ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != nil {
		return nil, false
	}
	s := &slot{since: time.Now()}
	h.cur = s
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.cur == s && s.ws == nil {
			h.cur = nil
		}
	}, true
}

// Attach 预留升级：把 WS 连接登记进槽（握手全部通过后的最后一步）。
func (h *Hub) Attach(ws *websocket.Conn, fp string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != nil && h.cur.ws == nil {
		h.cur.ws = ws
		h.cur.fp = fp
	}
}

// Connected 是否有活动连接（管理端 status 的 connected 字段）。
func (h *Hub) Connected() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur != nil && h.cur.ws != nil
}

// FingerprintOf 当前连接指纹（无连接返回 ""）。
func (h *Hub) FingerprintOf() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur == nil {
		return ""
	}
	return h.cur.fp
}

// Detach 断开时清槽（仅当还是本人）。
func (h *Hub) Detach(ws *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != nil && h.cur.ws == ws {
		h.cur = nil
	}
}

// Kick 踢除当前连接（管理端 disable/rotate/reaper 用）。无连接时 no-op。
func (h *Hub) Kick(code int, reason string) {
	h.mu.Lock()
	s := h.cur
	h.mu.Unlock()
	if s == nil || s.ws == nil {
		return
	}
	s.writeMu.Lock()
	closeWS(s.ws, code, reason)
	s.writeMu.Unlock()
}

// SendEvent 向当前连接推送事件（订阅推送用）；无连接或写失败返回 false。
func (h *Hub) SendEvent(v any) bool {
	h.mu.Lock()
	s := h.cur
	h.mu.Unlock()
	if s == nil || s.ws == nil {
		return false
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.ws.Write(ctx, websocket.MessageText, mustJSON(v)) == nil
}

func mustJSON(v any) []byte {
	b, err := jsonMarshal(v)
	if err != nil {
		return []byte(`{"type":"error","payload":{"code":"INTERNAL","message":"序列化失败"}}`)
	}
	return b
}

// ---------------------------------------------------------------------------
// 握手（设计 §5.2 前置矩阵）
// ---------------------------------------------------------------------------

// Channel GET /debug/channel/:channel_id —— WSS upgrade 端点。
// 矩阵次序（失败即返回，绝不在鉴权前 upgrade）：
//
//	① 格式 400 → ② 限流桶 429（fail-open）→ ③ 未开启 404 → ④ 槽占 409
//	→ ⑤ Bearer 401 → ⑥ 失败锁定 429（fail-open）→ ⑦ 认证（404/401+计数）
//	→ ⑧ TLS 426 → ⑨ upgrade + 登记
func (h *Handler) Channel(c *gin.Context) {
	channelID := c.Param("channel_id")
	if !channelIDRe.MatchString(channelID) {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "非法通道 id")
		return
	}
	ip := c.ClientIP()
	ctx := c.Request.Context()

	// ② 握手专用桶（设计 §6.5；Valkey 故障 fail-open 放行——见 ratelimit.go 文件头）。
	if allowed, err := CheckHandshake(ctx, h.RL, ip); err == nil && !allowed {
		httperr.Envelope(c, http.StatusTooManyRequests, "DEBUG_RATE_LIMITED", "握手过于频繁，请稍后再试")
		return
	}

	// ③ 未开启 = 404（不存在/已关闭/已过期一视同仁，不可探测）。
	ch, err := h.Store.PeekByChannelID(ctx, channelID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if ch == nil || !ch.Enabled {
		httperr.Envelope(c, http.StatusNotFound, "NOT_FOUND", "调试通道不存在")
		return
	}

	// ④ 单连接槽。
	release, ok := h.Hub.Reserve()
	if !ok {
		httperr.Envelope(c, http.StatusConflict, "DEBUG_ALREADY_CONNECTED", "已有活动调试连接")
		return
	}
	failed := true
	defer func() {
		if failed {
			release()
		}
	}()

	// ⑤ Bearer 形态。
	auth := c.GetHeader("Authorization")
	key := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	if !strings.HasPrefix(auth, "Bearer ") || key == "" {
		httperr.Envelope(c, http.StatusUnauthorized, "DEBUG_AUTH_REQUIRED", "缺少调试密钥")
		return
	}

	// ⑥ 失败锁定（fail-open）。
	if locked, err := h.Locker.Locked(ctx, ip); err == nil && locked {
		h.recordAgent(c, audit.ActionDebugLocked, nil, map[string]any{
			"peer_ip":  ip,
			"window_s": int(lockDuration.Seconds()),
		})
		httperr.Envelope(c, http.StatusTooManyRequests, "DEBUG_LOCKED", "认证失败过多，已临时锁定")
		return
	}

	// ⑦ 认证（过期/未开启 404 同形；摘要失败 401 + 计数）。
	ch, err = h.Store.Authenticate(ctx, channelID, key, ip, time.Now())
	switch {
	case err == nil:
		// 通过
	case errors.Is(err, ErrExpired):
		h.recordAgent(c, audit.ActionDebugDisable, &channelID, map[string]any{
			"channel_fp": Fingerprint(channelID),
			"reason":     "expired",
		})
		httperr.Envelope(c, http.StatusNotFound, "NOT_FOUND", "调试通道不存在")
		return
	case errors.Is(err, ErrNotEnabled):
		httperr.Envelope(c, http.StatusNotFound, "NOT_FOUND", "调试通道不存在")
		return
	case errors.Is(err, ErrAuthFailed):
		h.registerFailureAndAudit(c, ip, channelID)
		httperr.Envelope(c, http.StatusUnauthorized, "DEBUG_AUTH_FAILED", "调试密钥无效或已过期")
		return
	default:
		httperr.Fail(c, http.StatusInternalServerError, "AUTH_FAILED", "认证失败", err)
		return
	}

	// ⑧ TLS：生产仅 WSS（设计 §6.2）；insecure 开关仅开发用，且仍限回环来源。
	if c.Request.TLS == nil && !h.allowInsecureWSFrom(ip) {
		httperr.Envelope(c, http.StatusUpgradeRequired, "DEBUG_TLS_REQUIRED", "调试通道仅允许 WSS 接入")
		return
	}

	// ⑨ upgrade。
	ws, err := websocket.Accept(c.Writer, c.Request, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Origin 不校验：agent 是 CLI 非浏览器，无 Origin 概念
	})
	if err != nil {
		log.Printf("[debug] WS upgrade 失败: %v", err)
		return
	}
	failed = false
	h.Hub.Attach(ws, Fingerprint(channelID))
	h.runSession(ws, Fingerprint(channelID), ip)
}

// allowInsecureWSFrom DEBUG_ALLOW_INSECURE_WS 放行时仍限回环来源（设计 §6.2）。
func (h *Handler) allowInsecureWSFrom(ip string) bool {
	return h.AllowInsecureWS && (ip == "127.0.0.1" || ip == "::1" || ip == "localhost")
}

// registerFailureAndAudit 失败计数 + 触顶审计（Valkey 故障静默——fail-open）。
func (h *Handler) registerFailureAndAudit(c *gin.Context, ip, channelID string) {
	locked, err := h.Locker.RegisterFailure(c.Request.Context(), ip)
	if err != nil {
		return
	}
	h.recordAgent(c, audit.ActionDebugAuthFail, &channelID, map[string]any{
		"channel_fp": Fingerprint(channelID),
		"peer_ip":    ip,
		"reason":     "bad_key",
	})
	if locked {
		h.recordAgent(c, audit.ActionDebugLocked, &channelID, map[string]any{
			"peer_ip":  ip,
			"window_s": int(lockDuration.Seconds()),
		})
	}
}

// recordAgent agent 触发事件审计。
//
// ⚠️ actor 只能为空（audit.NormalizeActor 只收 UUID，"agent:<fp>" 会被拒写）——
// 可追溯链由 target_id=channel_fp + detail.channel_fp 承载（设计 §7.2 的意图不变）。
func (h *Handler) recordAgent(c *gin.Context, action string, channelID *string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.Entry{
		Action:     action,
		IP:         c.ClientIP(),
		UserAgent:  "debug-agent",
		TargetType: audit.TargetDebugChannel,
		Detail:     detail,
	}
	if channelID != nil {
		e.TargetID = Fingerprint(*channelID)
	}
	h.Audit.Record(c.Request.Context(), e)
}

// ---------------------------------------------------------------------------
// 会话：命令循环 + 心跳 + 断开审计
// ---------------------------------------------------------------------------

// runSession 单连接的读写循环（upgrade 成功后调用，阻塞至连接结束）。
func (h *Handler) runSession(ws *websocket.Conn, fp, ip string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	sess := newSession(h, ws, fp, ip, ctx)
	defer func() {
		h.Hub.Detach(ws)
		dur := int(time.Since(started).Seconds())
		if h.Audit != nil {
			h.Audit.Record(context.Background(), audit.Entry{
				Action:     audit.ActionDebugAgentDisconnect,
				IP:         ip,
				UserAgent:  "debug-agent",
				TargetType: audit.TargetDebugChannel,
				TargetID:   fp,
				Detail:     map[string]any{"channel_fp": fp, "duration_s": dur, "close_code": sess.closeCode},
			})
		}
	}()

	// 心跳：WS ping；单次失败即判死踢除（设计 §9：ping 无 pong → Close 1001）。
	go func() {
		t := time.NewTicker(h.pingInterval())
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, h.pingInterval())
				err := ws.Ping(pctx)
				pcancel()
				if err != nil {
					sess.markDead(CloseGoingAway, "心跳超时")
					cancel()
					return
				}
			}
		}
	}()

	// 首消息须为 hello（15s 超时）；agent_version 记入 connect 审计。
	ws.SetReadLimit(maxFrameBytes)
	helloCtx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	mt, raw, err := ws.Read(helloCtx)
	hcancel()
	if err != nil || mt != websocket.MessageText {
		sess.markDead(CloseUnsupported, "首消息须为 hello")
		closeWS(ws, CloseUnsupported, "首消息须为 hello")
		return
	}
	var env envelope
	if err := jsonUnmarshal(raw, &env); err != nil || env.Type != msgHello {
		sess.markDead(CloseUnsupported, "首消息须为 hello")
		closeWS(ws, CloseUnsupported, "首消息须为 hello")
		return
	}
	var hp helloPayload
	if err := jsonUnmarshal(env.Data, &hp); err != nil {
		sess.markDead(CloseUnsupported, "hello 载荷非法")
		closeWS(ws, CloseUnsupported, "hello 载荷非法")
		return
	}
	if h.Audit != nil {
		h.Audit.Record(context.Background(), audit.Entry{
			Action:     audit.ActionDebugAgentConnect,
			IP:         ip,
			UserAgent:  "debug-agent/" + hp.AgentVersion,
			TargetType: audit.TargetDebugChannel,
			TargetID:   fp,
			Detail: map[string]any{
				"channel_fp":  fp,
				"agent_version": hp.AgentVersion,
				"peer_ip":       ip,
			},
		})
	}
	sess.sayHello(hp)

	// 命令循环。
	for {
		msgType, raw, err := ws.Read(ctx)
		if err != nil {
			code := CloseGoingAway
			if cs := websocket.CloseStatus(err); cs >= 0 {
				code = int(cs)
			}
			sess.markDead(code, "连接结束")
			closeWS(ws, code, "连接结束")
			return
		}
		if msgType != websocket.MessageText {
			sess.markDead(CloseUnsupported, "仅接受文本帧")
			closeWS(ws, CloseUnsupported, "仅接受文本帧")
			return
		}
		if len(raw) > maxFrameBytes {
			sess.markDead(CloseMessageTooBig, "消息超限")
			closeWS(ws, CloseMessageTooBig, "消息超限")
			return
		}
		sess.dispatch(raw)
	}
}

// pingInterval 心跳周期（env DEBUG_WS_PING_INTERVAL 秒，默认 30s）。
func (h *Handler) pingInterval() time.Duration {
	return envDuration("DEBUG_WS_PING_INTERVAL", 30*time.Second)
}

// reaperInterval 到期巡检周期（env DEBUG_REAPER_INTERVAL 秒，默认 300s）。
func (h *Handler) reaperInterval() time.Duration {
	return envDuration("DEBUG_REAPER_INTERVAL", 300*time.Second)
}

// StartReaper 到期巡检（设计 §4.2 条件 1 检测点 ②）：周期扫描 ExpireDue，
// 命中即踢连 4001 + 审计。阻塞调用方应自行 goroutine 化；ctx 取消即停。
func (h *Handler) StartReaper(ctx context.Context) {
	t := time.NewTicker(h.reaperInterval())
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			expired, fp, err := h.Store.ExpireDue(ctx, time.Now())
			if err != nil {
				log.Printf("[debug] reaper 巡检失败: %v", err)
				continue
			}
			if expired {
				h.Hub.Kick(CloseExpired, "调试通道已过期")
				if h.Audit != nil {
					h.Audit.Record(context.Background(), audit.Entry{
						Action:     audit.ActionDebugDisable,
						TargetType: audit.TargetDebugChannel,
						TargetID:   fp,
						Detail:     map[string]any{"channel_fp": fp, "reason": "expired"},
					})
				}
			}
		}
	}
}
