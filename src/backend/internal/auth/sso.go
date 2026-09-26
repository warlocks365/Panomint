package auth

// SSO/OIDC 登录（Job000054，T5.8）。契约 v1.3 补录。
//
// 设计取舍（[自行决策]，均可在管理端配置落地后再调）：
//  1. **通用 OIDC**：不绑定 Keycloak/Authelia 任何一家——两者都是标准 OIDC Provider，
//     走 discovery（{issuer}/.well-known/openid-configuration）+ authorization_code 流程，
//     环境变量配置即接入任意标准 IdP。
//  2. **未配置 = 功能不存在（FailClosed）**：SSO_OIDC_* 任一缺失 → /auth/sso/oidc/login
//     与 /auth/sso/oidc 返回 404 NOT_CONFIGURED（同形文案），/auth/sso/config 返回 {enabled:false}。
//     前端据此隐藏按钮。不给「配了一半」的状态留半开窗口。
//  3. **JIT 自动开通**：首次 SSO 登录按 email 自动建号（角色=member，管理端可改）；
//     再次登录复用既有账号（email 是唯一键）。这是企业相册 SSO 的标准做法，
//     与「独立账户体系并存」的 PRD 定位一致：本地账号照常可用，SSO 是平行入口。
//  4. **SSO 用户没有本地口令，但 schema 不变**：users.password_hash 保持 NOT NULL，
//     JIT 用户写入**随机 32 字节十六进制的 bcrypt**（任何人不可能知道，包括我们）。
//     效果：密码登录对其自然失败（与「密码错」同形 401），自助改密因「当前密码」不可得而失败，
//     无需任何特殊分支；日后想给该账号加本地口令，管理端「重置密码」直接可用（混合账号）。
//  5. **失败同形**：state 错/code 无效/token 换不到/userinfo 失败/email_verified=false
//     一律 401 SSO_FAILED 同形文案 —— 不向外部分层报错（那是给攻击者的 IdP 探测面）；
//     细分原因只进服务端日志。claims 缺 email 单独 400 EMAIL_REQUIRED（那是管理员的
//     IdP scope 配置错误，需要可诊断）。
//  6. **state**：HMAC 签名的随机值（JWT HS256，typ=sso-state，10 分钟有效），一次性
//     （进程内 used 集合，防重放；多副本部署需换共享存储，见 Limitations）。
//  7. **OIDC 客户端交互全部走可注入的 *http.Client**：单测用 httptest 假 IdP 全链路覆盖，
//     不触真网。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
)

// ---------------------------------------------------------------- 配置

// SSOConfig OIDC 接入配置（全部来自环境变量；零值 = 未启用）。
type SSOConfig struct {
	Enabled      bool
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string // 前端回调地址（IdP 授权后带 code/state 跳回这里）
	Scopes       string // 默认 "openid email profile"
}

// SSOConfigFromEnv 从环境变量组装配置；四个必填项缺一即整体不启用（FailClosed）。
func SSOConfigFromEnv() SSOConfig {
	c := SSOConfig{
		Issuer:       strings.TrimSpace(os.Getenv("SSO_OIDC_ISSUER")),
		ClientID:     strings.TrimSpace(os.Getenv("SSO_OIDC_CLIENT_ID")),
		ClientSecret: os.Getenv("SSO_OIDC_CLIENT_SECRET"),
		RedirectURI:  strings.TrimSpace(os.Getenv("SSO_OIDC_REDIRECT_URI")),
		Scopes:       strings.TrimSpace(os.Getenv("SSO_OIDC_SCOPES")),
	}
	if c.Scopes == "" {
		c.Scopes = "openid email profile"
	}
	c.Enabled = c.Issuer != "" && c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
	return c
}

// ---------------------------------------------------------------- OIDC 客户端（可测层）

// oidcEndpoints discovery 文档里我们关心的两个端点。
type oidcEndpoints struct {
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	Userinfo      string `json:"userinfo_endpoint"`
}

// discover 拉取 {issuer}/.well-known/openid-configuration。
func discover(client *http.Client, issuer string) (*oidcEndpoints, error) {
	wellKnown := strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration"
	resp, err := client.Get(wellKnown)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery HTTP %d", resp.StatusCode)
	}
	var ep oidcEndpoints
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ep); err != nil {
		return nil, err
	}
	if ep.Authorization == "" || ep.Token == "" || ep.Userinfo == "" {
		return nil, errors.New("discovery 缺必需端点")
	}
	return &ep, nil
}

// authorizeURL 拼 IdP 授权跳转地址。
func authorizeURL(ep *oidcEndpoints, c SSOConfig, state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", c.Scopes)
	q.Set("state", state)
	return ep.Authorization + "?" + q.Encode()
}

