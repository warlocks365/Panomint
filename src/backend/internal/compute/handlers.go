package compute

// 管理端 HTTP 端点（契约 v1.1 §15 的 4 个 /compute-nodes 路由）。
//
// 鉴权：这 4 个端点由**人**（管理员界面）调用，走 JWT + admin:system 权限，
// 由 cmd/api/main.go 装配时套 auth.AuthRequired 与 auth.RequirePerm(admin:system)。
// 节点侧的 3 个 agent 端点（agentapi.go）走完全不同的鉴权——两者不得混用，
// 详见 agentapi.go 顶部的说明。
//
// 装配（cmd/api/main.go，属主为集成方；本文件不注册路由）：
//
//	computeH := &compute.Handler{Store: &compute.Store{Pool: pool}, OfflineAfter: compute.OfflineAfterFromEnv()}
//	computeAgent := &compute.AgentHandler{Store: computeH.Store, OfflineAfter: computeH.OfflineAfter,
//		HeartbeatInterval: compute.DefaultHeartbeatInterval}
//	adminSystem := authed.Group("/compute-nodes", auth.RequirePerm(authStore, "admin:system"))
//	adminSystem.GET("", computeH.List)
//	adminSystem.POST("", computeH.Register)
//	adminSystem.PATCH("/:id", computeH.Patch)
//	adminSystem.DELETE("/:id", computeH.Delete)
//	// 节点侧（无 JWT，按 agent_token 校验）
//	agentPlane := r.Group("/compute-nodes/agent", compute.AgentAuth(computeH.Store, computeH.OfflineAfter))
//	agentPlane.POST("/heartbeat", computeAgent.Heartbeat)
//	agentPlane.POST("/poll", computeAgent.Poll)
//	agentPlane.POST("/result", computeAgent.Result)

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
)

// Handler 管理端节点端点。Store 不可为空。
type Handler struct {
	Store *Store

	// OfflineAfter 心跳静默多久视为离线（见 offline.go）。<= 0 时回落 DefaultOfflineAfter。
	OfflineAfter time.Duration
}

// offlineAfter 归一化阈值。
func (h *Handler) offlineAfter() time.Duration {
	if h.OfflineAfter <= 0 {
		return DefaultOfflineAfter
	}
	return h.OfflineAfter
}

// fail 统一错误响应 {"error":{"code","message"}}，与 media / faces 包保持一致。
// 形状委托给 internal/httperr（单一真源）。
func fail(c *gin.Context, status int, code, msg string) {
	httperr.Envelope(c, status, code, msg)
}

// failStore 把 Store 错误映射为 HTTP 状态码，未识别的错误记日志后返回 500。
func failStore(c *gin.Context, err error, what string) {
	switch {
	case errors.Is(err, ErrNotFound):
		fail(c, http.StatusNotFound, CodeNodeNotFound, "节点不存在")
	case errors.Is(err, ErrNodeInUse):
		fail(c, http.StatusConflict, CodeNodeBusy, "该节点仍被转码任务引用，请先处理其任务或改判 node_id")
	case errors.Is(err, ErrInvalidInput):
		fail(c, http.StatusBadRequest, CodeInvalidInput, err.Error())
	default:
		log.Printf("compute: %s 失败: %v", what, err)
		fail(c, http.StatusInternalServerError, CodeInternal, what+"失败")
	}
}

// List GET /compute-nodes → { nodes:[...] }
//
// 返回的 status 同时给出两个字段：status（库中存储态）与 effective_status（按心跳超时
// 修正后的生效态）。界面应以后者为准；保留前者是为了让"刚被超时判定为离线、但进程其实
// 还在跑"这类情况可诊断。
func (h *Handler) List(c *gin.Context) {
	nodes, err := h.Store.List(c.Request.Context(), h.offlineAfter())
	if err != nil {
		failStore(c, err, "查询节点列表")
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": nodes})
}

// Register POST /compute-nodes ← {name,kind,host?,codecs,has_nvenc,vram_mb,concurrency}
// → { node:{...}, agent_token? }
//
// agent_token 仅在 kind=lan_agent 时出现，且**只在这一刻返回**（库中只存 sha256 哈希）。
// 调用方必须当场把令牌交给节点；之后任何接口都无法再取回明文，遗失只能轮换。
func (h *Handler) Register(c *gin.Context) {
	var in RegisterInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "请求体格式错误")
		return
	}
	node, token, err := h.Store.Register(c.Request.Context(), in)
	if err != nil {
		failStore(c, err, "登记节点")
		return
	}
	resp := gin.H{"node": node}
	if token != "" {
		resp["agent_token"] = token
	}
	c.JSON(http.StatusCreated, resp)
}

// Patch PATCH /compute-nodes/:id → { node:{...}, agent_token? }
//
// 支持上下线（status=offline/online）、改能力，以及 rotate_token=true 轮换接入令牌
// （TDD §6.1「agent_token 轮换（管理员触发）」）。轮换出的新明文同样只返回这一次。
func (h *Handler) Patch(c *gin.Context) {
	var in PatchInput
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, CodeInvalidInput, "请求体格式错误")
		return
	}
	node, token, err := h.Store.Patch(c.Request.Context(), c.Param("id"), in)
	if err != nil {
		failStore(c, err, "更新节点")
		return
	}
	resp := gin.H{"node": node}
	if token != "" {
		resp["agent_token"] = token
	}
	c.JSON(http.StatusOK, resp)
}

// Delete DELETE /compute-nodes/:id
func (h *Handler) Delete(c *gin.Context) {
	if err := h.Store.Delete(c.Request.Context(), c.Param("id")); err != nil {
		failStore(c, err, "删除节点")
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": c.Param("id")})
}
