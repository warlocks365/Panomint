package httpsconfig

// Job000125 测试：源码形状守卫（存储语义）+ 行为级断言（中间件三边界、上传校验）。
// 与 transcode/sysconfig_test.go 同口径：无真库时行为级存储断言由部署后 verify 覆盖。

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// --- 中间件行为级断言（开关读取器注入，不碰库） ---

func forceTester(t *testing.T, force bool) *httptest.ResponseRecorder {
	t.Helper()
	return forceTesterPort(t, force, 443)
}

// forceTesterPort 注入 (force, httpsPort) 的中间件测试器（Job000128 点 9 扩展）。
func forceTesterPort(t *testing.T, force bool, httpsPort int) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(forceHTTPSWith(func(context.Context) (bool, int) { return force, httpsPort }))
	hit := false
	r.GET("/api/ping", func(c *gin.Context) { hit = true; c.String(http.StatusOK, "pong") })
	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Host = "pano.example.com"
	req.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if force && hit {
		t.Fatal("强制 HTTPS 生效时请求不得到达 handler")
	}
	if !force && !hit && w.Code != http.StatusMovedPermanently {
		t.Fatalf("开关关闭时应直达 handler（got code=%d hit=%v）", w.Code, hit)
	}
	return w
}

func TestForceHTTPSRedirectsPlainHTTP(t *testing.T) {
	w := forceTester(t, true)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("XFP=http + 开关开 必须 301（got %d）", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "https://pano.example.com/api/ping" {
		t.Fatalf("Location 必须保留 host+path+query（got %q）", loc)
	}
}

func TestForceHTTPSPassesHTTPSAndBare(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(forceHTTPSWith(func(context.Context) (bool, int) { return true, 443 }))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	// XFP=https：不拦
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("XFP=https 不得拦截（got %d）", w.Code)
	}

	// XFP 缺失（裸 HTTP 部署/内部探针）：不拦 —— 防全站 301 死局的关键边界
	req2 := httptest.NewRequest(http.MethodGet, "/x", nil)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("XFP 缺失不得拦截（got %d）——无反代部署会因此全站不可达", w2.Code)
	}
}

func TestForceHTTPSExemptsHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(forceHTTPSWith(func(context.Context) (bool, int) { return true, 443 }))
	called := false
	r.GET("/health", func(c *gin.Context) { called = true; c.String(http.StatusOK, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !called {
		t.Fatalf("/health 必须豁免跳转（got %d called=%v）——巡检探活不跟随重定向", w.Code, called)
	}
}

// --- Job000128 点 9：重定向端口 ---

func TestForceHTTPSRedirectUsesConfiguredPort(t *testing.T) {
	// HTTPS 挂在 8443：目标必须带 :8443（否则跳到 443 上无监听 → 连接失败）
	w := forceTesterPort(t, true, 8443)
	if w.Code != http.StatusMovedPermanently {
		t.Fatalf("must 301（got %d）", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "https://pano.example.com:8443/api/ping" {
		t.Fatalf("Location 必须携带配置的 HTTPS 端口（got %q）", loc)
	}
}

func TestForceHTTPSRedirectStripsLegacyPort(t *testing.T) {
	// Host 自带旧端口（如 HTTP 入口 host:8080）：剥掉再拼配置端口，不得出现双端口
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(forceHTTPSWith(func(context.Context) (bool, int) { return true, 8443 }))
	r.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Host = "pano.example.com:8080"
	req.Header.Set("X-Forwarded-Proto", "http")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if loc := w.Header().Get("Location"); loc != "https://pano.example.com:8443/x" {
		t.Fatalf("Location 必须剥旧端口拼新端口（got %q）", loc)
	}
}

func TestForceHTTPSRedirectPort443StaysBare(t *testing.T) {
	// 443：不带端口（URL 干净的常规形态）
	w := forceTesterPort(t, true, 443)
	if loc := w.Header().Get("Location"); loc != "https://pano.example.com/api/ping" {
		t.Fatalf("443 不得在 Location 携带端口（got %q）", loc)
	}
}

// --- 上传链路：生成自签证书做真配对校验 ---

func genSelfSigned(t *testing.T) (certPEM, keyPEM []byte, notAfter time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pano-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		DNSNames:     []string{"pano.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, tmpl.NotAfter
}

func TestX509KeyPairAcceptsGeneratedPair(t *testing.T) {
	certPEM, keyPEM, _ := genSelfSigned(t)
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("自签证书对必须通过 X509KeyPair: %v", err)
	}
}

func TestWriteCertFilesPerms(t *testing.T) {
	certPEM, keyPEM, _ := genSelfSigned(t)
	dir := t.TempDir()
	if err := writeCertFiles(dir, certPEM, keyPEM); err != nil {
		t.Fatalf("落盘失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, CertFileName)); err != nil {
		t.Fatalf("证书文件缺失: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, CertKeyFileName)); err != nil {
		t.Fatalf("私钥文件缺失: %v", err)
	}
}

func TestApplyHintMentionsCaddyRestart(t *testing.T) {
	if !strings.Contains(ApplyHint, "restart caddy") {
		t.Fatal("生效指引必须包含 restart caddy（热切换被设计排除，指引是唯一生效路径）")
	}
	if DefaultCertPath != "" {
		t.Fatal("默认证书路径必须为空串（上传端点在未配置 HTTPS_CERT_DIR 时须显式 503）")
	}
}

// TestInvalidateForceCache 写侧失效（Job000125 e2e 踩出）：PutConfig 写成功后必须
// 让 sharedForceCache 立即过期，否则开关变更最长 60s 内对中间件不可见，
// 「热生效」名存实亡。纯时间戳断言，无 DB 依赖。
func TestInvalidateForceCache(t *testing.T) {
	sharedForceCache.mu.Lock()
	sharedForceCache.expires = time.Now().Add(time.Hour) // 模拟缓存命中窗口
	sharedForceCache.value = true
	sharedForceCache.mu.Unlock()

	InvalidateForceCache()

	sharedForceCache.mu.Lock()
	defer sharedForceCache.mu.Unlock()
	if !sharedForceCache.expires.IsZero() {
		t.Fatalf("失效后 expires 必须为零值（下次 get 必重读库），实际 %v", sharedForceCache.expires)
	}
}

// --- 源码形状守卫（存储语义钉） ---

func TestStoreShapeGuards(t *testing.T) {
	b, err := os.ReadFile("httpsconfig.go")
	if err != nil {
		t.Fatalf("读不到 httpsconfig.go: %v", err)
	}
	seg := string(b)
	if !strings.Contains(seg, "ON CONFLICT (singleton) DO UPDATE") {
		t.Fatal("写入必须走 singleton 原子 upsert（迁移 00042 单行表约束）")
	}
	if !strings.Contains(seg, "COALESCE($1::boolean, $5::boolean)") {
		t.Fatal("INSERT 分支必须显式 ::boolean 锚定（unknown 类型撞 42804，transcode 包同因）")
	}
	if !strings.Contains(seg, "COALESCE($4::timestamptz, $8::timestamptz)") {
		t.Fatal("cert_not_after 必须显式 ::timestamptz 锚定")
	}
	if !strings.Contains(seg, "errors.Is(err, pgx.ErrNoRows)") {
		t.Fatal("Get 必须把 ErrNoRows 排除在错误分支外（缺行=默认值）")
	}
	if DefaultForceHTTPS != false {
		t.Fatal("DefaultForceHTTPS 必须恒为 false（存量部署行为不变）")
	}
}
