package albums

// P0-1 相册评论越权修复的**对抗性验证**（独立于 ownership_test.go 的第三视角）。
//
// 攻击模型：
//   A. 非成员/普通 member/viewer 对他人相册的 List/Add/Delete 评论必须全被拒；
//   B. 「相册不存在」「评论不存在」「畸形 id」的响应不得互相区分（防枚举）；
//   C. 权限判定本身不得有旁路（err 未检、canManage 被零值绕过、cid 跨相册替换）；
//   D. 三条评论路由必须挂在 auth 中间件之后。
//
// 已确认的残余观察（不在本文件断言为红，见 DeleteComment 一节的注释）：
//   DeleteComment 对「评论存在但无权」返回 403，与「不存在」的 404 可区分 ——
//   相册+评论的存在性预言机。与 handlers.go:99-100 自己声明的防枚举口径不一致。
//   该问题已上报 team-lead，此处只钉死「拒绝必须先于删除」这一安全关键性质。

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ---- A. canManage 谓词的对抗边界 ----

func advCtx(userID, role string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	if userID != "" {
		c.Set("user_id", userID)
	}
	if role != "" {
		c.Set("role", role)
	}
	return c
}

// TestAdversarialCanManageFailClosed 攻击者控制不了 session 里的 user_id/role，
// 但要钉住：任何"非精确匹配"都不能意外放行（大小写、空白、缺失键）。
func TestAdversarialCanManageFailClosed(t *testing.T) {
	const albumOwner = "user-album-owner"
	cases := []struct {
		name, user, role string
		want             bool
	}{
		{"属主本人", albumOwner, "member", true},
		{"owner 角色", "other", "owner", true},
		{"admin 角色", "other", "admin", true},
		// 以下全部必须拒绝（fail-closed）：
		{"普通 member", "other", "member", false},
		{"viewer", "other", "viewer", false},
		{"role 缺失（未 set）", "other", "", false},
		{"user_id 缺失（未 set）", "", "member", false},
		{"大小写变体 Owner", "other", "Owner", false},
		{"大小写变体 ADMIN", "other", "ADMIN", false},
		{"带空格 ' owner'", "other", " owner", false},
		{"role 为 owner 但 user_id 为空", "", "owner", true}, // role 本身有效即放行：与现有语义一致
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canManage(advCtx(tc.user, tc.role), albumOwner); got != tc.want {
				t.Fatalf("canManage(user=%q role=%q) = %v, want %v", tc.user, tc.role, got, tc.want)
			}
		})
	}
}

// TestAdversarialHandlersCheckLookupErrorBeforeCanManage 旁路检查：
// 若 getAlbumMeta 出错而 handler 没 return 就走到 canManage，ownerID 会是零值 ""，
// 此时攻击者 user_id 若也为 "" 就会被判成"本人"。三个评论 handler 必须在
// getAlbumMeta 与 canManage/删除判定之间存在错误返回分支。
func TestAdversarialHandlersCheckLookupErrorBeforeCanManage(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	src := string(b)
	for _, name := range []string{"ListComments", "AddComment", "DeleteComment"} {
		body := handlerBody(t, src, name)
		iMeta := strings.Index(body, "h.Store.getAlbumMeta(")
		iDeny := strings.Index(body, "canManage(c, ownerID)")
		if iMeta < 0 || iDeny < 0 {
			t.Fatalf("%s: 找不到 getAlbumMeta/canManage 接线", name)
		}
		mid := body[iMeta:iDeny]
		if !strings.Contains(mid, "if err != nil {") || !strings.Contains(mid, "return") {
			t.Fatalf("%s: getAlbumMeta 与权限判定之间没有 err 返回分支 —— "+
				"查库失败时 ownerID 零值会进入 canManage（零值旁路）:\n%s", name, body)
		}
	}
}

// ---- B. 防枚举同形 ----

