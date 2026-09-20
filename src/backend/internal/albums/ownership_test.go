package albums

// GET /albums/:id 的归属校验 + GET /albums 的 favorites 收窄：回归网。
//
// ⚠️ 环境限制先写在最前面，免得把下面的静态断言误当成端到端保证：
// 本仓库的 handler 测试**没有可用数据库**（既有用例统一用 unreachablePool 只测"在触库前就被拦掉"
// 的分支），而 Store 的 Pool 是 *pgxpool.Pool 具体类型、没有接口可以塞替身，所以
// "非 owner 请求 GET /albums/:id 真的返回 404"**无法**用一次真实 HTTP 往返证明。
// 因此这里用三层互补的断言，任何一层被削弱都会红：
//  1. canManage 谓词本身（纯函数，可真实调用）：非本人且非 owner/admin → false；
//  2. Get 的**接线形状**（读源码）：必须经过 getAlbumMeta + canManage，拒绝分支必须是 404 NOT_FOUND
//     且排在 200 之前、排在 Store.Get 之前 —— 并附"把校验删掉"的坏版本自证；
//  3. List 的 SQL 文本：favorites 不得再成为可见性条件。

import (
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func testCtx(userID, role string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("user_id", userID)
	c.Set("role", role)
	return c
}

// ---- 1. 谓词本身 ----

// TestCanManageOnlyOwnerOrAdminRole 钉住 Patch/Delete/Get 共用的归属谓词。
func TestCanManageOnlyOwnerOrAdminRole(t *testing.T) {
	cases := []struct {
		name, user, role string
		want             bool
	}{
		{"本人", ownerA, "viewer", true},
		{"owner 角色（非本人）", ownerB, "owner", true},
		{"admin 角色（非本人）", ownerB, "admin", true},
		{"他人 viewer", ownerB, "viewer", false},
		{"他人 member", ownerB, "member", false},
		{"无主体", "", "viewer", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManage(testCtx(tc.user, tc.role), ownerA); got != tc.want {
				t.Fatalf("canManage(user=%q role=%q, owner=%q) = %v，want %v",
					tc.user, tc.role, ownerA, got, tc.want)
			}
		})
	}
}

// ---- 2. Get 的接线形状 ----

// handlerBody 抠出某个 Handler 方法的函数体（到下一个顶层 func 为止）。
func handlerBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func (h *Handler) "+name+"(")
	if start < 0 {
		t.Fatalf("源码里找不到 func (h *Handler) %s —— 方法被改名或删除？", name)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

func TestGetHandlerIsOwnershipGuarded(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	body := handlerBody(t, src, "Get")

	// 前提自证：Patch 是本文件里既有的正确写法；先证明"抠函数体"这件事本身有效。
	if !strings.Contains(handlerBody(t, src, "Patch"), "canManage(c, ownerID)") {
		t.Fatal("前提失效：Patch 里找不到 canManage —— 既有基准写法变了，本用例需要重写")
	}

	for _, want := range []string{"h.Store.getAlbumMeta(", "canManage(c, ownerID)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("Get 里找不到 %q：GET /albums/:id 必须复用 Patch/Delete 那套 helper"+
				"（getAlbumMeta 取属主 + canManage 判定），不要另造一套。\n%s", want, body)
		}
	}

	iManage := strings.Index(body, "if !canManage(c, ownerID)")
	if iManage < 0 {
		t.Fatalf("Get 里找不到 `if !canManage(c, ownerID)` 判定:\n%s", body)
	}
	tail := body[iManage:]
	deny := regexp.MustCompile(`errResp\(c,\s*http\.StatusNotFound,\s*"NOT_FOUND"`).FindStringIndex(tail)
	if deny == nil {
		t.Fatalf("canManage 之后没有 404 NOT_FOUND 拒绝分支（无权必须返回 404）:\n%s", body)
	}
	iOK := strings.Index(tail, "c.JSON(http.StatusOK, d)")
	if iOK < 0 {
		t.Fatalf("Get 里找不到 200 响应 c.JSON(http.StatusOK, d):\n%s", body)
	}
	if deny[0] > iOK {
		t.Fatal("拒绝分支排在 200 响应之后 —— 那等于没设防")
	}
	if strings.Contains(body, "http.StatusForbidden") {
		t.Fatal("Get 不得返回 403：403 会告诉调用方\"该相册存在但不属于你\"，" +
			"成为可枚举的探测判据；无权应与\"相册不存在\"同形状（404 NOT_FOUND）")
	}
	if iGet := strings.Index(body, "h.Store.Get("); iGet >= 0 && iGet < iManage {
		t.Fatal("归属校验排在 Store.Get 之后：无权请求不该有机会触发 smart 相册的 criteria 全表查询")
	}

	// 自证：把校验整段删掉的坏版本必须让上面的断言落空，否则这组断言是空转的。
	broken := handlerBody(t, "func (h *Handler) Get(c *gin.Context) {\n"+
		"\td, err := h.Store.Get(c.Request.Context(), c.Param(\"id\"))\n"+
		"\t_ = err\n\tc.JSON(http.StatusOK, d)\n}\n", "Get")
	if strings.Contains(broken, "canManage") || strings.Contains(broken, "getAlbumMeta") {
		t.Fatal("守卫失效：本断言在\"无归属校验\"的版本上也会通过，拦不住回归")
	}
}

