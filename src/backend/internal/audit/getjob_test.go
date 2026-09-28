package audit

import (
	"net/http"
	"strings"
	"testing"
)

// GetJob：契约 §12 的 `GET /admin/jobs/:id`（此前只是契约里的悬空引用）。
//
// 三条断言各自守一件事：
//   - 命中 → 200 且返回该条；
//   - 未命中 → **404**（不是 200 空对象，也不是 500 —— "查不到"是正常结果）；
//   - id 非法 UUID → **400**（若直接进 SQL，`$1::uuid` 转换会抛错落进 500，
//     把"调用方传错 id"伪装成"服务故障"）。

const testJobID = "11111111-2222-4333-8444-555555555555"

func TestGetJobFound(t *testing.T) {
	fs := &fakeStore{jobs: []Job{
		{JobType: "index", ID: testJobID, Kind: "reindex", Status: "done"},
		{JobType: "transcode", ID: "99999999-8888-4777-8666-555555555555", Kind: "hls", Status: "failed"},
	}}
	r := newTestRouter(&Handler{Store: fs})

	w, body := doGet(t, r, "/admin/jobs/"+testJobID)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d，期望 200：%s", w.Code, w.Body.String())
	}
	if body["id"] != testJobID {
		t.Fatalf("应返回请求的那一条，实际 %v", body["id"])
	}
	if body["job_type"] != "index" {
		t.Fatalf("job_type 判别列应保留，实际 %v", body["job_type"])
	}
	if got := fs.jobID(); got != testJobID {
		t.Fatalf("store 应收到该 id，实际 %q", got)
	}
}

func TestGetJobNotFound(t *testing.T) {
	fs := &fakeStore{jobs: []Job{{JobType: "index", ID: testJobID, Status: "done"}}}
	r := newTestRouter(&Handler{Store: fs})

	w, body := doGet(t, r, "/admin/jobs/00000000-0000-4000-8000-000000000000")
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码 %d，期望 404：%s", w.Code, w.Body.String())
	}
	if body["error"] == nil {
		t.Fatalf("404 应带错误包络：%s", w.Body.String())
	}
}

func TestGetJobRejectsMalformedID(t *testing.T) {
	r := newTestRouter(&Handler{Store: &fakeStore{}})
	// 非法 UUID 必须在进 SQL 之前被挡住：否则 `$1::uuid` 转换报错 → 500，
	// 把"调用方传错 id"伪装成"服务故障"。
	//
	// 注：刻意不用 "../etc/passwd" 这类含斜杠的输入 —— httptest/HTTP 客户端会在**到达 handler 之前**
	// 就把路径规范化掉（实测落到 404 page not found），那样测的是路由而不是本 handler 的校验。
	for _, bad := range []string{"abc", "123", "11111111-2222-4333-8444-55555555555", "not-a-uuid"} {
		w, body := doGet(t, r, "/admin/jobs/"+bad)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("id=%q 状态码 %d，期望 400：%s", bad, w.Code, w.Body.String())
		}
		if errObj, ok := body["error"].(map[string]any); !ok || errObj["code"] != CodeInvalidInput {
			t.Fatalf("id=%q 应返回 %s：%s", bad, CodeInvalidInput, w.Body.String())
		}
	}
}

func TestGetJobStoreErrorMapsTo500(t *testing.T) {
	r := newTestRouter(&Handler{Store: &fakeStore{jobsErr: errBoom}})
	w, body := doGet(t, r, "/admin/jobs/"+testJobID)
	assertErrorBody(t, w, body, http.StatusInternalServerError, CodeInternal)
}

// TestJobByIDSQLSharesShapeWithList 单条查询与列表查询必须共用同一套列形状。
//
// 14 列的 UNION 写错一列或错一个类型只会在**运行时**报错（不是编译期），
// 而两处各写一份必然漂移 —— 故实现上共用常量，这里再断言一次结果。
func TestJobByIDSQLSharesShapeWithList(t *testing.T) {
	byID := JobByIDSQL()

	// 两支都要有 id 过滤，且共用同一个占位符编号（同一参数只传一次）
	if n := strings.Count(byID, "WHERE id = $1::uuid"); n != 2 {
		t.Fatalf("两个分支应各出现一次 WHERE id = $1::uuid，实际 %d 次：\n%s", n, byID)
	}
	if !strings.Contains(byID, "UNION ALL") {
		t.Fatalf("应同时查两张表：\n%s", byID)
	}
	if !strings.Contains(byID, "LIMIT 1") {
		t.Fatalf("单条查询应 LIMIT 1：\n%s", byID)
	}

	// 列形状：与列表查询用同一段分支文本
	list, _ := BuildJobsSQL(JobQuery{JobType: "index"})
	if !strings.Contains(list, indexJobBranch) || !strings.Contains(byID, indexJobBranch) {
		t.Fatal("列表与单条查询必须共用同一段 index 分支文本（否则会各自漂移）")
	}
	listTr, _ := BuildJobsSQL(JobQuery{JobType: "transcode"})
	if !strings.Contains(listTr, transcodeJobBranch) || !strings.Contains(byID, transcodeJobBranch) {
		t.Fatal("列表与单条查询必须共用同一段 transcode 分支文本")
	}

	// 每支 14 列（顶层逗号数 + 1）。必须先剥掉外层 `SELECT * FROM ( ... ) j` 包装，
	// 否则 `strings.Index(branch, "FROM")` 会命中**外层**的 FROM，列数被算成 1。
	inner := byID[strings.Index(byID, "(")+1 : strings.LastIndex(byID, ") j")]
	for _, branch := range strings.Split(inner, "UNION ALL") {
		sel := branch[strings.Index(branch, "SELECT"):strings.Index(branch, "FROM")]
		if cols := strings.Count(sel, ",") + 1; cols != 15 {
			t.Fatalf("分支列数应为 15（Job000132 加 current_file），实际 %d：\n%s", cols, sel)
		}
	}
}
