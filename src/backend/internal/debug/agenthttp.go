package debug

// HTTP 命令面（Job000140 Phase 1，Agent 语义接口设计 §4/§5）：POST /admin/agent/cmd。
//
// 与 WSS 命令面（/debug/channel/:id，外部 debugctl 专用）的关系：同一套命令核心
// （commands_core.go），两个入口互不挤占——WSS 单连接槽不受 HTTP 面影响。
//
// 鉴权：路由层 authed + RequirePerm("admin:system")（与调试通道管理面同一把锁），
// 本文件不做第二套鉴权（恒假 FailClosed：无权限者根本到达不了 handler）。
//
// 限流：用户维度独立令牌桶 agent:cmd:<user_id>（突发 30、持续 1/s）——语义工具调用
// 低频，防 LLM 异常循环刷命令；Valkey 故障 fail-open（与 debug:hs 同决策，见 ratelimit.go）。
//
// 应答形态：项目标准 HTTP 语义——成功 200 + result 载荷；失败按错误码映射 HTTP 状态
// （UNKNOWN_COMMAND/INVALID_PARAMS 400、JOB_NOT_FOUND 404、INVALID_STATE 409、
// INTERNAL 500）+ httperr.Envelope{code,message}，前端 errMessage/errCode 直接可解析。
//
// 审计：每命令一条 agent.cmd{cmd, ok, latency_ms, job_id?}（detail 键名避开脱敏名单）。

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
	"panoalbum/internal/queue"
)

// 错误码 → HTTP 状态映射（与握手矩阵同一哲学：明确状态 + 机器可读 code）。
func agentCmdHTTPStatus(code string) int {
	switch code {
	case "UNKNOWN_COMMAND", "INVALID_PARAMS", "BAD_REQUEST":
		return http.StatusBadRequest
	case "JOB_NOT_FOUND":
		return http.StatusNotFound
	case "INVALID_STATE":
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// AgentCmdHandler HTTP 命令面（装配见 cmd/api/main.go）。
type AgentCmdHandler struct {
	Pool  *pgxpool.Pool
	TransQ *queue.Queue
	Audit  *audit.Recorder // 可为 nil（测试跳过）
	RL     *queue.RateLimiter // 用户维度命令桶
}

// agentCmdBody 请求体：{type, payload}。payload 兼容缺省（ping/snapshot 无参）。
type agentCmdBody struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Cmd POST /admin/agent/cmd。
func (h *AgentCmdHandler) Cmd(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	// ① 用户维度命令桶（fail-open）。
	if h.RL != nil {
		if allowed, err := h.RL.Allow(ctx, "agent:cmd:"+userID, 30, 1, 1); err == nil && !allowed {
			httperr.Envelope(c, http.StatusTooManyRequests, "AGENT_RATE_LIMITED", "命令过于频繁，请稍后再试")
			return
		}
	}

	// ② 解析请求体。
	var body agentCmdBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Type == "" {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为含 type 的 JSON 对象")
		return
	}

	// ③ 白名单校验（恒假 FailClosed：不在表内一律 400 UNKNOWN_COMMAND）。
	if _, ok := agentHTTPCommands[body.Type]; !ok {
		h.recordCmd(c, body.Type, "", false, 0)
		httperr.Envelope(c, http.StatusBadRequest, "UNKNOWN_COMMAND", "未知命令 "+body.Type)
		return
	}

	// ④ 命令分发（与 WSS 同一核心实现）。
	started := time.Now()
	var payload map[string]any
	var jobID string
	var cmdErr error
	switch body.Type {
	case msgPing:
		payload, jobID, cmdErr = cmdPingCore()
	case msgSnapshot:
		payload, jobID, cmdErr = cmdSnapshotCore(ctx, h.Pool, h.TransQ)
	case msgQueueStats:
		payload, jobID, cmdErr = cmdQueueStatsCore(ctx, h.TransQ)
	case msgJobPause, msgJobResume, msgJobCancel:
		payload, jobID, cmdErr = cmdJobControlCore(ctx, h.Pool, h.TransQ, body.Type, body.Payload)
	case msgJobLogTail:
		payload, jobID, cmdErr = cmdJobLogTailCore(ctx, h.Pool, body.Payload)
	default:
		cmdErr = errf("UNKNOWN_COMMAND", "未知命令 %q", body.Type) // 防御：白名单与 switch 失配
	}
	lat := time.Since(started).Milliseconds()

	// ⑤ 审计（每命令一条，尽力而为）。
	h.recordCmd(c, body.Type, jobID, cmdErr == nil, lat)

	// ⑥ 应答。
	if cmdErr != nil {
		code := errCode(cmdErr)
		httperr.Envelope(c, agentCmdHTTPStatus(code), code, cmdErr.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "cmd": body.Type, "result": payload})
}

// recordCmd agent.cmd 审计。actor 来自 gin 上下文（管理员本人）；
// target_type=agent_cmd，target_id=命令名，job_id 进 detail（避开敏感子串键名）。
func (h *AgentCmdHandler) recordCmd(c *gin.Context, cmd, jobID string, ok bool, latencyMS int64) {
	if h.Audit == nil {
		return
	}
	detail := map[string]any{"cmd": cmd, "ok": ok, "latency_ms": latencyMS}
	if jobID != "" {
		detail["job_id"] = jobID
	}
	e := audit.FromGin(c)
	e.Action = audit.ActionAgentCmd
	e.TargetType = audit.TargetAgentCmd
	e.TargetID = cmd
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}