// TestAdversarialLookupErrorSameShape 畸形 id / 相册不存在 / 无权 三者必须同形 404。
// writeAlbumLookupError 是唯一映射点：22P02 与 ErrNotFound 走同一个 errResp 调用。
func TestAdversarialLookupErrorSameShape(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	src := string(b)
	// writeAlbumLookupError 是包级函数而非方法，不能复用 handlerBody 的方法签名。
	start := strings.Index(src, "func writeAlbumLookupError(")
	if start < 0 {
		t.Fatal("找不到 writeAlbumLookupError —— 被改名或删除？")
	}
	rest := src[start+1:]
	// 以函数收尾的 "\n}\n" 截断：若切到 "\nfunc " 会把下一个函数的 doc 注释
	// （含「相册不存在」字样）算进来，造成误报。
	body := rest
	if end := strings.Index(rest, "\n}\n"); end >= 0 {
		body = rest[:end+3]
	}

	// 两个分支必须合并进**同一个** 404 响应（构造上保证同形，而非两处手抄）。
	if !strings.Contains(body, "errors.Is(err, ErrNotFound) || pgxutil.IsMalformedID(err)") {
		t.Fatalf("writeAlbumLookupError 未把 ErrNotFound 与畸形 id 合并判定:\n%s", body)
	}
	if strings.Count(body, `"相册不存在"`) != 1 {
		t.Fatalf("404 文案出现多次：同形靠手抄维持，迟早漂移:\n%s", body)
	}
	// 500 分支不得回显 err（PG 原文含 SQLSTATE/列名）。
	if strings.Contains(body, "err.Error()") {
		t.Fatalf("writeAlbumLookupError 的 500 分支回显了 err 原文:\n%s", body)
	}

	// List/Add 的查库错误必须全部走这个映射点（不得各自为政）。
	for _, name := range []string{"ListComments", "AddComment"} {
		hb := handlerBody(t, src, name)
		if !strings.Contains(hb, "writeAlbumLookupError(c, err)") {
			t.Fatalf("%s 未经 writeAlbumLookupError 映射查库错误", name)
		}
	}
}

// ---- C. DeleteComment 的跨相册 cid 替换与拒绝次序 ----

// TestAdversarialGetCommentAuthorAlbumScoped 关键防线：GetCommentAuthor 的 SQL
// 必须同时按 id **和** album_id 过滤。若只按 id 过滤，攻击者可以：
//  1. 拿自己相册 A 里一条评论的 cid；
//  2. 对受害者相册 B 调 DeleteComment(B, cid_from_A?) —— 反过来更危险：
//     用自己属主的相册 A 的路径参数 + 受害者相册 B 里的 cid，
//     GetCommentAuthor 若不看 album_id 就会返回受害者为作者，
//     随后 canManage(攻击者, A的属主=攻击者) = true → 删掉受害者相册 B 的评论。
func TestAdversarialGetCommentAuthorAlbumScoped(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go: %v", err)
	}
	src := string(b)
	start := strings.Index(src, "func (s *Store) GetCommentAuthor(")
	if start < 0 {
		t.Fatal("找不到 GetCommentAuthor")
	}
	rest := src[start:]
	end := strings.Index(rest[1:], "\nfunc ")
	body := rest
	if end >= 0 {
		body = rest[:end+1]
	}
	if !strings.Contains(body, "WHERE id = $1 AND album_id = $2") {
		t.Fatalf("GetCommentAuthor 未按 album_id 限定：跨相册 cid 替换可越权删评论:\n%s", body)
	}
	// 自证：不带 album 限定的坏版本必须被上面的断言拦住。
	broken := strings.Replace(body, "WHERE id = $1 AND album_id = $2", "WHERE id = $1", 1)
	if strings.Contains(broken, "WHERE id = $1 AND album_id = $2") {
		t.Fatal("守卫失效：去掉 album 限定的版本也能通过断言")
	}
}

