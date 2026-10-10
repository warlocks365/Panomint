// Package agentllm Agent 语义接口的 LLM 上游配置与代理（Job000140 Phase 2，设计 §4/§5/§9）。
//
// 三件事：
//   1. agent_llm_config 单行配置的存取（api_key 列级加密，storage.EncryptString 同体系）；
//   2. 管理端点三件（GET/PUT /admin/agent/llm-config、POST .../test，admin:system 门禁
//      由路由层把关）——key 永不回显（只给 has_key + 尾 4 位）；
//   3. OpenAI 兼容代理 POST /agent/llm/v1/chat/completions（authed + admin:system）：
//      浏览器侧 page-agent 把 baseURL 指向本端点，真实上游 key 只存在服务端；
//      page-agent v1.12.4 的 OpenAIClient 为**非流式**实现（整响应 JSON），代理为
//      一次性 POST 转发（读 body → 提取 usage 记审计 → 原样透传状态与响应）。
//
// 安全要点：
//   - 代理请求/响应体不落审计（只记 model/status/耗时/usage 数字）；
//   - 上游未配置/未启用 → 404 AGENT_LLM_NOT_CONFIGURED（与不存在同形，不泄露状态）；
//   - 请求体上限 4MB（AgentOutput 工具调用 payload 小，超限即 413）。
package agentllm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
	"panoalbum/internal/storage"
)

// 单行配置的列（与迁移 00044 对齐）。
type row struct {
	BaseURL   string
	Model     string
	APIKeyEnc string
	Enabled   bool
}

// Handler LLM 配置管理 + 代理。
type Handler struct {
	Pool  *pgxpool.Pool
	Audit *audit.Recorder // 可为 nil（测试跳过）
	HTTP  *http.Client    // 上游探活/转发客户端（可注入测试；nil 用默认）
}

// client 上游 HTTP 客户端。
//
// ⚠️ 默认走 guardedClient（netguard.go）：出站拨号前判定目标 IP，
// 阻断本机/内网/链路本地（含云元数据 169.254.169.254）。
// 注入的 h.HTTP（测试用）**不受**该防线约束 —— 测试要能连 httptest 的
// 127.0.0.1，守卫会拦掉；生产路径永远是 h.HTTP == nil 走 guardedClient。
func (h *Handler) client() *http.Client {
	if h.HTTP != nil {
		return h.HTTP
	}
	return guardedClient
}

// ---------------------------------------------------------------------------
// 配置存取
// ---------------------------------------------------------------------------

func (h *Handler) loadRow(ctx context.Context) (*row, error) {
	var r row
	err := h.Pool.QueryRow(ctx,
		`SELECT base_url, model, api_key_enc, enabled FROM agent_llm_config WHERE id = true`).
		Scan(&r.BaseURL, &r.Model, &r.APIKeyEnc, &r.Enabled)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil // 未配置（空行不存在）
		}
		return nil, err
	}
	return &r, nil
}

// recordAudit agent 域审计（actor=管理员本人）。
func (h *Handler) recordAudit(c *gin.Context, action string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = action
	e.TargetType = audit.TargetAgentCmd
	e.TargetID = "llm_config"
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}

