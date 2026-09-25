package index

// 成员端扫描端点（Job000123）的 handler 级测试。
//
// DB 依赖用脚本化替身（fakeMemberDB）：按调用顺序返回预置行，钉死
// 「未分配=403 / 越界=400 / 保留段=400 / 任务归属过滤=404」四条安全口径。
// 文件系统用 t.TempDir() 真实目录——钳制判定必须与真实路径解析行为一致。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

// scriptedRow 一个可编程的 pgx.Row。
type scriptedRow struct {
	scan func(dest ...any) error
}

func (r *scriptedRow) Scan(dest ...any) error { return r.scan(dest...) }

// fakeMemberDB 按脚本顺序返回行的 MemberDB 替身。
type fakeMemberDB struct {
	rows []pgx.Row
	n    int
}

func (f *fakeMemberDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	if f.n >= len(f.rows) {
		panic("fakeMemberDB: 脚本行耗尽（调用方查得比测试预期多）")
	}
	r := f.rows[f.n]
	f.n++
	return r
}

// nullScanRootRow users.scan_root = NULL 的查询行。
func nullScanRootRow() pgx.Row {
	return &scriptedRow{scan: func(dest ...any) error {
		p, ok := dest[0].(**string)
		if !ok {
			return fmt.Errorf("dest[0] 应为 **string")
		}
		*p = nil
		return nil
	}}
}

// scanRootRow users.scan_root = v 的查询行。
func scanRootRow(v string) pgx.Row {
	return &scriptedRow{scan: func(dest ...any) error {
		p, ok := dest[0].(**string)
		if !ok {
			return fmt.Errorf("dest[0] 应为 **string")
		}
		*p = &v
		return nil
	}}
}

// errRow Scan 恒返回 err 的查询行。
func errRow(err error) pgx.Row {
	return &scriptedRow{scan: func(dest ...any) error { return err }}
}

// jobRow 返回一条 index_jobs 行的查询行。
func jobRow(id, kind, status string, total, processed int) pgx.Row {
	return &scriptedRow{scan: func(dest ...any) error {
		if len(dest) != 8 {
			return fmt.Errorf("jobRow: dest 数=%d，want 8", len(dest))
		}
		idPtr, _ := dest[0].(*string)
		kindPtr, _ := dest[1].(*string)
		statusPtr, _ := dest[2].(*string)
		totalPtr, _ := dest[3].(**int)
		processedPtr, _ := dest[4].(**int)
		startedPtr, _ := dest[5].(**time.Time)
		finishedPtr, _ := dest[6].(**time.Time)
		createdPtr, _ := dest[7].(*time.Time)
		*idPtr, *kindPtr, *statusPtr = id, kind, status
		*totalPtr, *processedPtr = &total, &processed
		*startedPtr, *finishedPtr = nil, nil
		*createdPtr = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
		return nil
	}}
}

func newMemberTestRouter(h *Handler, userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	setUser := func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	}
	r.POST("/scan", setUser, h.ScanMine)
	r.GET("/fs/tree", setUser, h.ListDirTreeMine)
	r.GET("/jobs/:id", setUser, h.MyJobStatus)
	return r
}

func doMemberScan(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/scan", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMemberTree(t *testing.T, r *gin.Engine, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/fs/tree?"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doMemberJob(t *testing.T, r *gin.Engine, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/jobs/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// errBody 取错误响应的 code 字段。
func errCodeOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析错误响应失败: %v body=%s", err, w.Body.String())
	}
	return body.Error.Code
}

