package storage

// Job000103 存储位置端点守卫（不依赖 DB 的前置分支：slug 校验/403/请求体格式）。
// DB 路径由 e2e 覆盖；本文件钉死「非法名绝不触池」「非管理员绝不触池」两条 FailFast 口径，
// 防后续重构把校验沉到 DB 层才报错（友好 400 是契约）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func setupLocRouter(h *Handler, role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "u1")
		c.Set("role", role)
		c.Next()
	})
	r.POST("/storage/locations", h.CreateLocation)
	r.GET("/storage/locations", h.ListLocations)
	return r
}

// 非管理员 403 且不触池（Pool=nil，触池必 panic，能过即证前置）。
func TestCreateLocation_ForbiddenBeforePool(t *testing.T) {
	h := &Handler{Pool: nil}
	r := setupLocRouter(h, "user")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storage/locations",
		strings.NewReader(`{"name":"ssd-pool"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// 非法名 400 且不触池（nil pool 路径等价 FailFast 证据）。
func TestCreateLocation_InvalidNameBeforePool(t *testing.T) {
	h := &Handler{Pool: nil}
	r := setupLocRouter(h, "admin")
	cases := []string{
		`{"name":"SSD Pool"}`,     // 大写/空格
		`{"name":"-lead"}`,        // 非字母开头
		`{"name":"a/b"}`,          // 斜杠（路径穿越面）
		`{"name":"a..b"}`,         // 点号
		`{"name":""}`,             // 空
		`{"name":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`, // 33 位超长
	}
	for _, body := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/storage/locations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("body=%s want 400, got %d body=%s", body, w.Code, w.Body.String())
		}
		var resp struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("body=%s unmarshal: %v", body, err)
		}
		if resp.Error.Code != "INVALID_NAME" {
			t.Fatalf("body=%s want code INVALID_NAME, got %q", body, resp.Error.Code)
		}
	}
}

// 合法名通过 HTTP 校验层后才会触池——nil pool 下必须 500（CREATE_FAILED）而非 400，
// 证明校验已放行（错误码与 400 族明确区分，防校验逻辑被误删后门禁恒 400 假绿）。
func TestCreateLocation_ValidNameReachesPool(t *testing.T) {
	defer func() {
		if rec := recover(); rec != nil {
			// nil pool panic 也算「已触池」证据；但 500 分支更干净，见下。
			t.Logf("panic（触池证据）: %v", rec)
		}
	}()
	h := &Handler{Pool: nil}
	r := setupLocRouter(h, "admin")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/storage/locations",
		strings.NewReader(`{"name":"ssd-pool","description":"NVMe 池"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500（触池后失败）, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Error.Code != "CREATE_FAILED" {
		t.Fatalf("want CREATE_FAILED, got %q body=%s", resp.Error.Code, w.Body.String())
	}
}

// slug 正边界：合法名全集放行（到池层）。
func TestLocationNameRe_Boundary(t *testing.T) {
	ok := []string{"a", "ssd-pool", "hdd-archive-01", strings.Repeat("a", 32)}
	for _, s := range ok {
		if !LocationNameRe.MatchString(s) {
			t.Fatalf("want match: %q", s)
		}
	}
	bad := []string{"", "-a", "1a", "A", "a b", "a/b", "a.b", "a_1", strings.Repeat("a", 33)}
	for _, s := range bad {
		if LocationNameRe.MatchString(s) {
			t.Fatalf("want reject: %q", s)
		}
	}
}
