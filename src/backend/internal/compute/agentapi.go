package compute

// 节点侧 HTTP 端点：心跳 / 任务拉取 / 结果回传（TDD v1.1 §6.1 的协议三件事）。
//
// ⚠️ 鉴权模型的选择与理由（这是本文件最需要说清的设计决定）
//
// 节点 agent **不是用户**，因此**不复用 JWT 用户令牌**，走独立的 AgentAuth 中间件：
//
//  1. 身份性质不同。JWT 的 claims 是 user_id + role，整套 RBAC（role_permissions）以
//     "角色 → 权限"建模。节点没有 user_id、没有角色、不进任何角色表；硬把它塞进 users
//     表会污染用户体系（用户列表、分享归属、审计日志都会多出一个机器账号），
//     而且它的"权限"是恒定的（只能心跳/拉任务/回传自己的任务），用 RBAC 表达只是空转。
//
//  2. 生命周期不同。JWT 为人类交互设计：短期 access + refresh 轮换、登录/登出、
//     改密码即全端失效。节点令牌是**长期驻留的机器凭据**，需要「单节点吊销/轮换」
//     （TDD §6.1「agent_token 轮换（管理员触发）」）。走 JWT 会带来两个坏结果：
//     要么节点必须持有可换取 access 的 refresh 令牌（等于一把能升级成用户身份的钥匙），
//     要么节点令牌的有效期受 access TTL 约束而不得不频繁重认证。按节点令牌校验则
//     吊销 = 删/换该行的 agent_token_hash，粒度精确到一个节点。
//
//  3. 故障隔离。用户令牌泄漏影响的是某个人的数据；节点令牌泄漏影响的是"谁能领任务"。
//     两者混用会让审计无法区分"某用户调了接口"与"某节点调了接口"。独立中间件会把
//     node_id 注进上下文，任务归属（transcode_jobs.node_id）天然可追溯到具体节点。
//
//  4. TDD 已经指明。§6.1 注册一行写的是「连接时 Authorization: Bearer <agent_token>」
//     —— 同一个 header、不同的凭据语义，正是"同一传输、不同认证平面"的做法。
//
// 因此：管理端 4 个 /compute-nodes 路由走 admin:system（人操作），节点侧 3 个
// /compute-nodes/agent/* 路由走 AgentAuth（机器凭据）。两条平面在 main.go 里分开装配。

import (
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// nodeIDKey gin 上下文中节点 id 的键（由 AgentAuth 注入）。
const nodeIDKey = "compute_node_id"

// AgentAuth 节点令牌鉴权中间件。
//
// 校验链：Authorization: Bearer <明文令牌> → sha256 → 按哈希查 compute_nodes
// → 校验未过期 → 注入 node_id。
//
// 库中只有哈希，没有明文；明文令牌在 SQL 参数里也不出现，因此日志/慢查询不会留下可用凭据。
// 失败一律 401 且**不区分**"无此令牌"与"令牌过期"（避免给枚举者反馈）。
func AgentAuth(store *Store, offlineAfter time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "缺少节点令牌")
			c.Abort()
			return
		}
		plain := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if plain == "" {
			fail(c, http.StatusUnauthorized, CodeUnauthorized, "缺少节点令牌")
			c.Abort()
			return
		}

		node, tokenHash, expiresAt, err := store.FindByTokenHash(c.Request.Context(), HashAgentToken(plain), offlineAfter)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				fail(c, http.StatusUnauthorized, CodeInvalidAgentToken, "节点令牌无效或已过期")
				c.Abort()
				return
			}
			log.Printf("compute: 节点令牌校验失败: %v", err)
			fail(c, http.StatusInternalServerError, CodeInternal, "校验节点令牌失败")
			c.Abort()
			return
		}
		if !TokenValid(tokenHash, plain, expiresAt, time.Now()) {
			fail(c, http.StatusUnauthorized, CodeInvalidAgentToken, "节点令牌无效或已过期")
			c.Abort()
			return
		}

		c.Set(nodeIDKey, node.ID)
		c.Set("compute_node", node)
		c.Next()
	}
}

// AgentHandler 节点侧端点。Store 不可为空。
type AgentHandler struct {
	Store *Store

	// OfflineAfter / HeartbeatInterval 回给节点，让节点按服务端口径对齐节奏，
	// 避免节点侧自造一套与 TDD 不一致的超时参数。<= 0 时用 Default* 常量。
	OfflineAfter      time.Duration
	HeartbeatInterval time.Duration
}

func (h *AgentHandler) offlineAfter() time.Duration {
	if h.OfflineAfter <= 0 {
		return DefaultOfflineAfter
	}
	return h.OfflineAfter
}

func (h *AgentHandler) heartbeatInterval() time.Duration {
	if h.HeartbeatInterval <= 0 {
		return DefaultHeartbeatInterval
	}
	return h.HeartbeatInterval
}

