package compute

// 路由装配形状测试。
//
// 为什么值得单测：本交付项不修改 cmd/api/main.go（路由由集成方统一注册），
// 于是「这两组路由能不能共存于同一个 gin 引擎」就成了唯一无法在包内直接验证、
// 却又最容易踩的坑——gin 的 radix 树在同一层同时出现**静态段与通配段**时会 panic
// （例如 POST /compute-nodes/agent 与 POST /compute-nodes/:id 并存）。
//
// 本测试用与汇报中给出的**完全一致**的注册代码建一棵路由树，从而把
// 「照抄了汇报里的那几行会不会启动即 panic」这件事在提交前就钉死。

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// buildTestRouter 复刻汇报中给出的路由注册形状。
func buildTestRouter(h *Handler, agent *AgentHandler, store *Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// 管理端平面：真实项目里这一组挂在 authed（JWT + RequirePerm("admin:system")）之下。
	adminNodes := r.Group("/compute-nodes")
	adminNodes.GET("", h.List)
	adminNodes.POST("", h.Register)
	adminNodes.PATCH("/:id", h.Patch)
	adminNodes.DELETE("/:id", h.Delete)

	// 节点侧平面：必须挂在根引擎（不走 JWT），由 AgentAuth 按 agent_token 校验。
	agentPlane := r.Group("/compute-nodes/agent", AgentAuth(store, DefaultOfflineAfter))
	agentPlane.POST("/heartbeat", agent.Heartbeat)
	agentPlane.POST("/poll", agent.Poll)
	agentPlane.POST("/result", agent.Result)

	return r
}

// TestRouteRegistrationShape 管理端 4 条 + 节点侧 3 条必须能同时注册（不 panic）。
//
// 关键约束：管理端的通配段只有 :id（且只出现在 PATCH/DELETE），
// 节点侧只在 POST 下引入静态段 agent —— 两者不在同一方法的同一层冲突。
// 因此**不要**为令牌轮换新增 POST /compute-nodes/:id/... 之类的路由，
// 否则会与 /compute-nodes/agent 冲突（轮换已并入 PATCH 的 rotate_token 字段）。
func TestRouteRegistrationShape(t *testing.T) {
	store := &Store{} // Pool 为 nil：本测试只验证路由形状与不需要 DB 的分支
	h := &Handler{Store: store, OfflineAfter: DefaultOfflineAfter}
	agent := &AgentHandler{Store: store, OfflineAfter: DefaultOfflineAfter, HeartbeatInterval: DefaultHeartbeatInterval}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("路由注册 panic（gin 静态段与通配段冲突）：%v", r)
		}
	}()

	r := buildTestRouter(h, agent, store)

	want := map[string][]string{
		http.MethodGet:    {"/compute-nodes"},
		http.MethodPost:   {"/compute-nodes", "/compute-nodes/agent/heartbeat", "/compute-nodes/agent/poll", "/compute-nodes/agent/result"},
		http.MethodPatch:  {"/compute-nodes/:id"},
		http.MethodDelete: {"/compute-nodes/:id"},
	}
	got := map[string]map[string]bool{}
	for _, ri := range r.Routes() {
		if got[ri.Method] == nil {
			got[ri.Method] = map[string]bool{}
		}
		got[ri.Method][ri.Path] = true
	}
	for method, paths := range want {
		for _, p := range paths {
			if !got[method][p] {
				t.Fatalf("路由 %s %s 未注册成功（实际 %v %v）", method, p, method, got[method])
			}
		}
	}
}

// TestAgentPlaneRejectsWithoutToken 无 Authorization 头必须 401，且**不触库**
// （Store.Pool 为 nil，若中间件先查库这里就会 panic 而不是返回 401）。
func TestAgentPlaneRejectsWithoutToken(t *testing.T) {
	store := &Store{}
	h := &Handler{Store: store}
	agent := &AgentHandler{Store: store, OfflineAfter: DefaultOfflineAfter, HeartbeatInterval: DefaultHeartbeatInterval}
	r := buildTestRouter(h, agent, store)

	for _, path := range []string{"/compute-nodes/agent/heartbeat", "/compute-nodes/agent/poll", "/compute-nodes/agent/result"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s 无令牌应返回 401，实际 %d（body=%s）", path, w.Code, w.Body.String())
		}
	}
}