// TestAdversarialDeleteCommentDenyPrecedesDelete 拒绝分支必须排在真实删除之前，
// 且「评论不存在」404 必须先于一切权限判定（否则先 403 会泄漏"评论不存在"）。
func TestAdversarialDeleteCommentDenyPrecedesDelete(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	body := handlerBody(t, string(b), "DeleteComment")

	iNoRows := strings.Index(body, "ErrCommentNoRows")
	iDeny := strings.Index(body, `c.GetString("user_id") != author && !canManage(c, ownerID)`)
	iDel := strings.Index(body, "h.Store.DeleteComment(")
	if iNoRows < 0 || iDeny < 0 || iDel < 0 {
		t.Fatalf("DeleteComment 缺关键环节（404/拒绝/删除）:\n%s", body)
	}
	if !(iNoRows < iDeny && iDeny < iDel) {
		t.Fatalf("DeleteComment 次序错误：必须 404判定 < 权限拒绝 < 真实删除，"+
			"否则要么先删后判、要么泄漏评论存在性:\n%s", body)
	}
	// 删除必须用经 album 限定验证过的 cid（而不是请求里别的字段）。
	if !strings.Contains(body, "h.Store.DeleteComment(c.Request.Context(), cid)") {
		t.Fatalf("DeleteComment 删除的标识不是路径里的 cid（可能被换成请求体字段）:\n%s", body)
	}

	// 自证：把真实删除挪到权限拒绝之前的坏版本，上面的次序断言必须落空。
	broken := body[:iDeny] + "h.Store.DeleteComment(c.Request.Context(), cid)\n\t" + body[iDeny:]
	bNoRows := strings.Index(broken, "ErrCommentNoRows")
	bDeny := strings.Index(broken, `c.GetString("user_id") != author && !canManage(c, ownerID)`)
	bDel := strings.Index(broken, "h.Store.DeleteComment(")
	if bNoRows < bDeny && bDeny < bDel {
		t.Fatal("守卫失效：拒绝在删除之后的版本也能通过次序断言")
	}

	// 残余观察（已上报）：拒绝分支当前是 403 而非 404 —— 对「(album,cid) 存在但无权」
	// 与「不存在」可区分，构成存在性预言机。此处不钉 403（避免固化），只记录。
	if !strings.Contains(body, "http.StatusForbidden") {
		t.Log("DeleteComment 的拒绝已不再是 403 —— 存在性预言机残余已消除")
	}
}

// ---- D. 路由必须在 auth 中间件之后 ----

// TestAdversarialCommentRoutesBehindAuth 三条评论路由必须注册在带 AuthRequired 的
// authed 组上；若被挪到匿名组（或新文件里另开注册点）即红。
func TestAdversarialCommentRoutesBehindAuth(t *testing.T) {
	b, err := os.ReadFile(backendRoot(t) + "/cmd/api/main.go")
	if err != nil {
		t.Fatalf("读不到 main.go: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, `authed := r.Group("", auth.AuthRequired(secret))`) {
		t.Fatal("main.go 里找不到带 AuthRequired 的 authed 组 —— 认证接线变了，本守卫需重写")
	}
	for _, route := range []string{
		`authed.GET("/albums/:id/comments"`,
		`authed.POST("/albums/:id/comments"`,
		`authed.DELETE("/albums/:id/comments/:cid"`,
	} {
		if !strings.Contains(src, route) {
			t.Fatalf("评论路由 %s 不在 authed 组上 —— 匿名可达或被删", route)
		}
	}
	// 反向自证：匿名组注册形态必须匹配不到。
	for _, anon := range []string{
		`r.GET("/albums/:id/comments"`,
		`r.POST("/albums/:id/comments"`,
		`r.DELETE("/albums/:id/comments/:cid"`,
	} {
		if strings.Contains(src, anon) {
			t.Fatalf("发现绕过 authed 组的匿名评论路由注册: %s", anon)
		}
	}
}

// ---- 附带：AddComment 的作者标识必须来自 session，不能来自请求体 ----

func TestAdversarialAddCommentAuthorFromSession(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go: %v", err)
	}
	body := handlerBody(t, string(b), "AddComment")
	if !strings.Contains(body, `h.Store.AddComment(c.Request.Context(), albumID, c.GetString("user_id")`) {
		t.Fatalf("AddComment 的作者标识不是 session 的 user_id —— 请求体可伪造作者:\n%s", body)
	}
	// 请求体 struct 里不得出现 user_id 字段（攻击面封死）。
	if strings.Contains(body, `json:"user_id"`) {
		t.Fatalf("AddComment 请求体暴露了 user_id 字段:\n%s", body)
	}
}