// ---- 3. 评论子资源的归属校验（P0-01 负对照）----

// assertOwnershipGuardShape 钉住某个 handler 的归属校验接线形状：
// getAlbumMeta 取属主 → canManage 判定 → 404 NOT_FOUND 拒绝（在 Store 调用之前，无 403）。
// storeCall 是"真正的业务调用"标志串，拒绝分支必须排在它之前。
func assertOwnershipGuardShape(t *testing.T, src, handler, storeCall string) {
	t.Helper()
	body := handlerBody(t, src, handler)

	for _, want := range []string{"h.Store.getAlbumMeta(", "canManage(c, ownerID)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s 里找不到 %q：评论子资源必须复用 Get 那套 helper"+
				"（getAlbumMeta 取属主 + canManage 判定），不要另造一套。\n%s", handler, want, body)
		}
	}
	iManage := strings.Index(body, "if !canManage(c, ownerID)")
	if iManage < 0 {
		t.Fatalf("%s 里找不到 `if !canManage(c, ownerID)` 判定:\n%s", handler, body)
	}
	tail := body[iManage:]
	deny := regexp.MustCompile(`errResp\(c,\s*http\.StatusNotFound,\s*"NOT_FOUND"`).FindStringIndex(tail)
	if deny == nil {
		t.Fatalf("%s：canManage 之后没有 404 NOT_FOUND 拒绝分支（无权必须与「相册不存在」同形）:\n%s", handler, body)
	}
	iCall := strings.Index(body, storeCall)
	if iCall < 0 {
		t.Fatalf("%s 里找不到业务调用 %q:\n%s", handler, storeCall, body)
	}
	if iCall < iManage {
		t.Fatalf("%s：归属校验排在 %s 之后 —— 无权请求不该有机会触库", handler, storeCall)
	}
	if strings.Contains(body, "http.StatusForbidden") {
		t.Fatalf("%s 不得返回 403：403 会告诉调用方「该相册存在但不属于你」，"+
			"成为可枚举的探测判据；无权应与「相册不存在」同形状（404 NOT_FOUND）", handler)
	}
}

// TestListCommentsIsOwnershipGuarded viewer 读他人相册评论 → 404（修复前：任意账号可读
// 任意相册的全部评论，含作者信息，且响应形状直接泄漏相册存在性）。
func TestListCommentsIsOwnershipGuarded(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	assertOwnershipGuardShape(t, src, "ListComments", "h.Store.ListComments(")

	// 自证：修复前的坏版本（直接 ListComments 无任何校验）必须让断言落空。
	broken := handlerBody(t, "func (h *Handler) ListComments(c *gin.Context) {\n"+
		"\tcomments, err := h.Store.ListComments(c.Request.Context(), c.Param(\"id\"))\n"+
		"\t_ = err\n\tc.JSON(http.StatusOK, gin.H{\"comments\": comments})\n}\n", "ListComments")
	if strings.Contains(broken, "canManage") || strings.Contains(broken, "getAlbumMeta") {
		t.Fatal("守卫失效：本断言在「无归属校验」的版本上也会通过，拦不住回归")
	}
}

// TestAddCommentIsOwnershipGuarded viewer 写他人相册评论 → 404（修复前只校验相册存在，
// 任何持 album:write 的账号可往他人相册注入评论）。
func TestAddCommentIsOwnershipGuarded(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	assertOwnershipGuardShape(t, src, "AddComment", "h.Store.AddComment(")

	// 自证：修复前的坏版本（只判存在性、不判归属）必须让断言落空。
	broken := handlerBody(t, "func (h *Handler) AddComment(c *gin.Context) {\n"+
		"\tif _, _, err := h.Store.getAlbumMeta(c.Request.Context(), c.Param(\"id\")); errors.Is(err, ErrNotFound) {\n"+
		"\t\terrResp(c, http.StatusNotFound, \"NOT_FOUND\", \"相册不存在\")\n\t\treturn\n\t}\n"+
		"\tid, err := h.Store.AddComment(c.Request.Context(), c.Param(\"id\"), c.GetString(\"user_id\"), \"x\", nil)\n"+
		"\t_ = err\n\tc.JSON(http.StatusCreated, gin.H{\"id\": id})\n}\n", "AddComment")
	if strings.Contains(broken, "canManage") {
		t.Fatal("守卫失效：只判存在性不判归属的版本也能通过断言，拦不住回归")
	}
}

