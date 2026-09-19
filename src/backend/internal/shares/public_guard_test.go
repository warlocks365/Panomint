package shares

// 公开分享端点本轮修复（P2-05/06/07/18/19 + P1-04 配合确认）的回归网。
// 与 internal/albums 同一处境：handler 测试没有可用数据库，
// 能真实走 HTTP 的（校验在触库之前）走真实往返，其余用源码形状断言 + 坏版本自证。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func shareReqCtx(method, target string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, rec
}

// ---- P2-05：X-Share-Password 头优先，?password= 保留兼容 ----

func TestSharePasswordHeaderPreferred(t *testing.T) {
	cases := []struct {
		name, target, header, want string
	}{
		{"仅头", "/public/shares/t", "s3cret", "s3cret"},
		{"仅查询串（兼容）", "/public/shares/t?password=legacy", "", "legacy"},
		{"头优先于查询串", "/public/shares/t?password=legacy", "s3cret", "s3cret"},
		{"都为空", "/public/shares/t", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := shareReqCtx(http.MethodGet, tc.target)
			if tc.header != "" {
				c.Request.Header.Set("X-Share-Password", tc.header)
			}
			if got := sharePassword(c); got != tc.want {
				t.Fatalf("sharePassword() = %q, want %q", got, tc.want)
			}
		})
	}

	// guardPublic 必须经 sharePassword 取值，不得再直接 c.Query("password")。
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, "checkAccess(sh, sharePassword(c), time.Now())") {
		t.Fatal("guardPublic 未走 sharePassword：X-Share-Password 头不会生效")
	}
	if strings.Contains(body, `checkAccess(sh, c.Query("password")`) {
		t.Fatal("guardPublic 仍只读 ?password= 查询串")
	}
}

// ---- P2-06：Create 对非 UUID target_id 返回 400 INVALID_PARAMS ----