func TestScanMine_RequiresScanRoot(t *testing.T) {
	h := &Handler{MediaRoot: t.TempDir(), Pool: &fakeMemberDB{rows: []pgx.Row{nullScanRootRow()}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberScan(t, r, `{"dir":""}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d，want 403 body=%s", w.Code, w.Body.String())
	}
	if code := errCodeOf(t, w); code != "SCAN_ROOT_REQUIRED" {
		t.Fatalf("code=%s，want SCAN_ROOT_REQUIRED", code)
	}
}

func TestScanMine_EscapesScope(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos")); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("photos")}}}
	r := newMemberTestRouter(h, "user-1")

	// dir 相对 scan_root 解析："../outside" 越过 photos → 400 路径穿越。
	w := doMemberScan(t, r, `{"dir":"../outside"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d，want 400 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "不允许路径穿越") {
		t.Fatalf("响应应含穿越文案 body=%s", w.Body.String())
	}
}

func TestScanMine_ReservedSegment(t *testing.T) {
	root := t.TempDir()
	h := &Handler{MediaRoot: root, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("")}}}
	r := newMemberTestRouter(h, "user-1")

	// scan_root = ""（整个媒体根）时 _imports 必须仍被黑名单挡住（resolve 不查存在性，
	// 保留段判定在 os.Stat 之前——即便目录不存在也不能透）。
	w := doMemberScan(t, r, `{"dir":"_imports"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d，want 400 body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "系统保留区") {
		t.Fatalf("响应应含保留区文案 body=%s", w.Body.String())
	}
}

func TestScanMine_SuccessWithinRoot(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos", "trip")); err != nil {
		t.Fatal(err)
	}
	fs := &fakeScanner{jobID: "job-42"}
	h := &Handler{MediaRoot: root, Indexer: fs, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("photos")}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberScan(t, r, `{"dir":"trip"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d，want 202 body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		JobID  string `json:"job_id"`
		Root   string `json:"root"`
		Dir    string `json:"dir"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.JobID != "job-42" || resp.Status != "running" {
		t.Fatalf("job 响应异常: %+v", resp)
	}
	if resp.Root != "photos" || resp.Dir != "trip" {
		t.Fatalf("root/dir 回显异常: %+v", resp)
	}
	// 入库归属真实调用者，扫描根边界 = MEDIA_ROOT/photos/trip。
	if fs.gotOwner != "user-1" {
		t.Fatalf("gotOwner=%s，want user-1", fs.gotOwner)
	}
	wantAbs := filepath.Join(root, "photos", "trip")
	if fs.gotRoot != wantAbs {
		t.Fatalf("gotRoot=%s，want %s", fs.gotRoot, wantAbs)
	}
}

func TestScanMine_EmptyDirMeansScanRootItself(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos")); err != nil {
		t.Fatal(err)
	}
	fs := &fakeScanner{jobID: "job-43"}
	h := &Handler{MediaRoot: root, Indexer: fs, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("photos")}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberScan(t, r, `{"dir":""}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d，want 202 body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Root string `json:"root"`
		Dir  string `json:"dir"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Root != "photos" || resp.Dir != "." {
		t.Fatalf("空 dir 应扫 scan_root 本身: %+v", resp)
	}
	if fs.gotRoot != filepath.Join(root, "photos") {
		t.Fatalf("gotRoot=%s", fs.gotRoot)
	}
}

func TestListDirTreeMine_FiltersReservedSegment(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"keepme", "_imports", "other"} {
		if err := mkdirAll(filepath.Join(root, d)); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handler{MediaRoot: root, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("")}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberTree(t, r, "dir=")
	resp := decodeTree(t, w)
	names := map[string]bool{}
	for _, it := range resp.Items {
		names[it.Name] = true
	}
	if names["_imports"] {
		t.Fatalf("保留段 _imports 不应出现在成员树: %+v", resp.Items)
	}
	if !names["keepme"] || !names["other"] {
		t.Fatalf("正常目录缺失: %+v", resp.Items)
	}
	if resp.Root != "." {
		t.Fatalf("root=%s，want .（媒体根本身）", resp.Root)
	}
}

func TestListDirTreeMine_EscapesScope(t *testing.T) {
	root := t.TempDir()
	if err := mkdirAll(filepath.Join(root, "photos")); err != nil {
		t.Fatal(err)
	}
	h := &Handler{MediaRoot: root, Pool: &fakeMemberDB{rows: []pgx.Row{scanRootRow("photos")}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberTree(t, r, "dir=../outside")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d，want 400 body=%s", w.Code, w.Body.String())
	}
}

func TestMyJobStatus_BadUUIDRejectedBeforeQuery(t *testing.T) {
	root := t.TempDir()
	db := &fakeMemberDB{rows: []pgx.Row{scanRootRow("")}}
	h := &Handler{MediaRoot: root, Pool: db}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberJob(t, r, "not-a-uuid")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d，want 400 body=%s", w.Code, w.Body.String())
	}
	if db.n != 1 {
		t.Fatalf("UUID 校验应先于任务查询，users 查询=%d 次、任务查询不应发生", db.n)
	}
}

func TestMyJobStatus_NotMineIs404(t *testing.T) {
	root := t.TempDir()
	db := &fakeMemberDB{rows: []pgx.Row{scanRootRow(""), errRow(pgx.ErrNoRows)}}
	h := &Handler{MediaRoot: root, Pool: db}
	r := newMemberTestRouter(h, "user-1")

	// 任务存在但不属于调用者（或根本不存在）→ 404，与"不存在"逐字节同形。
	w := doMemberJob(t, r, "11111111-2222-3333-4444-555555555555")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d，want 404 body=%s", w.Code, w.Body.String())
	}
	if code := errCodeOf(t, w); code != "JOB_NOT_FOUND" {
		t.Fatalf("code=%s，want JOB_NOT_FOUND", code)
	}
}

func TestMyJobStatus_RequiresScanRoot(t *testing.T) {
	h := &Handler{MediaRoot: t.TempDir(), Pool: &fakeMemberDB{rows: []pgx.Row{nullScanRootRow()}}}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberJob(t, r, "11111111-2222-3333-4444-555555555555")
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d，want 403 body=%s", w.Code, w.Body.String())
	}
	if code := errCodeOf(t, w); code != "SCAN_ROOT_REQUIRED" {
		t.Fatalf("code=%s，want SCAN_ROOT_REQUIRED", code)
	}
}

func TestMyJobStatus_Success(t *testing.T) {
	root := t.TempDir()
	jobID := "11111111-2222-3333-4444-555555555555"
	db := &fakeMemberDB{rows: []pgx.Row{
		scanRootRow("photos"),
		jobRow(jobID, "full", "running", 10, 4),
	}}
	h := &Handler{MediaRoot: root, Pool: db}
	r := newMemberTestRouter(h, "user-1")

	w := doMemberJob(t, r, jobID)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d，want 200 body=%s", w.Code, w.Body.String())
	}
	var j memberJob
	if err := json.Unmarshal(w.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if j.ID != jobID || j.Status != "running" || j.Total == nil || *j.Total != 10 ||
		j.Processed == nil || *j.Processed != 4 {
		t.Fatalf("任务视图异常: %+v", j)
	}
	if j.Progress == nil || *j.Progress < 0.39 || *j.Progress > 0.41 {
		t.Fatalf("progress=%v，want ~0.4", j.Progress)
	}
}

// 守卫：errRow 的 ErrNoRows 必须能被 errors.Is 识别为 pgx.ErrNoRows（404 分支依赖）。
func TestErrRowIsPgxNoRows(t *testing.T) {
	if !errors.Is(errRow(pgx.ErrNoRows).Scan(), pgx.ErrNoRows) {
		t.Fatal("errRow 未透传 pgx.ErrNoRows")
	}
}

// 占位引用 os（部分平台 tempdir 清理路径需要），防止 import 漂移。
var _ = os.TempDir
