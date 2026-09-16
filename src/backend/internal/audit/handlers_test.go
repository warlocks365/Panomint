package audit

// Handler 层单测：查询参数解析、错误映射、审计自记录。
//
// 用 gin 的真实路由 + httptest 跑，而不是直接调 parseXxx：
// 这样同时覆盖「解析 → 调 store → 写审计 → 回响应」的接线，
// 也顺带验证 handler 不会把 store 错误包装成 200（比只测解析函数更有价值）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

const testActor = "7c6b1b9c-cba2-4394-b982-67d9038421f3"

// newTestRouter 装配一个最小 gin 路由，模拟 main.go 里 authed 组的两个注入：
//   - user_id（由 auth.AuthRequired 注入）
//   - 真实 client IP（gin 从 RemoteAddr 推导）
func newTestRouter(h *Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("", func(c *gin.Context) {
		c.Set("user_id", testActor)
		c.Next()
	})
	g.GET("/admin/audit", h.ListAudit)
	g.GET("/admin/stats", h.Stats)
	g.GET("/admin/jobs", h.Jobs)
	g.GET("/admin/jobs/:id", h.GetJob)
	return r
}

func doGet(t *testing.T, r *gin.Engine, target string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("User-Agent", "pano-test/1.0")
	req.RemoteAddr = "192.168.1.115:51234"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body map[string]any
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s 响应不是 JSON: %s", target, w.Body.String())
		}
	}
	return w, body
}

// assertErrorBody 断言统一错误包络 {"error":{"code","message"}}。
func assertErrorBody(t *testing.T, w *httptest.ResponseRecorder, body map[string]any, wantStatus int, wantCode string) {
	t.Helper()
	if w.Code != wantStatus {
		t.Fatalf("状态码 %d，期望 %d（body=%s）", w.Code, wantStatus, w.Body.String())
	}
	er, ok := body["error"].(map[string]any)
	if !ok {
		t.Fatalf("错误响应应为 {\"error\":{...}}，实际 %s", w.Body.String())
	}
	if er["code"] != wantCode {
		t.Fatalf("错误码 %q，期望 %q", er["code"], wantCode)
	}
	if msg, _ := er["message"].(string); msg == "" {
		t.Fatalf("错误响应必须带 message：%s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /admin/audit
// ---------------------------------------------------------------------------

func TestListAuditParsesAllFilters(t *testing.T) {
	fs := &fakeStore{}
	h := &Handler{Store: fs}
	r := newTestRouter(h)

	w, _ := doGet(t, r, "/admin/audit?action=%20ADMIN.AUDIT.READ%20&actor="+strings.ToUpper(testActor)+
		"&target_type=User&target_id=abc&from=2026-09-01T00:00:00Z&to=2026-09-15T00:00:00Z&limit=500&cursor="+
		encodeCursor(mustTime(t, "2026-09-10T12:00:00Z"), 42))
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
	}

	f := fs.filter()
	if f.Action != ActionAuditRead {
		t.Fatalf("action 应归一化为小写：%q", f.Action)
	}
	if f.ActorUserID != testActor {
		t.Fatalf("actor 应归一化为小写 UUID：%q", f.ActorUserID)
	}
	if f.TargetType != TargetUser {
		t.Fatalf("target_type 应归一为小写：%q", f.TargetType)
	}
	if f.TargetID != "abc" {
		t.Fatalf("target_id = %q", f.TargetID)
	}
	if f.From == nil || f.From.Format("2006-01-02T15:04:05Z") != "2026-09-01T00:00:00Z" {
		t.Fatalf("from 解析错误：%v", f.From)
	}
	if f.To == nil || f.To.Format("2006-01-02T15:04:05Z") != "2026-09-15T00:00:00Z" {
		t.Fatalf("to 解析错误：%v", f.To)
	}
	// limit 超上限应被夹紧（而不是原样透给 SQL）。
	if f.Limit != maxAuditLimit {
		t.Fatalf("limit 应夹紧到 %d，实际 %d", maxAuditLimit, f.Limit)
	}
	if f.CursorAt == nil || f.CursorID == nil || *f.CursorID != 42 {
		t.Fatalf("游标未解析：%+v / %v", f.CursorAt, f.CursorID)
	}
}

func TestListAuditRejectsBadInput(t *testing.T) {
	h := &Handler{Store: &fakeStore{}}
	r := newTestRouter(h)

	cases := []struct{ name, query string }{
		{"action 非法字符", "?action=admin/audit"},
		{"action 内嵌换行", "?action=admin%0Aaudit"},
		{"actor 非 UUID", "?actor=not-a-uuid"},
		{"target_id 缺 target_type", "?target_id=abc"},
		{"target_type 非法", "?target_type=9bad"},
		{"from 非 RFC3339", "?from=2026-09-01"},
		{"from 晚于 to", "?from=2026-09-15T00:00:00Z&to=2026-09-01T00:00:00Z"},
		{"from 等于 to", "?from=2026-09-15T00:00:00Z&to=2026-09-15T00:00:00Z"},
		{"游标非法", "?cursor=!!!bad!!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, body := doGet(t, r, "/admin/audit"+tc.query)
			assertErrorBody(t, w, body, http.StatusBadRequest, CodeInvalidInput)
		})
	}
}

// TestListAuditBlankFilterIsNoFilter 全空白参数视为"不过滤"而非 400。
//
// 口径：过滤参数统一 trim；trim 后为空 = 不限制。保持宽松是刻意的 ——
// 「空 = 不限制」只有一条规则，比「空串不限制、空白串报错」这种按字节区分的
// 规则更难被前端用错。
func TestListAuditBlankFilterIsNoFilter(t *testing.T) {
	fs := &fakeStore{}
	r := newTestRouter(&Handler{Store: fs})
	w, _ := doGet(t, r, "/admin/audit?action=%20%20&actor=%20&target_type=")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
	}
	f := fs.filter()
	if f.Action != "" || f.ActorUserID != "" || f.TargetType != "" {
		t.Fatalf("空白参数应视为不过滤，实际 %+v", f)
	}
}

