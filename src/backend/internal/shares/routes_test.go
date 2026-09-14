package shares

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestPhase4RoutesRegistrable 把交付给 cmd/api/main.go 的路由行原样注册一遍。
//
// 目的：gin 的路由树在**注册期**就会因通配符/参数段冲突而 panic，
// 而 main.go 由 team lead 统一修改、无法在本次改动里编译验证。
// 本测试用同一组路径做一次真实注册，确保交接的路由行不会让服务起不来；
// 同时固定「静态段优先于 :param」的行为（如 /og、/bandwidth-probe 不会被 :token 吃掉）。
func TestPhase4RoutesRegistrable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{}
	noop := func(c *gin.Context) { c.Next() }

	// 既有公开端点（Stage 2）
	r.GET("/public/shares/:token", h.PublicGet)
	r.GET("/public/shares/:token/media/:id/thumb", h.PublicThumb)
	r.GET("/public/shares/:token/media/:id/hls/*file", h.PublicHLS)
	r.GET("/public/shares/:token/media/:id/download", h.PublicDownload)

	// Phase 4 P1 新增：OG 封面 + 带宽探针/汇总（公开）
	r.GET("/public/shares/:token/og", h.PublicOG)
	r.GET("/public/shares/:token/bandwidth-probe", h.PublicProbeDown)
	r.POST("/public/shares/:token/bandwidth-probe", h.PublicProbeUp)
	r.POST("/public/shares/:token/bandwidth-test", h.PublicBandwidthTest)

	// 既有管理端点
	authed := r.Group("", noop)
	authed.POST("/shares", h.Create)
	authed.GET("/shares", h.List)
	authed.DELETE("/shares/:id", h.Delete)

	// Phase 4 P1 新增：登录态带宽
	authed.GET("/bandwidth", h.GetBandwidth)
	authed.PATCH("/bandwidth", h.PatchBandwidth)
	authed.GET("/bandwidth/probe", h.ProbeDown)
	authed.POST("/bandwidth/probe", h.ProbeUp)
	authed.POST("/bandwidth/self-test", h.SelfTest)

	want := map[string]bool{
		"GET /public/shares/:token/og":                  false,
		"GET /public/shares/:token/bandwidth-probe":     false,
		"POST /public/shares/:token/bandwidth-probe":    false,
		"POST /public/shares/:token/bandwidth-test":     false,
		"GET /public/shares/:token/media/:id/thumb":     false,
		"GET /public/shares/:token/media/:id/hls/*file": false,
		"GET /bandwidth":            false,
		"PATCH /bandwidth":          false,
		"GET /bandwidth/probe":      false,
		"POST /bandwidth/probe":     false,
		"POST /bandwidth/self-test": false,
		"GET /shares":               false,
		"POST /shares":              false,
		"DELETE /shares/:id":        false,
	}
	for _, ri := range r.Routes() {
		key := ri.Method + " " + ri.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("路由未注册成功: %s", k)
		}
	}
}

// TestOGPathNotSwallowedByTokenParam 静态段 /og 必须命中 OG 处理器，
// 而不是被 /public/shares/:token 兜住（交接 nginx 规则时依赖这一点）。
func TestOGPathNotSwallowedByTokenParam(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	hit := ""
	r.GET("/public/shares/:token", func(c *gin.Context) { hit = "token"; c.Status(200) })
	r.GET("/public/shares/:token/og", func(c *gin.Context) { hit = "og"; c.Status(200) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/shares/abc123/og", nil)
	r.ServeHTTP(w, req)
	if hit != "og" {
		t.Fatalf("/public/shares/abc123/og 应命中 OG 处理器，实际命中 %q", hit)
	}
}

// TestProbeQueryReachable /public/shares/:token/bandwidth-probe?bytes=N 的 query
// 必须能正常解析（探针尺寸可由调用方/BW_PROBE_BYTES 调整，此处固定契约）。
func TestProbeQueryReachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var got string
	r.GET("/public/shares/:token/bandwidth-probe", func(c *gin.Context) {
		got = c.Query("bytes")
		c.Status(200)
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/public/shares/tok/bandwidth-probe?bytes=131072", nil))
	if got != "131072" {
		t.Fatalf("bytes 参数应可解析，实际 %q", got)
	}
}