// 校验在触库之前，因此 Handler.Store 为 nil 也能走真实 HTTP 往返。
func TestCreateRejectsMalformedTargetID(t *testing.T) {
	h := &Handler{}
	for _, id := range []string{"not-a-uuid", "123", "..%2f.."} {
		c, rec := shareReqCtx(http.MethodPost, "/shares")
		c.Request = httptest.NewRequest(http.MethodPost, "/shares",
			strings.NewReader(`{"kind":"album","target_id":"`+id+`"}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "11111111-1111-1111-1111-111111111111")
		c.Set("role", "member")
		h.Create(c)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("target_id=%q：畸形 id 必须 400 而不是 500，实际 %d: %s", id, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "INVALID_PARAMS") {
			t.Fatalf("target_id=%q：错误码应为 INVALID_PARAMS: %s", id, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "22P02") || strings.Contains(rec.Body.String(), "invalid input") {
			t.Fatalf("target_id=%q：响应泄漏 PG 原文: %s", id, rec.Body.String())
		}
	}

	// 合法 UUID 必须能通过格式校验（否则会误拦正常创建；后续触库不在本用例范围）。
	if !uuidRe.MatchString("11111111-1111-1111-1111-111111111111") {
		t.Fatal("uuidRe 不匹配合法 UUID —— 会误拦正常创建")
	}
}

// ---- P2-07：PublicGet 的 ErrTargetLost → 404「分享目标已删除」----

func TestPublicGetMapsTargetLostTo404(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *Handler) PublicGet(")
	if start < 0 {
		t.Fatal("handlers.go 里找不到 PublicGet")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end]
	}
	iLost := strings.Index(body, "errors.Is(err, ErrTargetLost)")
	iList := strings.Index(body, "h.Store.ListItems(")
	if iList < 0 || iLost < 0 {
		t.Fatalf("PublicGet 缺少 ListItems 或 ErrTargetLost 判定:\n%s", body)
	}
	if iLost < iList {
		t.Fatal("ErrTargetLost 判定排在 ListItems 之前 —— 那永远拦不到")
	}
	if !strings.Contains(body[iLost:], `errResp(c, http.StatusNotFound, "NOT_FOUND", "分享目标已删除")`) {
		t.Fatal("ErrTargetLost 必须映射为 404「分享目标已删除」，不得落成 QUERY_FAILED 500")
	}

	// 自证：修复前的坏版本（直接 500）必须让断言落空。
	broken := "items, err := h.Store.ListItems(ctx, sh)\n\tif err != nil {\n\t\thttperr.Fail(c, 500, \"QUERY_FAILED\", \"查询失败\", err)\n\t}"
	if strings.Contains(broken, "ErrTargetLost") {
		t.Fatal("守卫失效：无映射的版本也能通过断言")
	}
}

// ---- P2-19：PublicThumb 文件缺失返回统一 JSON FILE_MISSING ----

func TestPublicThumbFileMissingIsJSON(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (h *Handler) PublicThumb(")
	if start < 0 {
		t.Fatal("handlers.go 里找不到 PublicThumb")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end]
	}
	iStat := strings.Index(body, "os.Stat(path)")
	iFile := strings.Index(body, "c.File(path)")
	if iStat < 0 || iFile < 0 {
		t.Fatalf("PublicThumb 缺 os.Stat 前置或 c.File:\n%s", body)
	}
	if iStat > iFile {
		t.Fatal("os.Stat 必须排在 c.File 之前，否则文件缺失仍回落 net/http 纯文本 404")
	}
	if !strings.Contains(body, `errResp(c, http.StatusNotFound, "FILE_MISSING", "文件不在磁盘上")`) {
		t.Fatal("文件缺失必须返回统一 JSON FILE_MISSING（与 internal/media/thumb.go 对齐）")
	}
}

// ---- P2-18：THUMB_DIR 默认值统一为 ./data/thumbnails ----

func TestThumbDirDefaultIsDataDir(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	if strings.Contains(src, "./testdata/thumbnails") {
		t.Fatal("THUMB_DIR 默认值仍是 ./testdata/thumbnails —— 与 gen 工具链不指向同一目录")
	}
	if !strings.Contains(src, `dir = "./data/thumbnails"`) {
		t.Fatal("THUMB_DIR 默认值应为 ./data/thumbnails（与其他二进制统一）")
	}
}

// ---- P1-04 配合确认：shares.ListItems 的相册分支与 albums.Get 同一路径 ----

// 相册条目硬上限在 albums.Store.Get 内生效；ListItems 必须继续经它取条目，
// 公开分享链路才自动吃到同一个 5000 上限（不得在 shares 侧另抄一份查询绕开）。
func TestListItemsGoesThroughAlbumsGet(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (s *Store) ListItems(")
	if start < 0 {
		t.Fatal("store.go 里找不到 ListItems")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end]
	}
	if !strings.Contains(body, "s.Albums.Get(") {
		t.Fatal("ListItems 的相册分支未走 albums.Store.Get：公开分享会绕开条目硬上限")
	}
}

// ---- 收尾项：PublicThumb / PublicHLS 的 ErrTargetLost 同样映射 404 ----
//
// P2-07 只修了 PublicGet；MediaInShare 在分享目标（相册）已删除时同样会返回
// ErrTargetLost，PublicThumb/PublicHLS 若不做同款映射，访客点开已删相册分享的
// 缩略图/HLS 链接会得到 500 而非 404。此处钉住三端点口径一致。

func TestPublicThumbAndHLSMapTargetLostTo404(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	for _, fn := range []string{"PublicThumb", "PublicHLS"} {
		start := strings.Index(src, "func (h *Handler) "+fn+"(")
		if start < 0 {
			t.Fatalf("handlers.go 里找不到 %s", fn)
		}
		rest := src[start+1:]
		end := strings.Index(rest, "\nfunc ")
		body := rest
		if end >= 0 {
			body = rest[:end]
		}
		iLost := strings.Index(body, "errors.Is(err, ErrTargetLost)")
		iInShare := strings.Index(body, "h.Store.MediaInShare(")
		if iInShare < 0 || iLost < 0 {
			t.Fatalf("%s 缺少 MediaInShare 或 ErrTargetLost 判定:\n%s", fn, body)
		}
		if iLost < iInShare {
			t.Fatalf("%s 的 ErrTargetLost 判定排在 MediaInShare 之前 —— 那永远拦不到", fn)
		}
		if !strings.Contains(body[iLost:], `errResp(c, http.StatusNotFound, "NOT_FOUND", "分享目标已删除")`) {
			t.Fatalf("%s 的 ErrTargetLost 必须映射为 404「分享目标已删除」，不得落成 QUERY_FAILED 500", fn)
		}
	}
}