// TestListAuditSelfRecordsRead 审计查询本身必须留下一条 admin.audit.read。
//
// 这是本包当前唯一的生产写入点，也是「读取审计表本身有后果」这一判断的落地。
func TestListAuditSelfRecordsRead(t *testing.T) {
	fs := &fakeStore{}
	h := &Handler{Store: fs, Recorder: NewWithStore(fs, zap.NewNop())}
	r := newTestRouter(h)

	w, _ := doGet(t, r, "/admin/audit?action=share.create&limit=10")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", w.Code, w.Body.String())
	}

	entries := fs.entries()
	if len(entries) != 1 {
		t.Fatalf("应写入 1 条审计自记录，实际 %d：%+v", len(entries), entries)
	}
	e := entries[0]
	if e.Action != ActionAuditRead {
		t.Fatalf("自记录动作应为 %q，实际 %q", ActionAuditRead, e.Action)
	}
	if e.TargetType != TargetAuditLog {
		t.Fatalf("自记录 target_type 应为 %q，实际 %q", TargetAuditLog, e.TargetType)
	}
	if e.ActorUserID != testActor {
		t.Fatalf("自记录应从 gin 上下文取到触发者，实际 %q", e.ActorUserID)
	}
	if e.IP != "192.168.1.115" {
		t.Fatalf("自记录应记录来源 IP，实际 %q", e.IP)
	}
	if e.UserAgent != "pano-test/1.0" {
		t.Fatalf("自记录应记录 UA，实际 %q", e.UserAgent)
	}
	if e.Detail["action"] != ActionShareCreate {
		t.Fatalf("detail 应保留过滤条件：%+v", e.Detail)
	}
	if e.Detail["limit"] != 10 {
		t.Fatalf("detail.limit 应为 10：%+v", e.Detail)
	}
	// 不得把返回内容也塞进 detail（审计表会指数膨胀）。
	if _, hit := e.Detail["items"]; hit {
		t.Fatalf("detail 不得包含返回内容：%+v", e.Detail)
	}
}

// TestListAuditSelfRecordFailureDoesNotBreakResponse
// 端到端再验一次硬约束：审计写入炸了，业务响应必须照常 200。
func TestListAuditSelfRecordFailureDoesNotBreakResponse(t *testing.T) {
	core, logs := observerNew()
	fs := &fakeStore{insertErr: errBoom}
	h := &Handler{Store: fs, Recorder: NewWithStore(fs, zap.New(core))}
	r := newTestRouter(h)

	w, body := doGet(t, r, "/admin/audit")
	if w.Code != http.StatusOK {
		t.Fatalf("审计写入失败不得影响查询响应，实际状态码 %d：%s", w.Code, w.Body.String())
	}
	if _, ok := body["items"]; !ok {
		t.Fatalf("响应体应含 items：%s", w.Body.String())
	}
	if logs.Len() == 0 {
		t.Fatal("审计写入失败应留下 warn 日志")
	}
	if !strings.Contains(logs.All()[0].Message, "审计写入失败") {
		t.Fatalf("日志文案不符：%q", logs.All()[0].Message)
	}
}

// TestListAuditQueryErrorMapsTo500 store 报错必须 500，不能回 200 空列表
// （空列表会被前端读成"没有审计记录"，而事实是"查不到" —— 两者含义相反）。
func TestListAuditQueryErrorMapsTo500(t *testing.T) {
	h := &Handler{Store: &fakeStore{queryErr: errBoom}}
	r := newTestRouter(h)
	w, body := doGet(t, r, "/admin/audit")
	assertErrorBody(t, w, body, http.StatusInternalServerError, CodeInternal)
}

