package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Job000129 AuthRequired query token 分支守卫测试。
// 硬编码期望值当参照物（守卫纪律：不拿产品函数互比）：
//  · GET /media/ 读路径：Bearer 头 / ?at= query 双通道均可过，注入同一 user_id/role
//  · 白名单收窄：POST /media/ 带 ?at= 必须 401（写路径拒绝 query token）
//  · 白名单收窄：GET 非 /media/ 前缀带 ?at= 必须 401（防 URL 泄露横向重放）
//  · ?at= 非法值必须 INVALID_TOKEN；无头无 query 维持 UNAUTHORIZED（回归）

func newAuthTestRouter(secret []byte) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthRequired(secret))
	r.GET("/media/:id/download", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString("user_id"), "role": c.GetString("role")})
	})
	r.POST("/media/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/albums", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func doAuthReq(r *gin.Engine, method, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthRequiredQueryToken(t *testing.T) {
	secret := []byte("job000129-middleware-test-secret")
	tok, err := IssueAccess(secret, "u-1", "admin")
	if err != nil {
		t.Fatalf("签发测试 token 失败: %v", err)
	}
	r := newAuthTestRouter(secret)

	t.Run("GET /media/ + 有效 ?at= → 200 且注入 claims", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/media/m1/download?at="+tok, "")
		if w.Code != http.StatusOK {
			t.Fatalf("应 200，得 %d body=%s", w.Code, w.Body.String())
		}
		if !containsAll(w.Body.String(), []string{"u-1", "admin"}) {
			t.Fatalf("响应应含 user_id/role，得 %s", w.Body.String())
		}
	})

	t.Run("GET /media/ + Bearer 头（回归）→ 200", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/media/m1/download", "Bearer "+tok)
		if w.Code != http.StatusOK {
			t.Fatalf("Bearer 回归应 200，得 %d", w.Code)
		}
	})

	t.Run("POST /media/ 带 ?at= → 401（写路径拒绝 query token）", func(t *testing.T) {
		w := doAuthReq(r, http.MethodPost, "/media/m1?at="+tok, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("POST 带 ?at= 必须 401，得 %d body=%s", w.Code, w.Body.String())
		}
		if !containsAll(w.Body.String(), []string{"UNAUTHORIZED"}) {
			t.Fatalf("应报 UNAUTHORIZED（不暴露 query 通道存在性），得 %s", w.Body.String())
		}
	})

	t.Run("GET 非 /media/ 前缀带 ?at= → 401", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/albums?at="+tok, "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("非媒体路径带 ?at= 必须 401，得 %d", w.Code)
		}
	})

	t.Run("GET /media/ + 非法 ?at= → 401 INVALID_TOKEN", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/media/m1/download?at=garbage", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("应 401，得 %d", w.Code)
		}
		if !containsAll(w.Body.String(), []string{"INVALID_TOKEN"}) {
			t.Fatalf("应报 INVALID_TOKEN，得 %s", w.Body.String())
		}
	})

	t.Run("无 Authorization 头且无 ?at=（回归）→ 401 UNAUTHORIZED", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/media/m1/download", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("应 401，得 %d", w.Code)
		}
		if !containsAll(w.Body.String(), []string{"UNAUTHORIZED"}) {
			t.Fatalf("应报 UNAUTHORIZED，得 %s", w.Body.String())
		}
	})

	t.Run("Authorization 头非 Bearer 且 GET /media/ 无 ?at= → 401（不被 query 分支吞掉）", func(t *testing.T) {
		w := doAuthReq(r, http.MethodGet, "/media/m1/download", "Basic dXNlcjpwYXNz")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("Basic 头应 401，得 %d", w.Code)
		}
	})
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
