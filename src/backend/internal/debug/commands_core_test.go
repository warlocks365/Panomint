package debug

// HTTP 命令面单测（Job000140 Phase 1）。只覆盖**不触 DB** 的分支：
// 参数校验、白名单守卫、错误码→状态映射、handler 应答形态矩阵。
// 触 DB 的命令路径由 115 测试服 e2e 矩阵覆盖（与 debugctl 回归共用命令核心即等价）。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestAgentHTTPCommandsWhitelist 白名单守卫：拿硬编码期望值当参照物（验证纪律），
// 防止有人静默增删命令面——白名单变更必须显式改本测试。
func TestAgentHTTPCommandsWhitelist(t *testing.T) {
	want := map[string]struct{}{
		"ping":        {},
		"snapshot":    {},
		"queue.stats": {},
		"job.pause":   {},
		"job.resume":  {},
		"job.cancel":  {},
		"job.log.tail": {},
	}
	if len(want) != 7 {
		t.Fatalf("期望值本身错误：应 7 条，实 %d", len(want))
	}
	for name := range want {
		if _, ok := agentHTTPCommands[name]; !ok {
			t.Errorf("白名单缺失命令 %q", name)
		}
	}
	for name := range agentHTTPCommands {
		if _, ok := want[name]; !ok {
			t.Errorf("白名单多出命令 %q（若为有意变更请同步修改本测试与设计文档 §6.2）", name)
		}
	}
	// WSS 专属命令不得出现在 HTTP 面。
	for _, name := range []string{"hello", "subscribe", "unsubscribe"} {
		if _, ok := agentHTTPCommands[name]; ok {
			t.Errorf("命令 %q 属连接/会话层，不应进入 HTTP 白名单", name)
		}
	}
}

func TestAgentCmdHTTPStatus(t *testing.T) {
	cases := map[string]int{
		"UNKNOWN_COMMAND": 400,
		"INVALID_PARAMS":  400,
		"JOB_NOT_FOUND":   404,
		"INVALID_STATE":   409,
		"INTERNAL":        500,
		"任意未登记码":          500,
	}
	for code, want := range cases {
		if got := agentCmdHTTPStatus(code); got != want {
			t.Errorf("agentCmdHTTPStatus(%q)=%d, want %d", code, got, want)
		}
	}
}

func TestCmdJobControlCoreInvalidParams(t *testing.T) {
	// nil Pool 不会触达：参数校验必须先于任何 DB 访问（恒假 FailClosed 语义在先）。
	cases := []string{
		`{"job_id":"not-a-uuid"}`,
		`{"job_id":""}`,
		`{}`,
		`not-json`,
	}
	for _, data := range cases {
		_, _, err := cmdJobControlCore(nil, nil, nil, msgJobPause, json.RawMessage(data))
		if err == nil {
			t.Errorf("payload=%q 应报 INVALID_PARAMS", data)
			continue
		}
		if got := errCode(err); got != "INVALID_PARAMS" {
			t.Errorf("payload=%q 错误码=%s, want INVALID_PARAMS", data, got)
		}
	}
}

func TestCmdJobLogTailCoreInvalidParams(t *testing.T) {
	_, _, err := cmdJobLogTailCore(nil, nil, json.RawMessage(`{"job_id":"xx"}`))
	if err == nil || errCode(err) != "INVALID_PARAMS" {
		t.Errorf("非法 job_id 应 INVALID_PARAMS，got %v", err)
	}
}

func TestCmdPingCore(t *testing.T) {
	payload, jobID, err := cmdPingCore()
	if err != nil || jobID != "" {
		t.Fatalf("ping 不应失败且无 job_id，got err=%v jobID=%q", err, jobID)
	}
	if payload["server_time"] == "" {
		t.Error("ping 载荷缺 server_time")
	}
}

// newAgentCmdRouter 最小装配（Pool/TransQ/RL/Audit 全 nil——只走不触依赖的分支）。
func newAgentCmdRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &AgentCmdHandler{}
	r.POST("/admin/agent/cmd", h.Cmd)
	return r
}

func postCmd(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/admin/agent/cmd", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentCmdHandlerShape(t *testing.T) {
	r := newAgentCmdRouter()

	// 未知命令 → 400 + UNKNOWN_COMMAND（且不触依赖即拒绝）。
	if w := postCmd(t, r, `{"type":"exec.shell"}`); w.Code != 400 {
		t.Errorf("未知命令应 400，got %d", w.Code)
	} else if !bytes.Contains(w.Body.Bytes(), []byte("UNKNOWN_COMMAND")) {
		t.Errorf("应含 UNKNOWN_COMMAND，body=%s", w.Body.String())
	}

	// 缺 type → 400 BAD_REQUEST。
	if w := postCmd(t, r, `{}`); w.Code != 400 || !bytes.Contains(w.Body.Bytes(), []byte("BAD_REQUEST")) {
		t.Errorf("缺 type 应 400 BAD_REQUEST，got %d %s", w.Code, w.Body.String())
	}

	// 非法 JSON → 400。
	if w := postCmd(t, r, `not-json`); w.Code != 400 {
		t.Errorf("非法 JSON 应 400，got %d", w.Code)
	}

	// ping（白名单内、无参、不触 DB）→ 200 + ok=true + result.server_time。
	w := postCmd(t, r, `{"type":"ping"}`)
	if w.Code != 200 {
		t.Fatalf("ping 应 200，got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK     bool           `json:"ok"`
		Cmd    string         `json:"cmd"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("应答非合法 JSON：%v", err)
	}
	if !resp.OK || resp.Cmd != "ping" || resp.Result["server_time"] == "" {
		t.Errorf("ping 应答形态不符：%s", w.Body.String())
	}
}