// TestListAuditNilRecorderOK Recorder 为 nil 时不得 panic（灰度/测试装配路径）。
func TestListAuditNilRecorderOK(t *testing.T) {
	h := &Handler{Store: &fakeStore{}}
	r := newTestRouter(h)
	w, _ := doGet(t, r, "/admin/audit")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /admin/stats
// ---------------------------------------------------------------------------

func TestStatsPassesThroughStoreResult(t *testing.T) {
	want := &Stats{
		MediaTotal:  93,
		Users:       4,
		StorageUsed: 39372205,
		IndexStatus: IndexInfo{State: "idle", Running: 0, LastJob: &Job{JobType: "index", ID: "j1", Status: "done"}},
	}
	h := &Handler{Store: &fakeStore{stats: want}}
	r := newTestRouter(h)

	w, body := doGet(t, r, "/admin/stats")
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d：%s", w.Code, w.Body.String())
	}
	// 契约 §14 的 4 个字段必须都在。
	for _, k := range []string{"media_total", "users", "storage_used", "index_status"} {
		if _, ok := body[k]; !ok {
			t.Fatalf("响应缺少契约字段 %q：%s", k, w.Body.String())
		}
	}
	if body["media_total"].(float64) != 93 || body["storage_used"].(float64) != 39372205 {
		t.Fatalf("字段值不符：%s", w.Body.String())
	}
	is := body["index_status"].(map[string]any)
	if is["state"] != "idle" {
		t.Fatalf("index_status.state 不符：%v", is)
	}
}

func TestStatsErrorMapsTo500(t *testing.T) {
	h := &Handler{Store: &fakeStore{statsErr: errBoom}}
	r := newTestRouter(h)
	w, body := doGet(t, r, "/admin/stats")
	assertErrorBody(t, w, body, http.StatusInternalServerError, CodeInternal)
}

// ---------------------------------------------------------------------------
// GET /admin/jobs
// ---------------------------------------------------------------------------

func TestJobsParsesQuery(t *testing.T) {
	fs := &fakeStore{}
	h := &Handler{Store: fs}
	r := newTestRouter(h)

	cases := []struct {
		query      string
		wantType   string
		wantStatus string
		wantLimit  int
	}{
		{"", "", "", defaultJobsLimit},
		{"?type=all", "", "", defaultJobsLimit},
		{"?type=INDEX", "index", "", defaultJobsLimit},
		{"?type=transcode&status=done&limit=5", "transcode", "done", 5},
		{"?limit=99999", "", "", maxJobsLimit}, // 超上限夹到上限（不是回落默认值）
	}
	for _, tc := range cases {
		w, body := doGet(t, r, "/admin/jobs"+tc.query)
		if w.Code != http.StatusOK {
			t.Fatalf("%s 状态码 %d：%s", tc.query, w.Code, w.Body.String())
		}
		q := fs.jobQuery()
		if q.JobType != tc.wantType || q.Status != tc.wantStatus || q.Limit != tc.wantLimit {
			t.Fatalf("%s 解析为 %+v，期望 type=%q status=%q limit=%d",
				tc.query, q, tc.wantType, tc.wantStatus, tc.wantLimit)
		}
		if _, ok := body["items"]; !ok {
			t.Fatalf("响应应含 items：%s", w.Body.String())
		}
		if body["limit"].(float64) != float64(tc.wantLimit) {
			t.Fatalf("响应 limit 应为 %d：%s", tc.wantLimit, w.Body.String())
		}
	}
}

func TestJobsRejectsBadQuery(t *testing.T) {
	h := &Handler{Store: &fakeStore{}}
	r := newTestRouter(h)
	for _, q := range []string{
		"?type=media",
		"?type=" + strings.Repeat("a", 300),
		"?status=DONE",                       // 大写：状态库内为小写，不做归一化以免掩盖脏数据
		"?status=1running",                   // 数字开头
		"?status=" + strings.Repeat("a", 40), // 超长
	} {
		w, body := doGet(t, r, "/admin/jobs"+q)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d：%s", q, w.Code, w.Body.String())
		}
		er, _ := body["error"].(map[string]any)
		if er["code"] != CodeInvalidInput {
			t.Fatalf("%s 错误码不符：%s", q, w.Body.String())
		}
	}
}

func TestJobsErrorMapsTo500(t *testing.T) {
	h := &Handler{Store: &fakeStore{jobsErr: errBoom}}
	r := newTestRouter(h)
	w, body := doGet(t, r, "/admin/jobs")
	assertErrorBody(t, w, body, http.StatusInternalServerError, CodeInternal)
}

// TestJobsEmptyIsArrayNotNull 空结果必须是 []，不能是 null。
// 前端 Array.map 遇到 null 会整页崩 —— 项目既有列表端点都遵循这条。
func TestJobsEmptyIsArrayNotNull(t *testing.T) {
	h := &Handler{Store: &fakeStore{jobs: []Job{}}}
	r := newTestRouter(h)
	w, _ := doGet(t, r, "/admin/jobs")
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("空任务列表应序列化为 []：%s", w.Body.String())
	}
}