// exchangeCode authorization_code 换 token 对（HTTP Basic 客户端认证，RFC 6749 惯例；
// Keycloak/Authelia 均支持）。
func exchangeCode(client *http.Client, tokenURL, code string, c SSOConfig) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	req, err := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(c.ClientID), url.QueryEscape(c.ClientSecret))
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("token endpoint HTTP %d", resp.StatusCode)
	}
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", err
	}
	if body.AccessToken == "" {
		return "", errors.New("token 响应缺 access_token")
	}
	return body.AccessToken, nil
}

// ssoClaims userinfo 中我们关心的字段（email_verified 指针：缺失=不校验，显式 false=拒绝）。
type ssoClaims struct {
	Email         string `json:"email"`
	EmailVerified *bool  `json:"email_verified"`
	Name          string `json:"name"`
	Preferred     string `json:"preferred_username"`
}

func fetchClaims(client *http.Client, userinfoURL, accessToken string) (*ssoClaims, error) {
	req, err := http.NewRequest(http.MethodGet, userinfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("userinfo HTTP %d", resp.StatusCode)
	}
	var cl ssoClaims
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cl); err != nil {
		return nil, err
	}
	return &cl, nil
}

// displayName 从 claims 推断展示名：name → preferred_username → email 的 @ 前缀。
func (cl *ssoClaims) displayName() string {
	if v := strings.TrimSpace(cl.Name); v != "" {
		return v
	}
	if v := strings.TrimSpace(cl.Preferred); v != "" {
		return v
	}
	if i := strings.IndexByte(cl.Email, '@'); i > 0 {
		return cl.Email[:i]
	}
	return cl.Email
}

// ---------------------------------------------------------------- state（签名 + 一次性）

const ssoStateTTL = 10 * time.Minute

// signState 生成一次性 state：随机 16 字节 + HMAC 签名 + 过期，拼成 "<hex>.<sig>"。
func signState(secret []byte) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	expiresAt := time.Now().Add(ssoStateTTL).Unix()
	payload := fmt.Sprintf("%d.%s", expiresAt, hex.EncodeToString(nonce[:]))
	sig := hmacHex(secret, payload)
	return payload + "." + sig, nil
}

// verifyState 校验签名与过期，并检查一次性（重放即拒）。
func (h *SSOHandler) verifyState(state string) bool {
	parts := strings.Split(state, ".")
	if len(parts) != 3 {
		return false
	}
	payload := parts[0] + "." + parts[1]
	if !hmacEqual(hmacHex(h.Secret, payload), parts[2]) {
		return false
	}
	var exp int64
	if _, err := fmt.Sscanf(parts[0], "%d", &exp); err != nil {
		return false
	}
	if time.Now().Unix() > exp {
		return false
	}
	// 一次性：进程内 used 集合（key = nonce 部分）。
	h.usedMu.Lock()
	defer h.usedMu.Unlock()
	if h.used == nil {
		h.used = map[string]time.Time{}
	}
	if _, dup := h.used[parts[1]]; dup {
		return false
	}
	h.used[parts[1]] = time.Now().Add(ssoStateTTL)
	// 顺手清扫过期项（集合小时无所谓性能；map 涨到上千才清）。
	if len(h.used) > 1024 {
		now := time.Now()
		for k, v := range h.used {
			if now.After(v) {
				delete(h.used, k)
			}
		}
	}
	return true
}

// hmacHex HMAC-SHA256 的十六进制（state 签名用；不与 jwt.go 的令牌签发混用）。
func hmacHex(secret []byte, payload string) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

func hmacEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// ---------------------------------------------------------------- Handler

// SSOHandler SSO 端点集。Secret 复用 JWT 密钥（签 state）；Client 可注入（单测走假 IdP）。
type SSOHandler struct {
	Cfg    SSOConfig
	Store  *Store
	Secret []byte
	Audit  *audit.Recorder
	Client *http.Client

	usedMu sync.Mutex
	used   map[string]time.Time
}

func (h *SSOHandler) client() *http.Client {
	if h.Client != nil {
		return h.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// ConfigInfo GET /auth/sso/config → {"enabled": bool}（公开，供登录页决定是否渲染 SSO 按钮）。
func (h *SSOHandler) ConfigInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"enabled": h.Cfg.Enabled})
}

// LoginRedirect GET /auth/sso/oidc/login → 302 到 IdP 授权地址（带一次性 state）。
// 未配置 404 同形 —— 与 Callback 的 FailClosed 口径一致。
func (h *SSOHandler) LoginRedirect(c *gin.Context) {
	if !h.Cfg.Enabled {
		errResp(c, http.StatusNotFound, "NOT_CONFIGURED", "SSO 未配置")
		return
	}
	ep, err := discover(h.client(), h.Cfg.Issuer)
	if err != nil {
		log.Printf("auth: SSO discovery 失败（issuer=%s）: %v", h.Cfg.Issuer, err)
		errResp(c, http.StatusServiceUnavailable, "SSO_UNAVAILABLE", "SSO 身份源暂不可用")
		return
	}
	state, err := signState(h.Secret)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "state 生成失败")
		return
	}
	c.Redirect(http.StatusFound, authorizeURL(ep, h.Cfg, state))
}

