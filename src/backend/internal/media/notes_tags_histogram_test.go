package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Job000005 单测：PATCH notes 字段白名单 / 标签名校验 / histogram 粒度校验 / 归属判定。
// 均覆盖不触库的校验分支（Store 为 nil 时校验须先于 DB 调用）。

func doReq(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1"); c.Set("role", "member") })
	r.PATCH("/media/:id", h.Patch)
	r.POST("/media/:id/tags", h.AddTag)
	r.GET("/media/date-histogram", h.DateHistogram)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

// ---- PATCH /media/:id 字段白名单 ----

func TestPatchNotesFieldWhitelist(t *testing.T) {
	h := &Handler{} // Store 为 nil：非法 body 应在触库前 400
	// 非 notes 字段拒收
	if rec := doReq(h, http.MethodPatch, "/media/m1", `{"filename":"x"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	// 缺 notes 字段
	if rec := doReq(h, http.MethodPatch, "/media/m1", `{}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("缺 notes 应 400，实际 %d", rec.Code)
	}
	// 非法 JSON
	if rec := doReq(h, http.MethodPatch, "/media/m1", `{oops`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，实际 %d", rec.Code)
	}
}

// ---- 归属判定：他人媒体拒绝 ----

func TestCanAccessOwnership(t *testing.T) {
	if canAccess("u-other", "member", "u1") {
		t.Fatal("他人媒体 + member 角色应拒绝")
	}
	if !canAccess("u1", "member", "u1") {
		t.Fatal("本人媒体应放行")
	}
	if !canAccess("u-other", "owner", "u1") || !canAccess("u-other", "admin", "u1") {
		t.Fatal("owner/admin 角色应放行")
	}
}

// ---- 标签名校验 ----

func TestNormalizeTagName(t *testing.T) {
	if got, err := NormalizeTagName("  西湖  "); err != nil || got != "西湖" {
		t.Fatalf("应去首尾空白，实际 %q, %v", got, err)
	}
	if _, err := NormalizeTagName("   "); err == nil {
		t.Fatal("空白名应拒绝")
	}
	if _, err := NormalizeTagName(strings.Repeat("长", 129)); err == nil {
		t.Fatal("超 128 字符应拒绝")
	}
	if _, err := NormalizeTagName(strings.Repeat("a", 128)); err != nil {
		t.Fatalf("128 字符应合法: %v", err)
	}
}

func TestAddTagBadRequest(t *testing.T) {
	h := &Handler{} // Store 为 nil：校验失败应在触库前 400
	if rec := doReq(h, http.MethodPost, "/media/m1/tags", `{"name":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("空标签名应 400，实际 %d", rec.Code)
	}
	if rec := doReq(h, http.MethodPost, "/media/m1/tags", `{bad`); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，实际 %d", rec.Code)
	}
}

// ---- 详情 tags 序列化：含 kind/color ----

func TestDetailTagsJSONKindColor(t *testing.T) {
	color := "#ff8800"
	d := Detail{Tags: []DetailTagRef{
		{ID: "t1", Name: "晚霞", Kind: "user", Color: &color},
		{ID: "t2", Name: "天空", Kind: "ai"},
	}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var out struct {
		Tags []map[string]any `json:"tags"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if len(out.Tags) != 2 {
		t.Fatalf("应有 2 个标签，实际 %d", len(out.Tags))
	}
	if out.Tags[0]["kind"] != "user" || out.Tags[0]["color"] != "#ff8800" {
		t.Fatalf("首标签 kind/color 不符: %v", out.Tags[0])
	}
	if out.Tags[1]["kind"] != "ai" {
		t.Fatalf("次标签 kind 不符: %v", out.Tags[1])
	}
	if _, ok := out.Tags[1]["color"]; ok {
		t.Fatalf("color 为 null 时应省略: %v", out.Tags[1])
	}
}

// ---- 直方图粒度校验 ----

func TestDateHistogramGranularity(t *testing.T) {
	h := &Handler{} // Store 为 nil：粒度非法应在触库前 400
	if rec := doReq(h, http.MethodGet, "/media/date-histogram?granularity=week", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("非法粒度应 400，实际 %d: %s", rec.Code, rec.Body.String())
	}
	// 白名单映射完整性
	for _, g := range []string{"year", "month", "day"} {
		if _, ok := histogramTrunc[g]; !ok {
			t.Fatalf("粒度 %q 应在白名单中", g)
		}
	}
}

// TestDateHistogramDayPassesValidation 守护 handler 的粒度校验与 store 白名单**同源**
// （改动 D：查 histogramTrunc 而非硬编码 year||month）。
//
// 为什么不能用 `&Handler{}`（Store 为 nil）：那种构造下"被校验拒绝"与"被放行后 nil-panic"
// 无法区分——nil-panic 会让整个测试二进制崩掉，而不是给出可读的断言失败。
// 故这里接一个必然连不上的 pool：合法粒度**越过校验进入触库** → 500 QUERY_FAILED；
// 若改动 D 被回退为硬编码，day 会在触库前就返回 400 INVALID_PARAMS。
//
// 变异自证：把 handlers.go 的 `if _, ok := histogramTrunc[granularity]; !ok {`
// 改成 `if granularity != "year" && granularity != "month" {`，本用例必须变红。
func TestDateHistogramDayPassesValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{Store: &Store{Pool: unreachablePool(t)}}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user_id", "u1"); c.Set("role", "member") })
	r.GET("/media/date-histogram", h.DateHistogram)

	for _, g := range []string{"year", "month", "day"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
			"/media/date-histogram?granularity="+g, nil))

		// 判据要具体：越过校验 → 触库失败 → 500 且错误码是 QUERY_FAILED。
		// 缺陷态的签名是 400 + INVALID_PARAMS，正是下面这条断言要区分开的。
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("granularity=%s 应越过校验进入触库（500），实际 %d: %s",
				g, rec.Code, rec.Body.String())
		}
		var body struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("granularity=%s 响应不是错误封套: %v (%s)", g, err, rec.Body.String())
		}
		if body.Error.Code != "QUERY_FAILED" {
			t.Fatalf("granularity=%s 触库失败应回 QUERY_FAILED（证明已越过校验），实际 %q: %s",
				g, body.Error.Code, rec.Body.String())
		}
	}
}