// GetConfig GET /admin/agent/llm-config（无 key 明文；只给 has_key + 尾 4 位）。
func (h *Handler) GetConfig(c *gin.Context) {
	r, err := h.loadRow(c.Request.Context())
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	resp := gin.H{"enabled": false, "base_url": "", "model": "", "has_key": false, "key_tail": ""}
	if r != nil {
		resp["enabled"] = r.Enabled
		resp["base_url"] = r.BaseURL
		resp["model"] = r.Model
		if key, derr := storage.DecryptString(r.APIKeyEnc); derr == nil && key != "" {
			resp["has_key"] = true
			if len(key) > 4 {
				resp["key_tail"] = key[len(key)-4:]
			} else {
				resp["key_tail"] = "****"
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

// putBody PUT 请求体。api_key 三态：非空串=更新；空串=清除；null=保留旧值
// （key 不可回显的现实妥协，契约中明确标注——区别于 ui-prefs 的整行替换铁律）。
type putBody struct {
	BaseURL string  `json:"base_url"`
	Model   string  `json:"model"`
	APIKey  *string `json:"api_key"`
	Enabled bool    `json:"enabled"`
}

// PutConfig PUT /admin/agent/llm-config。
func (h *Handler) PutConfig(c *gin.Context) {
	var b putBody
	if err := c.ShouldBindJSON(&b); err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "请求体需为 JSON 对象")
		return
	}
	b.BaseURL = strings.TrimRight(strings.TrimSpace(b.BaseURL), "/")
	b.Model = strings.TrimSpace(b.Model)
	if b.Enabled && (b.BaseURL == "" || b.Model == "") {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "启用时 base_url 与 model 必填")
		return
	}
	// SSRF 防线（netguard.go H-1）：非空 base_url 必须是 http/https；
	// 指向私网/环回时，需主机已加入环境变量 AGENT_LLM_ALLOWED_HOSTS 白名单
	//（本地大模型部署需求，2026-10-11）。放在**保存前**而不是只在出站前 ——
	// 让配置错误在写入时就明确拒绝（400 + 可操作文案），而不是等到
	// 探活/代理时才发现连不上。
	if b.BaseURL != "" {
		if _, err := validateUpstreamURL(b.BaseURL); err != nil {
			httperr.Envelope(c, http.StatusBadRequest, "BAD_URL",
				"base_url 非法："+err.Error())
			return
		}
	}

	// 旧 key 保留语义：api_key=null → 沿用；"" → 清除；非空 → 加密更新。
	newKeyEnc := ""
	if b.APIKey == nil {
		if old, err := h.loadRow(c.Request.Context()); err == nil && old != nil {
			newKeyEnc = old.APIKeyEnc
		}
	} else if *b.APIKey != "" {
		if !storage.CipherKeyConfigured() {
			httperr.Envelope(c, http.StatusBadRequest, "CIPHER_KEY_MISSING",
				"服务端未配置 STORAGE_CIPHER_KEY，无法保存 API Key（FailClosed）")
			return
		}
		enc, err := storage.EncryptString(*b.APIKey)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "ENCRYPT_FAILED", "加密失败", err)
			return
		}
		newKeyEnc = enc
	}

	_, err := h.Pool.Exec(c.Request.Context(),
		`INSERT INTO agent_llm_config (id, base_url, model, api_key_enc, enabled, updated_by, updated_at)
		 VALUES (true, $1, $2, $3, $4, $5, now())
		 ON CONFLICT (id) DO UPDATE SET base_url=$1, model=$2, api_key_enc=$3, enabled=$4, updated_by=$5, updated_at=now()`,
		b.BaseURL, b.Model, newKeyEnc, b.Enabled, c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "SAVE_FAILED", "保存失败", err)
		return
	}
	h.recordAudit(c, audit.ActionAgentLLMConfig, map[string]any{
		"enabled": b.Enabled, "has_key": newKeyEnc != "", "op": "save",
	})
	c.Status(http.StatusNoContent)
}

