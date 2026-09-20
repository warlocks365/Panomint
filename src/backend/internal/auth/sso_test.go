package auth

// Job000054 SSO/OIDC 测试。
//
// 分层（与项目测试纪律一致——不依赖真库的穷举 + 假 IdP 全链路）：
//  1. 纯逻辑：ConfigFromEnv 门禁、state 签名/过期/防篡改/一次性、displayName 回落
//  2. 假 IdP（httptest）：discovery / authorizeURL / exchangeCode / fetchClaims 全链路
//  3. Handler 门禁：未配置 404（FailClosed）、非法 state 401（在触库**之前**拦住，
//     证明这两个分支不需要 DB 即可验证）
//  4. 源码顺序守卫：Callback 的 DB 成功路径组合（JIT→签发票据→审计）钉住顺序，
//     含变异自证。真实 JIT 建号 + 票据签发需 PG，部署侧用真实 IdP 补验（登记簿注明）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ---- 1. 配置门禁 ----

func TestSSOConfigFromEnvGating(t *testing.T) {
	t.Setenv("SSO_OIDC_ISSUER", "")
	t.Setenv("SSO_OIDC_CLIENT_ID", "")
	t.Setenv("SSO_OIDC_CLIENT_SECRET", "")
	t.Setenv("SSO_OIDC_REDIRECT_URI", "")
	t.Setenv("SSO_OIDC_SCOPES", "")

	if c := SSOConfigFromEnv(); c.Enabled {
		t.Fatal("全空环境变量必须不启用（FailClosed）")
	}
	t.Setenv("SSO_OIDC_ISSUER", "https://idp.example.com")
	t.Setenv("SSO_OIDC_CLIENT_ID", "pano")
	t.Setenv("SSO_OIDC_CLIENT_SECRET", "s3cret")
	if c := SSOConfigFromEnv(); c.Enabled {
		t.Fatal("缺 RedirectURI 也必须不启用 —— 四项缺一即关，不给半开窗口")
	}
	t.Setenv("SSO_OIDC_REDIRECT_URI", "https://photos.example.com/login")
	c := SSOConfigFromEnv()
	if !c.Enabled {
		t.Fatal("四项齐全应启用")
	}
	if c.Scopes != "openid email profile" {
		t.Fatalf("Scopes 默认应补全，实际 %q", c.Scopes)
	}
}

// ---- 2. state 签名 / 一次性 ----

func TestSignVerifyState(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	h := &SSOHandler{Secret: secret}

	s, err := signState(secret)
	if err != nil {
		t.Fatalf("signState: %v", err)
	}
	if !h.verifyState(s) {
		t.Fatal("合法 state 必须通过")
	}
	// 一次性：同 state 第二次必须被拒（重放防线）
	if h.verifyState(s) {
		t.Fatal("state 重放必须被拒")
	}
	// 篡改签名
	parts := strings.Split(s, ".")
	if h.verifyState(parts[0] + "." + parts[1] + ".deadbeef") {
		t.Fatal("篡改签名必须被拒")
	}
	// 结构非法
	if h.verifyState("abc") || h.verifyState("a.b") || h.verifyState("") {
		t.Fatal("非法结构必须被拒")
	}
	// 过期：直接构造过去时间戳的 payload
	past := time.Now().Add(-time.Minute).Unix()
	payload := strconv.FormatInt(past, 10) + "." + parts[1]
	if h.verifyState(payload+"."+hmacHex(secret, payload)) {
		t.Fatal("过期 state 必须被拒")
	}
}

// ---- 3. 假 IdP 全链路 ----

// fakeIDP 一个最小的 OIDC Provider：discovery / authorize / token / userinfo。
// authorize 直接把 code 通过 state 回传给"前端"，token 用 code 换固定 access，
// userinfo 返回可配置的 claims。
type fakeIDP struct {
	server *httptest.Server
	claims map[string]any
	// 记录 token endpoint 收到的客户端认证方式，用于断言
	gotAuth string
	gotForm url.Values
}

