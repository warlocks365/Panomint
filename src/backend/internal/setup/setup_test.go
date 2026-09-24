package setup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fakeStore 是最小的可编程 Store：Initialized/CreateOwner 的行为由调用方钉死，
// 用来验证 handler 的每个分支（不依赖真实 PG —— 本机/CI 无数据库）。
type fakeStore struct {
	initialized      bool
	initErr          error
	createErr        error
	created          int // CreateOwner 实际被调用的次数
	lastEmail        string
	lastDisplayName  string
	lastPassword     string
	flipAfterCreate  bool // 创建成功后即视为已初始化（模拟第二次请求）
}

func (f *fakeStore) Initialized(ctx context.Context) (bool, error) {
	return f.initialized, f.initErr
}

func (f *fakeStore) CreateOwner(ctx context.Context, email, displayName, password string) (string, error) {
	// 与 PGStore 同契约：内部先判"当前确无用户"，已初始化即拒绝（调用方不预查）。
	if f.initialized {
		return "", ErrAlreadyInitialized
	}
	f.created++
	f.lastEmail, f.lastDisplayName, f.lastPassword = email, displayName, password
	if errors.Is(f.createErr, ErrAlreadyInitialized) {
		return "", f.createErr
	}
	if f.createErr != nil {
		return "", f.createErr
	}
	if f.flipAfterCreate {
		f.initialized = true
	}
	return "11111111-2222-3333-4444-555555555555", nil
}

func newTestRouter(store Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := &Handler{Store: store}
	r.GET("/setup/status", h.Status)
	r.POST("/setup", h.Create)
	return r
}

func doRequest(t *testing.T, r *gin.Engine, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *strings.Reader = strings.NewReader(body)
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var parsed map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &parsed)
	}
	return rec, parsed
}

// G1：未初始化 → status.initialized=false；已初始化 → true（两个方向都钉死）。
func TestStatus_ReportsBothDirections(t *testing.T) {
	// 未初始化
	r := newTestRouter(&fakeStore{initialized: false})
	rec, body := doRequest(t, r, http.MethodGet, "/setup/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("未初始化 status 期望 200，得 %d", rec.Code)
	}
	if body["initialized"] != false {
		t.Fatalf("未初始化时期望 initialized=false，得 %v", body["initialized"])
	}
	if v, ok := body["version"].(string); !ok || v == "" {
		t.Fatalf("status 必须携带非空 version 字段，得 %v", body["version"])
	}

	// 已初始化
	r = newTestRouter(&fakeStore{initialized: true})
	rec, body = doRequest(t, r, http.MethodGet, "/setup/status", "")
	if rec.Code != http.StatusOK || body["initialized"] != true {
		t.Fatalf("已初始化 status 期望 200 + initialized=true，得 %d %v", rec.Code, body)
	}
}

// G2：FAIL-CLOSED —— 初始化状态查库失败 → 500，绝不可按"已初始化"放行。
func TestStatus_QueryErrorIsFailClosed(t *testing.T) {
	r := newTestRouter(&fakeStore{initErr: errors.New("connection refused")})
	rec, _ := doRequest(t, r, http.MethodGet, "/setup/status", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("查询失败时期望 500（fail-closed），得 %d", rec.Code)
	}
}

// G3：正常初始化 —— 201 回 id/email，且创建入参被原样传递（含默认显示名）。
func TestCreate_HappyPath(t *testing.T) {
	fs := &fakeStore{initialized: false, flipAfterCreate: true}
	r := newTestRouter(fs)
	rec, body := doRequest(t, r, http.MethodPost, "/setup",
		`{"email":"boss@pano.local","password":"sup3r-secret","display_name":"老板"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("期望 201，得 %d（body=%s）", rec.Code, rec.Body.String())
	}
	if fs.created != 1 {
		t.Fatalf("CreateOwner 应被调用 1 次，得 %d", fs.created)
	}
	if fs.lastEmail != "boss@pano.local" || fs.lastDisplayName != "老板" {
		t.Fatalf("入参传递失真：email=%q name=%q", fs.lastEmail, fs.lastDisplayName)
	}
	if id, _ := body["id"].(string); id == "" {
		t.Fatalf("201 响应必须回 id，得 %v", body)
	}
}

// G4：显示名缺省回落「管理员」。
func TestCreate_DefaultDisplayName(t *testing.T) {
	fs := &fakeStore{}
	r := newTestRouter(fs)
	doRequest(t, r, http.MethodPost, "/setup", `{"email":"a@b.co","password":"12345678"}`)
	if fs.lastDisplayName != "管理员" {
		t.Fatalf("显示名缺省应回落「管理员」，得 %q", fs.lastDisplayName)
	}
}

// G5：参数校验 —— 坏邮箱 / 短密码 → 400，且**不得触及** Store.CreateOwner。
func TestCreate_ValidationMatrix(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"坏邮箱", `{"email":"not-an-email","password":"12345678"}`},
		{"缺邮箱", `{"password":"12345678"}`},
		{"短密码", `{"email":"a@b.co","password":"short"}`},
		{"缺密码", `{"email":"a@b.co"}`},
		{"显示名超长", `{"email":"a@b.co","password":"12345678","display_name":"` + strings.Repeat("x", 65) + `"}`},
	}
	for _, tc := range cases {
		fs := &fakeStore{}
		r := newTestRouter(fs)
		rec, _ := doRequest(t, r, http.MethodPost, "/setup", tc.body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s：期望 400，得 %d", tc.name, rec.Code)
		}
		if fs.created != 0 {
			t.Fatalf("%s：校验失败绝不允许触及创建（created=%d）", tc.name, fs.created)
		}
	}
}

// G6：一次性 —— 已初始化时 POST /setup → 409 SETUP_COMPLETED，且创建零调用。
// （创建过程中的并发竞态由 PGStore 的咨询锁事务保证，此处钉 handler 分支。）
func TestCreate_AlreadyInitializedConflict(t *testing.T) {
	fs := &fakeStore{initialized: true}
	r := newTestRouter(fs)
	rec, body := doRequest(t, r, http.MethodPost, "/setup",
		`{"email":"late@pano.local","password":"12345678"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("已初始化时期望 409，得 %d", rec.Code)
	}
	if code, _ := body["error"].(map[string]any)["code"].(string); code != "SETUP_COMPLETED" {
		t.Fatalf("错误码应为 SETUP_COMPLETED，得 %v", body)
	}
	if fs.created != 0 {
		t.Fatalf("已初始化时绝不允许真正创建用户（created=%d）", fs.created)
	}
}

// G7：创建落库失败 → 500（fail-closed，不伪装成功）。
func TestCreate_StoreError(t *testing.T) {
	fs := &fakeStore{createErr: errors.New("db down")}
	r := newTestRouter(fs)
	rec, _ := doRequest(t, r, http.MethodPost, "/setup",
		`{"email":"a@b.co","password":"12345678"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("落库失败期望 500，得 %d", rec.Code)
	}
}

// G8：第二个并发请求在创建提交后到达 —— store 报 ErrAlreadyInitialized → 409。
// （竞态由 PGStore 的咨询锁事务在库层保证，此处钉 handler 对 ErrAlreadyInitialized 的分支。）
func TestCreate_RaceLoserGetsConflict(t *testing.T) {
	loser := &fakeStore{createErr: ErrAlreadyInitialized}
	r := newTestRouter(loser)
	rec, _ := doRequest(t, r, http.MethodPost, "/setup",
		`{"email":"second@pano.local","password":"12345678"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("竞态败者期望 409，得 %d", rec.Code)
	}
}