// TestDeleteCommentAllowsAlbumOwner P2-09：删除判定必须扩为
// 「评论作者 ∪ 相册属主（经 getAlbumMeta）∪ owner/admin 角色」。
// 修复前仅「作者本人或 owner/admin 角色」，属主对自己相册下他人写入的评论没有治理权。
func TestDeleteCommentAllowsAlbumOwner(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	body := handlerBody(t, string(b), "DeleteComment")

	if !strings.Contains(body, "h.Store.getAlbumMeta(") {
		t.Fatal("DeleteComment 未经 getAlbumMeta 取相册属主：属主删自己相册下他人评论会被 403")
	}
	if !strings.Contains(body, "canManage(c, ownerID)") {
		t.Fatal("DeleteComment 未复用 canManage：相册属主/owner/admin 的判定又散了一份实现")
	}
	// 拒绝条件必须是「非作者 且 非可管理」—— 任何一边放行都算有权。
	if !strings.Contains(body, `c.GetString("user_id") != author && !canManage(c, ownerID)`) {
		t.Fatal("DeleteComment 的拒绝条件不是「非作者且非属主/管理员」：判定集被改窄或改宽")
	}
	// 防枚举：拒绝分支不得是 403（存在性预言机），必须与「评论不存在」同形 404。
	if strings.Contains(body, "StatusForbidden") {
		t.Fatal("DeleteComment 拒绝分支返回 403：泄漏「该评论存在但你无权」，必须与不存在同形 404")
	}

	// 自证：修复前的坏版本（只看作者与角色）必须让断言落空。
	broken := handlerBody(t, "func (h *Handler) DeleteComment(c *gin.Context) {\n"+
		"\trole := c.GetString(\"role\")\n"+
		"\tif c.GetString(\"user_id\") != author && role != \"owner\" && role != \"admin\" {\n"+
		"\t\terrResp(c, http.StatusForbidden, \"FORBIDDEN\", \"仅评论本人或管理员可删除\")\n\t\treturn\n\t}\n}\n", "DeleteComment")
	if strings.Contains(broken, "getAlbumMeta") || strings.Contains(broken, "canManage") {
		t.Fatal("守卫失效：本断言在「属主无治理权」的版本上也会通过，拦不住回归")
	}
}

// TestListCommentsDoesNotLeakEmail P0-01 附带：评论列表不得泄漏作者邮箱。
// display_name 为空时只允许固定兜底（'用户'），绝不回落 u.email。
func TestListCommentsDoesNotLeakEmail(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (s *Store) ListComments(")
	if start < 0 {
		t.Fatal("store.go 里找不到 ListComments —— 方法被改名或删除？")
	}
	rest := src[start+1:]
	end := strings.Index(rest, "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end]
	}
	if strings.Contains(body, "u.email") {
		t.Fatal("ListComments 仍在回落 u.email：评论列表会把作者邮箱交给相册访问者")
	}
	if !strings.Contains(body, `'用户'`) {
		t.Fatal("ListComments 缺少 display_name 为空时的固定兜底（'用户'）")
	}
}

// ---- 4. GET /albums 的 favorites 收窄 ----

// whereClauseOf 取 SQL 的 WHERE 段（ORDER BY 之前）。
func whereClauseOf(t *testing.T, sql string) string {
	t.Helper()
	i := strings.Index(strings.ToUpper(sql), "ORDER BY")
	if i < 0 {
		t.Fatalf("SQL 里找不到 ORDER BY，切不出 WHERE 段: %s", sql)
	}
	return sql[:i]
}

func TestListAlbumsSQLScopesToOwner(t *testing.T) {
	// 前提自证用的坏版本 = 本次修复前的写法：OR a.type = 'favorites' 会把全站所有人的
	// 收藏相册列给任意账号。同一组断言必须先把它拦住，否则断言没有意义。
	const broken = `
		SELECT a.id FROM albums a
		WHERE a.owner_id = $1 OR a.type = 'favorites'
		ORDER BY a.type = 'favorites' DESC`

	check := func(sql string) []string {
		where := whereClauseOf(t, sql)
		var problems []string
		if !strings.Contains(where, "a.owner_id = $1") {
			problems = append(problems, "WHERE 缺少 a.owner_id = $1")
		}
		if strings.Contains(where, " OR ") {
			problems = append(problems, "WHERE 里仍有 OR 分支（favorites 的全站敞口又回来了）")
		}
		if strings.Contains(where, "favorites") {
			problems = append(problems, "WHERE 里不该再出现 favorites 条件")
		}
		return problems
	}

	if p := check(broken); len(p) == 0 {
		t.Fatal("守卫失效：修复前的 SQL 竟然通过了断言，这组断言拦不住回归")
	}
	if p := check(listAlbumsSQL); len(p) != 0 {
		t.Fatalf("listAlbumsSQL 未收敛到本人：%v\nSQL: %s", p, listAlbumsSQL)
	}
	// favorites 只允许作为**排序**保留（列表内置顶，与可见性无关）。
	if !strings.Contains(listAlbumsSQL, "ORDER BY a.type = 'favorites' DESC") {
		t.Fatal("ORDER BY 里的 favorites 置顶被误删：那是排序，不是可见性条件")
	}
}
