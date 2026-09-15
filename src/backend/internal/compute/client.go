package compute

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 节点侧 HTTP 客户端：心跳 / 任务拉取 / 结果回传。
//
// ⚠️ **刻意不用 WebSocket**（TDD §6.1 的传输层首选是 wss）：
//   - 契约 §15 并未要求长连接，协议三件事本来就是"节点主动发起 + 短应答"；
//   - 本项目需要在没有 TLS 的内网 http 上也能跑（wss 强制 TLS 是 §6.1 的安全要求，
//     而当前控制端在 Nginx 后面按域名暴露，节点侧还要带 IP 白名单才完整）；
//   - HTTP 轮询更容易测（httptest 就能覆盖全链路）、更容易排障（curl 即可复现）、
//     不需要引入任何 WS 依赖。
//
// 服务端主动推送需求出现时（如"立即取消某任务"）再评估长连接，届时本客户端的
// 三个方法即为协议语义的参照实现。

// HTTPError 服务端返回的非 2xx 响应。
type HTTPError struct {
	Status  int    // HTTP 状态码
	Code    string // 响应包络里的 error.code（可能为空）
	Message string // 响应包络里的 error.message（可能为空）
}

// Error 可读错误串（包含 HTTP 状态与业务码，便于日志定位）。
func (e *HTTPError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("节点接口返回 HTTP %d", e.Status)
	}
	return fmt.Sprintf("节点接口返回 HTTP %d (%s): %s", e.Status, e.Code, e.Message)
}

// IsFatal 该错误是否应当让 agent 放弃重试。
//
// 401/403 说明凭据本身有问题（令牌错、被轮换、节点被删）——指数退避重试一万次
// 也不会有别的结果，只会把日志刷满；这类必须立刻退出并把错误抛给运维。
// 其余（网络不通、5xx、超时）都属"等一下可能就好"，交给退避重试。
func IsFatal(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden
	}
	return false
}

// Client 节点侧客户端。
type Client struct {
	baseURL string
	token   string
	hc      *http.Client
}

// NewClient 构造客户端。serverURL 形如 http://host:port（结尾斜杠会被去掉）。
func NewClient(serverURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(serverURL), "/"),
		token:   strings.TrimSpace(token),
		hc:      &http.Client{Timeout: 30 * time.Second},
	}
}

// Heartbeat 上报心跳。hb 中的能力字段应在首次心跳（或重连后）携带，
// 对应 TDD §6.1「重连后重新注册能力」。
func (c *Client) Heartbeat(ctx context.Context, hb HeartbeatRequest) (*HeartbeatResponse, error) {
	var out HeartbeatResponse
	if err := c.post(ctx, "/compute-nodes/agent/heartbeat", hb, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Poll 拉取任务。服务端保证 jobs 为空数组而非 null。
func (c *Client) Poll(ctx context.Context, max int) (*PollResponse, error) {
	var out PollResponse
	if err := c.post(ctx, "/compute-nodes/agent/poll", PollRequest{Max: max}, &out); err != nil {
		return nil, err
	}
	if out.Jobs == nil {
		// 兜底归一化：即使服务端返回 null，上层也只需处理"空切片"一种情况。
		out.Jobs = []JobSpec{}
	}
	return &out, nil
}

// Submit 回传任务结果。
func (c *Client) Submit(ctx context.Context, r ResultRequest) error {
	return c.post(ctx, "/compute-nodes/agent/result", r, nil)
}

// post 发起一次 JSON POST。out 为 nil 时只关心状态码。
func (c *Client) post(ctx context.Context, path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("编码请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("请求 %s 失败: %w", path, err)
	}
	defer resp.Body.Close()

	// 读取上限：错误响应里可能夹带大 body，不该让节点被拖住。
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("读取 %s 响应失败: %w", path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		he := &HTTPError{Status: resp.StatusCode}
		var env APIErrorEnvelope
		if json.Unmarshal(raw, &env) == nil {
			he.Code, he.Message = env.Error.Code, env.Error.Message
		}
		if he.Message == "" {
			he.Message = strings.TrimSpace(string(raw))
		}
		return he
	}

	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("解析 %s 响应失败: %w", path, err)
	}
	return nil
}