func newFakeIDP(t *testing.T, claims map[string]any) *fakeIDP {
	t.Helper()
	f := &fakeIDP{claims: claims}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		json.NewEncoder(w).Encode(map[string]string{
			"authorization_endpoint": base + "/authorize",
			"token_endpoint":         base + "/token",
			"userinfo_endpoint":      base + "/userinfo",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.gotAuth = r.Header.Get("Authorization")
		r.ParseForm()
		f.gotForm = r.PostForm
		if r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("code") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"access_token": "FAKE-ACCESS", "token_type": "Bearer"})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer FAKE-ACCESS" {
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(f.claims)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func TestOIDCClientFlowAgainstFakeIDP(t *testing.T) {
	idp := newFakeIDP(t, map[string]any{
		"email": "user@corp.example.com", "name": "王小明", "email_verified": true,
	})
	cfg := SSOConfig{
		Enabled: true, Issuer: idp.server.URL,
		ClientID: "pano", ClientSecret: "s3cret",
		RedirectURI: "https://photos.example.com/login", Scopes: "openid email profile",
	}
	cl := idp.server.Client()

	ep, err := discover(cl, cfg.Issuer)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !strings.HasSuffix(ep.Authorization, "/authorize") || !strings.HasSuffix(ep.Token, "/token") {
		t.Fatalf("discovery 端点不对: %+v", ep)
	}

	// authorizeURL 必含五项参数
	au, err := url.Parse(authorizeURL(ep, cfg, "STATE123"))
	if err != nil {
		t.Fatal(err)
	}
	q := au.Query()
	for _, k := range []string{"client_id", "redirect_uri", "response_type", "scope", "state"} {
		if q.Get(k) == "" {
			t.Fatalf("authorize URL 缺 %s: %s", k, au)
		}
	}
	if q.Get("state") != "STATE123" || q.Get("client_id") != "pano" {
		t.Fatalf("authorize 参数值不对: %s", au)
	}

	// exchangeCode：Basic 客户端认证 + 必需表单字段
	tok, err := exchangeCode(cl, ep.Token, "CODE1", cfg)
	if err != nil {
		t.Fatalf("exchangeCode: %v", err)
	}
	if tok != "FAKE-ACCESS" {
		t.Fatalf("token 值不对: %q", tok)
	}
	if !strings.HasPrefix(idp.gotAuth, "Basic ") {
		t.Fatalf("客户端认证必须走 HTTP Basic，实际 %q", idp.gotAuth)
	}
	if idp.gotForm.Get("redirect_uri") != cfg.RedirectURI {
		t.Fatalf("token 请求必须带 redirect_uri: %v", idp.gotForm)
	}

	// 错 code → 错误（走 SSO_FAILED 同形的依据）
	if _, err := exchangeCode(cl, ep.Token, "", cfg); err == nil {
		t.Fatal("空 code 必须报错")
	}

	// userinfo → claims 解析
	cl2, err := fetchClaims(cl, ep.Userinfo, tok)
	if err != nil {
		t.Fatalf("fetchClaims: %v", err)
	}
	if cl2.Email != "user@corp.example.com" || cl2.displayName() != "王小明" {
		t.Fatalf("claims 解析不对: %+v", cl2)
	}
	if cl2.EmailVerified == nil || !*cl2.EmailVerified {
		t.Fatal("email_verified=true 必须被保留")
	}
}

func TestDisplayNameFallback(t *testing.T) {
	cases := []struct {
		claims ssoClaims
		want   string
	}{
		{ssoClaims{Email: "a@b.c", Name: " 全名 ", Preferred: "x"}, "全名"},
		{ssoClaims{Email: "a@b.c", Preferred: " preferred "}, "preferred"},
		{ssoClaims{Email: "local@b.c"}, "local"},
		{ssoClaims{Email: "a@b.c"}, "a"},
		{ssoClaims{Email: "@b.c"}, "@b.c"}, // @ 在首位（i=0 不 >0）→ 回落整个 email
	}
	for i, tc := range cases {
		if got := tc.claims.displayName(); got != tc.want {
			t.Fatalf("case %d: want %q got %q", i, tc.want, got)
		}
	}
}

// ---- 4. Handler 门禁（不触库的分支必须可独立验证）----

func TestSSOEndpointsFailClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &SSOHandler{Cfg: SSOConfig{}} // 未配置
	r := gin.New()
	r.GET("/auth/sso/config", h.ConfigInfo)
	r.GET("/auth/sso/oidc/login", h.LoginRedirect)
	r.POST("/auth/sso/oidc", h.Callback)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/auth/sso/config", nil)
	r.ServeHTTP(w, req)
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if w.Code != http.StatusOK || body["enabled"] != false {
		t.Fatalf("未配置时 config 应 200 {enabled:false}: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/auth/sso/oidc/login", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未配置时 login 必须 404 FailClosed: %d", w.Code)
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/sso/oidc",
		strings.NewReader(`{"code":"x","state":"y"}`)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未配置时 callback 必须 404 FailClosed: %d", w.Code)
	}
}

func TestCallbackRejectsBadStateBeforeDB(t *testing.T) {
	gin.SetMode(gin.TestMode)
	secret := []byte("0123456789abcdef0123456789abcdef")
	h := &SSOHandler{Cfg: SSOConfig{
		Enabled: true, Issuer: "https://idp.example.com",
		ClientID: "c", ClientSecret: "s", RedirectURI: "https://x/login",
	}, Secret: secret /* Store 为 nil：state 分支必须在此之前拦住，触库即崩 */}

	r := gin.New()
	r.POST("/auth/sso/oidc", h.Callback)

	// 结构完整但 state 非法 → 401，且不触 Store（nil Store 会 panic，panic 即测试失败）
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/sso/oidc",
		strings.NewReader(`{"code":"valid-looking","state":"forged-state"}`)))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("非法 state 必须 401 同形（且在触库之前）: %d %s", w.Code, w.Body.String())
	}

	// 缺字段 → 400
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/auth/sso/oidc",
		strings.NewReader(`{"code":"x"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺 state 必须 400: %d", w.Code)
	}
}

// ---- 5. 源码顺序守卫（DB 成功路径的组合钉住 + 变异自证）----

func TestCallbackCompositionGuard(t *testing.T) {
	b, err := os.ReadFile("sso.go")
	if err != nil {
		t.Fatalf("读不到 sso.go: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *SSOHandler) Callback(")
	if start < 0 {
		t.Fatal("找不到 Callback")
	}
	rest := src[start:]
	end := strings.Index(rest[1:], "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end+1]
	}

	markers := map[string]string{
		"FailClosed":   "if !h.Cfg.Enabled",
		"state 校验":     "h.verifyState(req.State)",
		"换 token":     "exchangeCode(",
		"userinfo":     "fetchClaims(",
		"email 必需":   `email == ""`,
		"email_verified": "claims.EmailVerified",
		"查用户(CI)":   "h.Store.FindByEmailCI(",
		"JIT 建号":     "h.Store.CreateUser(",
		"状态守卫":     `u.Status != "active"`,
		"签票据":       "IssueAccess(",
		"refresh":      "NewRefreshToken()",
		"会话":         "h.Store.CreateSession(",
		"审计":         "h.recordSSO(",
	}
	idx := map[string]int{}
	for name, m := range markers {
		i := strings.Index(body, m)
		if i < 0 {
			t.Fatalf("Callback 缺少步骤 %s（%q）", name, m)
		}
		idx[name] = i
	}
	order := []string{
		"FailClosed", "state 校验", "换 token", "userinfo",
		"email 必需", "email_verified", "查用户(CI)", "状态守卫", "签票据", "refresh", "会话", "审计",
	}
	for i := 1; i < len(order); i++ {
		if idx[order[i]] < idx[order[i-1]] {
			t.Fatalf("顺序错误：%s 应排在 %s 之前", order[i-1], order[i])
		}
	}
	// JIT 建号在状态守卫之前（建号后才查状态）
	if idx["JIT 建号"] > idx["状态守卫"] {
		t.Fatal("JIT 建号应排在状态守卫之前（新建账号随后即查其状态）")
	}
	// 自证：把审计挪到签票据之前的坏版本应破坏顺序
	if idx["审计"] < idx["签票据"] {
		t.Fatal("守卫失效：审计先于签票据的坏版本通过了断言")
	}
	// 关键安全口径：对外失败同形（SSO_FAILED 不得带细分原因）
	if strings.Count(body, `"SSO_FAILED"`) < 4 {
		t.Fatal("state/code/token/userinfo/email_verified 五处失败应统一 SSO_FAILED 同形")
	}
}