// TestConfig POST /admin/agent/llm-config/test：服务端向上游发一次 GET {base}/models 探活。
// 请求体可带 base_url/api_key（保存前测试）；缺省用已存配置。
func (h *Handler) TestConfig(c *gin.Context) {
	var b struct {
		BaseURL string  `json:"base_url"`
		APIKey  *string `json:"api_key"`
	}
	_ = c.ShouldBindJSON(&b) // 允许空体：全用已存配置
	baseURL := strings.TrimRight(strings.TrimSpace(b.BaseURL), "/")
	apiKey := ""
	if b.APIKey != nil {
		apiKey = *b.APIKey
	} else if r, err := h.loadRow(c.Request.Context()); err == nil && r != nil {
		if baseURL == "" {
			baseURL = r.BaseURL
		}
		if k, derr := storage.DecryptString(r.APIKeyEnc); derr == nil {
			apiKey = k
		}
	}
	if baseURL == "" {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "base_url 为空")
		return
	}
	// SSRF 防线（netguard.go H-1）：出站前再校验一次。
	// 必须在**每次**出站前校验而不只保存时：库里的 base_url 可能是在本次
	// 修复之前写入的（存量数据），且 DNS 解析结果随时会变（重绑定）。
	if _, err := validateUpstreamURL(baseURL); err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_URL",
			"base_url 非法："+err.Error())
		return
	}

	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_URL", "base_url 非法")
		return
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := h.client().Do(req)
	if err != nil {
		h.recordAudit(c, audit.ActionAgentLLMConfig, map[string]any{"op": "test", "ok": false})
		// 完整错误只进服务端日志（httperr 包文档的既定分工）：err 内含目标 URL
		// 与底层网络细节，直接回显等于给 SSRF 探测提供「内网端口是否开放」的
		// 观测通道（连拒绝 vs 超时 vs DNS 失败能区分出版本拓扑）。
		log.Printf("[agentllm] 上游探活失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "上游不可达，请检查 base_url 与网络"})
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	h.recordAudit(c, audit.ActionAgentLLMConfig, map[string]any{
		"op": "test", "ok": resp.StatusCode == http.StatusOK, "status": resp.StatusCode,
	})
	if resp.StatusCode != http.StatusOK {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "上游返回 HTTP " + http.StatusText(resp.StatusCode)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---------------------------------------------------------------------------
// OpenAI 兼容代理
// ---------------------------------------------------------------------------

// ProxyChat POST /agent/llm/v1/chat/completions。
func (h *Handler) ProxyChat(c *gin.Context) {
	ctx := c.Request.Context()
	r, err := h.loadRow(ctx)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if r == nil || !r.Enabled || r.BaseURL == "" || r.Model == "" {
		// 与不存在同形：不泄露「配置了但停用」的内部状态。
		httperr.Envelope(c, http.StatusNotFound, "AGENT_LLM_NOT_CONFIGURED", "LLM 上游未配置")
		return
	}
	apiKey, derr := storage.DecryptString(r.APIKeyEnc)
	if derr != nil && !errors.Is(derr, storage.ErrNoCipherKey) {
		httperr.Fail(c, http.StatusInternalServerError, "DECRYPT_FAILED", "解密失败", derr)
		return
	}

	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 4<<20))
	if err != nil || len(body) == 0 {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_REQUEST", "请求体非法")
		return
	}

	upstream := r.BaseURL + "/chat/completions"
	// SSRF 防线（netguard.go H-1）：每次转发前校验，覆盖「修复前写入的存量
	// base_url」与「DNS 重绑定」两种情况 —— 库里的值不是可信输入的免检凭证。
	if _, err := validateUpstreamURL(r.BaseURL); err != nil {
		log.Printf("[agentllm] 上游地址被 SSRF 防线拒绝: %v", err)
		// 对外同形：与「未配置」一致的 404 视角，不告诉调用者「地址存在但被拦」
		// —— 调用方就是 admin 自己的浏览器，但保持响应面不泄露库内配置细节。
		httperr.Envelope(c, http.StatusNotFound, "AGENT_LLM_NOT_CONFIGURED", "LLM 上游未配置")
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstream, bytes.NewReader(body))
	if err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "BAD_URL", "上游地址非法")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	started := time.Now()
	resp, err := h.client().Do(req)
	if err != nil {
		h.recordLLM(c, r.Model, 0, 0, 0, false, time.Since(started))
		httperr.Fail(c, http.StatusBadGateway, "UPSTREAM_FAILED", "上游不可达", err)
		return
	}
	defer resp.Body.Close()

	respBody, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if rerr != nil {
		h.recordLLM(c, r.Model, resp.StatusCode, 0, 0, false, time.Since(started))
		httperr.Fail(c, http.StatusBadGateway, "UPSTREAM_FAILED", "读取上游响应失败", rerr)
		return
	}

	// usage 提取（尽力而为；失败不影响透传）。
	var usage struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(respBody, &usage)
	h.recordLLM(c, r.Model, resp.StatusCode,
		usage.Usage.PromptTokens, usage.Usage.CompletionTokens,
		resp.StatusCode == http.StatusOK, time.Since(started))

	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}

// recordLLM agent.llm 审计：只记数字与状态，**不含请求/响应内容**（隐私红线）。
func (h *Handler) recordLLM(c *gin.Context, model string, status, prompt, completion int, ok bool, dur time.Duration) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = audit.ActionAgentLLM
	e.TargetType = audit.TargetAgentCmd
	e.TargetID = "chat"
	e.Detail = map[string]any{
		"model": model, "status": status, "ok": ok,
		"duration_ms": dur.Milliseconds(),
		"prompt_tokens": prompt, "completion_tokens": completion,
	}
	h.Audit.Record(c.Request.Context(), e)
}