// TestAgentPlaneRejectsMalformedHeader 非 Bearer 前缀同样 401。
func TestAgentPlaneRejectsMalformedHeader(t *testing.T) {
	store := &Store{}
	h := &Handler{Store: store}
	agent := &AgentHandler{Store: store, OfflineAfter: DefaultOfflineAfter, HeartbeatInterval: DefaultHeartbeatInterval}
	r := buildTestRouter(h, agent, store)

	cases := []string{"", "Basic abc", "Bearer", "Bearer   ", "token abc"}
	for _, hdr := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/compute-nodes/agent/poll", nil)
		if hdr != "" {
			req.Header.Set("Authorization", hdr)
		}
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q 应返回 401，实际 %d", hdr, w.Code)
		}
	}
}

// TestHandlerOfflineAfterDefaults 阈值归一化：<= 0 一律回落 DefaultOfflineAfter。
func TestHandlerOfflineAfterDefaults(t *testing.T) {
	for _, v := range []time.Duration{0, -1, -time.Hour} {
		h := &Handler{OfflineAfter: v}
		if got := h.offlineAfter(); got != DefaultOfflineAfter {
			t.Fatalf("Handler.OfflineAfter=%s 应回落 %s，实际 %s", v, DefaultOfflineAfter, got)
		}
		a := &AgentHandler{OfflineAfter: v, HeartbeatInterval: v}
		if got := a.offlineAfter(); got != DefaultOfflineAfter {
			t.Fatalf("AgentHandler.offlineAfter() 应回落 %s，实际 %s", DefaultOfflineAfter, got)
		}
		if got := a.heartbeatInterval(); got != DefaultHeartbeatInterval {
			t.Fatalf("AgentHandler.heartbeatInterval() 应回落 %s，实际 %s", DefaultHeartbeatInterval, got)
		}
	}
}

// TestFailEnvelope 错误响应包络必须与项目其它包一致（{"error":{code,message}}）。
func TestFailEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", func(c *gin.Context) { fail(c, http.StatusTeapot, CodeInvalidInput, "参数不对") })

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))

	if w.Code != http.StatusTeapot {
		t.Fatalf("状态码应为 418，实际 %d", w.Code)
	}
	want := `{"error":{"code":"INVALID_INPUT","message":"参数不对"}}`
	if w.Body.String() != want {
		t.Fatalf("包络不匹配：\n得到 %s\n期望 %s", w.Body.String(), want)
	}
}

// TestFailStoreMapping Store 错误到 HTTP 状态码的映射（集成方据此判断响应语义）。
func TestFailStoreMapping(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ErrNotFound, http.StatusNotFound, CodeNodeNotFound},
		{ErrNodeInUse, http.StatusConflict, CodeNodeBusy},
		{ErrInvalidInput, http.StatusBadRequest, CodeInvalidInput},
	}
	for _, tc := range cases {
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.GET("/x", func(c *gin.Context) { failStore(c, tc.err, "测试") })
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
		if w.Code != tc.status {
			t.Fatalf("%v 应映射为 %d，实际 %d", tc.err, tc.status, w.Code)
		}
		if !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("%v 的响应应含业务码 %s，实际 %s", tc.err, tc.code, w.Body.String())
		}
	}

	// 未识别的错误 → 500（且不回显内部细节）
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", func(c *gin.Context) { failStore(c, errSentinel, "某操作") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("未知错误应映射为 500，实际 %d", w.Code)
	}
	if strings.Contains(w.Body.String(), errSentinel.Error()) {
		t.Fatalf("500 响应不应回显内部错误细节：%s", w.Body.String())
	}
}

var errSentinel = errors.New("内部细节不应外泄")