// nodeID 取由 AgentAuth 注入的节点 id。
func nodeID(c *gin.Context) string { return c.GetString(nodeIDKey) }

// Heartbeat POST /compute-nodes/agent/heartbeat
// ← {status?, codecs?, has_nvenc?, vram_mb?, concurrency?, gpu_util?, vram_used_mb?, active_tasks}
// → {node_id, status, server_time, heartbeat_interval_seconds, offline_after_seconds}
//
// 能力字段（codecs/has_nvenc/vram_mb/concurrency）只在非空时覆盖库中记录，
// 对应 TDD §6.1「重连后重新注册能力」：节点首个心跳即携带完整能力声明，
// 之后的心跳只续命与上报负载。
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	var hb HeartbeatRequest
	if err := c.ShouldBindJSON(&hb); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "请求体格式错误")
		return
	}

	node, err := h.Store.Heartbeat(c.Request.Context(), nodeID(c), hb, h.offlineAfter())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			fail(c, http.StatusNotFound, CodeNodeNotFound, "节点不存在（可能已被删除）")
			return
		}
		log.Printf("compute: 更新心跳失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "更新心跳失败")
		return
	}

	c.JSON(http.StatusOK, HeartbeatResponse{
		NodeID:                   node.ID,
		Status:                   string(node.EffectiveStatus),
		ServerTime:               time.Now().UTC(),
		HeartbeatIntervalSeconds: int(h.heartbeatInterval().Seconds()),
		OfflineAfterSeconds:      int(h.offlineAfter().Seconds()),
	})
}

// Poll POST /compute-nodes/agent/poll ← {max?} → {jobs:[...], server_time}
//
// 无任务时返回空数组（不是 null），节点侧无需为空的两种情况写分支。
//
// 顺序是**先回收、再认领**：回收把僵死节点名下卡在 running 的任务放回 pending，
// 紧接着的认领就能把刚刚释放出来的任务交给本节点，避免多等一个轮询周期。
func (h *AgentHandler) Poll(c *gin.Context) {
	var req PollRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "请求体格式错误")
		return
	}

	// 回收僵死任务（见 Store.ReclaimStale）。触发点是"有节点来拉任务"，不引入定时任务。
	// 同一趟还会结算"认领次数已耗尽"的任务（判 failed），否则它们会永远停在 pending。
	if res, err := h.Store.ReclaimStale(c.Request.Context(), h.offlineAfter()); err != nil {
		// 回收失败**不阻断**本次拉取：它只影响"僵尸任务能否被重派"，
		// 而阻断拉取会让本来健康的节点也一起停摆 —— 故障面被放大。
		log.Printf("compute: 回收僵死任务失败（不影响本次拉取）: %v", err)
	} else {
		if len(res.Requeued) > 0 {
			log.Printf("compute: 回收 %d 个僵死任务（原节点心跳静默超时 / 被管理员下线）: %v",
				len(res.Requeued), res.Requeued)
		}
		if len(res.Exhausted) > 0 {
			log.Printf("compute: %d 个任务认领次数达上限，已判 failed（不再无限重派）: %v",
				len(res.Exhausted), res.Exhausted)
		}
	}

	jobs, err := h.Store.PollJobs(c.Request.Context(), nodeID(c), req.Max)
	if err != nil {
		log.Printf("compute: 拉取任务失败: %v", err)
		fail(c, http.StatusInternalServerError, CodeInternal, "拉取任务失败")
		return
	}
	c.JSON(http.StatusOK, PollResponse{Jobs: jobs, ServerTime: time.Now().UTC()})
}

// Result POST /compute-nodes/agent/result ← {job_id, status, result_path?, error?} → {ok:true}
func (h *AgentHandler) Result(c *gin.Context) {
	var r ResultRequest
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "请求体格式错误")
		return
	}

	if err := h.Store.SubmitResult(c.Request.Context(), nodeID(c), r); err != nil {
		switch {
		case errors.Is(err, ErrJobFinalized):
			// 409 而不是 404：任务确实存在，只是已结束（正常竞态）。节点侧据此
			// 不必怀疑自己的任务 id，也不必重试。
			fail(c, http.StatusConflict, CodeJobFinalized, "任务已是终态，回传被拒绝")
		case errors.Is(err, ErrJobNotFound):
			fail(c, http.StatusNotFound, CodeJobNotFound, "任务不存在或不属于本节点")
		case errors.Is(err, ErrInvalidInput):
			fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())
		default:
			log.Printf("compute: 回传任务结果失败: %v", err)
			fail(c, http.StatusInternalServerError, CodeInternal, "回传任务结果失败")
		}
		return
	}
	// 失败原因不落库（transcode_jobs 无对应列），此处记进服务端日志便于排障。
	if r.Status == JobStatusFailed {
		log.Printf("compute: 节点 %s 回传任务 %s 失败: %s", nodeID(c), r.JobID, r.Error)
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