// Callback POST /auth/sso/oidc {code, state} → {access_token, refresh_token}（与密码登录同形）。
//
// 顺序：FailClosed → state（一次性）→ 换 token → userinfo → email 必需/已验证 →
// 账号解析（复用或 JIT 开通）→ 状态守卫 → 签发票据 → 审计。
// SSO 失败的细分原因只进日志；对外一律 401 SSO_FAILED 同形（防 IdP 探测面）。
func (h *SSOHandler) Callback(c *gin.Context) {
	if !h.Cfg.Enabled {
		errResp(c, http.StatusNotFound, "NOT_CONFIGURED", "SSO 未配置")
		return
	}
	var req struct {
		Code  string `json:"code" binding:"required"`
		State string `json:"state" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误（需要 code 与 state）")
		return
	}
	if !h.verifyState(req.State) {
		errResp(c, http.StatusUnauthorized, "SSO_FAILED", "SSO 登录失败")
		return
	}
	ep, err := discover(h.client(), h.Cfg.Issuer)
	if err != nil {
		log.Printf("auth: SSO discovery 失败（回调阶段）: %v", err)
		errResp(c, http.StatusUnauthorized, "SSO_FAILED", "SSO 登录失败")
		return
	}
	access, err := exchangeCode(h.client(), ep.Token, req.Code, h.Cfg)
	if err != nil {
		log.Printf("auth: SSO code 换 token 失败: %v", err)
		errResp(c, http.StatusUnauthorized, "SSO_FAILED", "SSO 登录失败")
		return
	}
	claims, err := fetchClaims(h.client(), ep.Userinfo, access)
	if err != nil {
		log.Printf("auth: SSO userinfo 失败: %v", err)
		errResp(c, http.StatusUnauthorized, "SSO_FAILED", "SSO 登录失败")
		return
	}
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		errResp(c, http.StatusBadRequest, "EMAIL_REQUIRED", "身份源未返回 email（检查 SSO scope 是否含 email）")
		return
	}
	if claims.EmailVerified != nil && !*claims.EmailVerified {
		log.Printf("auth: SSO 用户 %s 的 email_verified=false，拒绝", email)
		errResp(c, http.StatusUnauthorized, "SSO_FAILED", "SSO 登录失败")
		return
	}

	ctx := c.Request.Context()
	u, err := h.Store.FindByEmailCI(ctx, email)
	jitCreated := false
	if errors.Is(err, ErrBadCredentials) {
		// JIT 开通：member 角色 + 随机不可知口令（见文件头设计取舍 4）。
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			errResp(c, http.StatusInternalServerError, "INTERNAL", "随机源失败")
			return
		}
		id, err := h.Store.CreateUser(ctx, email, claims.displayName(), hex.EncodeToString(raw), "member")
		if err != nil {
			log.Printf("auth: SSO JIT 建号失败（%s）: %v", email, err)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "账号创建失败")
			return
		}
		u, err = h.Store.FindByID(ctx, id)
		if err != nil {
			errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
			return
		}
		jitCreated = true
	} else if err != nil {
		log.Printf("auth: SSO 查用户失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	if u.Status != "active" {
		errResp(c, http.StatusForbidden, "USER_DISABLED", ErrUserDisabled.Error())
		return
	}

	// 签发票据：与密码登录完全同一套（IssueAccess + NewRefreshToken + CreateSession）。
	accessTok, err := IssueAccess(h.Secret, u.ID, u.Role)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	refreshTok, err := NewRefreshToken()
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	if err := h.Store.CreateSession(ctx, u.ID, refreshTok, c.ClientIP(), c.GetHeader("User-Agent")); err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "会话创建失败")
		return
	}
	h.recordSSO(c, u.ID, jitCreated)
	// SSO 登录不带强制改密：口令由 IdP 管理，"强制改密"语义只适用于本地口令
	// （must_change_password 由管理员重置本地口令时置位，SSO 路径恒 false）。
	c.JSON(http.StatusOK, tokenPair{accessTok, refreshTok, int64(AccessTTL.Seconds()), "Bearer", false})
}

// recordSSO 写审计（尽力而为）。公开端点 → recordAs 显式传 actor。
func (h *SSOHandler) recordSSO(c *gin.Context, userID string, jitCreated bool) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = audit.ActionSSOLogin
	e.TargetType = audit.TargetUser
	e.TargetID = userID
	e.ActorUserID = userID
	e.Detail = map[string]any{"jit_created": jitCreated}
	h.Audit.Record(c.Request.Context(), e)
}
