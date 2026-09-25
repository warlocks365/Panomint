package storage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestNormalizeLandingDir 落点校验（Job000118）：逐段 slug / 拒穿越 / 保留前缀黑名单。
func TestNormalizeLandingDir(t *testing.T) {
	ok := []struct{ in, want string }{
		{"imports/holiday-2026", "imports/holiday-2026"},
		{"/imports/holiday/", "imports/holiday"},   // 首尾斜杠规整
		{"imports/a/b/c", "imports/a/b/c"},         // 多级
		{"a", "a"},                                 // 单段
		{"9lives/cat-1", "9lives/cat-1"},           // 数字开头段
	}
	for _, tc := range ok {
		got, err := normalizeLandingDir(tc.in)
		if err != nil {
			t.Errorf("%q 应合法: %v", tc.in, err)
		} else if got != tc.want {
			t.Errorf("%q → %q， want %q", tc.in, got, tc.want)
		}
	}
	bad := []string{
		"",
		"/",
		"../escape",
		"imports/../escape",
		"imports//double",
		"imports/./dot",
		"Imports/Case",      // 大写不在 slug 口径
		"im ports/x",        // 空格
		"_imports/legacy",   // 系统保留前缀（旧技术前缀）
		"photos/@eaDir",     // Synology 元数据目录
		"photos/@EAdir/x",   // 大小写不敏感
		"foo_bar/x",         // 下划线不在 slug 口径
		"foo/" + strings.Repeat("a", 65), // 段超 64
	}
	for _, in := range bad {
		if _, err := normalizeLandingDir(in); err == nil {
			t.Errorf("%q 应被拒绝", in)
		}
	}
}

func TestSlugifyName(t *testing.T) {
	cases := map[string]string{
		"我的相册 2026":  "2026",
		"Holiday Photos!": "holiday-photos",
		"NAS_Backup":      "nas-backup",
		"---":             "",
		"abc":             "abc",
	}
	for in, want := range cases {
		if got := slugifyName(in); got != want {
			t.Errorf("slugifyName(%q)=%q want %q", in, got, want)
		}
	}
}

// 前置守卫测试：403/400 在触及 Pool 之前返回——Pool=nil 不 panic 即证明顺序正确。
func newCreateRouter(role string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", "11111111-1111-1111-1111-111111111111")
		c.Set("role", role)
		c.Next()
	})
	h := &Handler{} // Pool 为 nil：守卫必须先于一切池操作
	r.POST("/storage/mounts", h.Create)
	return r
}

func postCreate(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/storage/mounts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreate_ForbiddenBeforePool(t *testing.T) {
	r := newCreateRouter("member")
	w := postCreate(t, r, `{"name":"x","type":"nfs","conn":{"host":"h","export":"/x"}}`)
	if w.Code != http.StatusForbidden {
		t.Errorf("非管理员应 403，got %d", w.Code)
	}
}

func TestCreate_BadLandingDirBeforePool(t *testing.T) {
	r := newCreateRouter("owner")
	body, _ := json.Marshal(map[string]any{
		"name": "x", "type": "nfs", "conn": map[string]string{"host": "h", "export": "/x"},
		"landing_dir": "../escape",
	})
	w := postCreate(t, r, string(body))
	if w.Code != http.StatusBadRequest {
		t.Errorf("非法落点应 400，got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "BAD_LANDING_DIR") {
		t.Errorf("错误码应 BAD_LANDING_DIR: %s", w.Body.String())
	}
}

func TestCreate_BadTypeBeforePool(t *testing.T) {
	r := newCreateRouter("owner")
	w := postCreate(t, r, `{"name":"x","type":"ftp","conn":{}}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("非法 type 应 400，got %d", w.Code)
	}
}
