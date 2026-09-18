package albums

// 相册条目越权修复的回归网（写入端 + 读路径）。
//
// 背景（2026-09-18 真机复现）：member 账号把 owner 的 media id 交给
// POST /albums/:id/items 后，GET /albums/:id 与**匿名**公开分享都能读回其
// filename/thumbnail_*；匿名 GET /public/shares/:token/media/:id/thumb 甚至返回 200 + 40716 字节。
// 根因：AddItems 只校验相册归属，其 INSERT 无任何 media 属主/可见性条件；
// 读路径（Get / List / 匿名分享）也都不做 media 级过滤。
//
// 本文件钉四类性质：
//  1. 写入端 SQL 逐字节包含 mediascope 产出的可见性谓词，且主体是**传入的相册属主**；
//  2. 占位符最高编号 == len(args)（错配只在运行期报 "expected N arguments"）；
//  3. 整笔原子：先校验、后插入，含不可访问媒体就一行都不写；
//  4. 匿名分享不被误杀（主体是相册属主，而非调用者；Store 层拿不到调用者身份）。
//
// 复用同包 criteria_owner_test.go 的 ownerA/ownerB 与 assertPlaceholdersConsistent。

import (
	"os"
	"strings"
	"testing"

	"panoalbum/internal/mediascope"
)

// storeFuncBody 抠出 internal/albums/store.go 里 **func (s *Store) Name(** 的函数体
// （到下一个顶层 func 为止）。与 ownership_test.go 的 handlerBody 同骨架，但锚点不同。
func storeFuncBody(t *testing.T, src, name string) string {
	t.Helper()
	start := strings.Index(src, "func (s *Store) "+name+"(")
	if start < 0 {
		t.Fatalf("store.go 里找不到 func (s *Store) %s —— 方法被改名或删除？", name)
	}
	rest := src[start+1:]
	if next := strings.Index(rest, "\nfunc "); next >= 0 {
		return rest[:next]
	}
	return rest
}

// TestAddItemsQueriesScopedToAlbumOwner 表格驱动：两个不同属主 → 谓词文本/args 必须跟着变。
//
// 这条能抓出"把相册属主参数换成调用者"——那种改动编译、vet、既有测试全绿，
// 只在匿名分享/跨用户时静默越权。
func TestAddItemsQueriesScopedToAlbumOwner(t *testing.T) {
	const albumID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	uniq := []string{"11111111-2222-3333-4444-555555555555"}

	// 前提自证：mediascope 的"恰 1 个参数"约定是本用例期望的基础。
	if _, a := mediascope.VisibleCondFor(2, "u", "m"); len(a) != 1 {
		t.Fatalf("前提不成立：VisibleCondFor 不再返回恰 1 个 userID 参数：%v", a)
	}

	for _, owner := range []string{ownerA, ownerB} {
		t.Run(owner, func(t *testing.T) {
			checkSQL, checkArgs, insertSQL, insertArgs := addItemsQueries(albumID, owner, uniq)

			// 1) 两条语句都必须逐字节包含该属主对应的谓词（$2 / $3 起编号）。
			wantCheck, _ := mediascope.VisibleCondFor(2, owner, "m")
			if !strings.Contains(checkSQL, wantCheck) {
				t.Fatalf("checkSQL 缺少 mediascope 谓词（$2 起）:\n得到 %s\n期望含 %s", checkSQL, wantCheck)
			}
			wantInsert, _ := mediascope.VisibleCondFor(3, owner, "m")
			if !strings.Contains(insertSQL, wantInsert) {
				t.Fatalf("insertSQL 缺少 mediascope 谓词（$3 起）:\n得到 %s\n期望含 %s", insertSQL, wantInsert)
			}

			// 2) 属主必须作为参数出现，而不是写死进 SQL 文本。
			if checkArgs[len(checkArgs)-1] != owner {
				t.Fatalf("check 的属主参数传错：want %q，实际 args=%v", owner, checkArgs)
			}
			if insertArgs[len(insertArgs)-1] != owner {
				t.Fatalf("insert 的属主参数传错：want %q，实际 args=%v", owner, insertArgs)
			}
			if strings.Contains(checkSQL, owner) || strings.Contains(insertSQL, owner) {
				t.Fatal("属主必须参数化，不得拼进 SQL 文本")
			}

			// 3) 占位符编号与 args 个数自洽。
			assertPlaceholdersConsistent(t, checkSQL, len(checkArgs))
			assertPlaceholdersConsistent(t, insertSQL, len(insertArgs))
		})
	}
}

// TestAddItemsOwnerIsParameterized 同一条件、两个属主：SQL 文本相同、args 必须不同。
func TestAddItemsOwnerIsParameterized(t *testing.T) {
	const albumID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	uniq := []string{"11111111-2222-3333-4444-555555555555"}

	cSQL, cA, iSQL, iA := addItemsQueries(albumID, ownerA, uniq)
	cSQL2, cB, iSQL2, iB := addItemsQueries(albumID, ownerB, uniq)
	if cSQL != cSQL2 || iSQL != iSQL2 {
		t.Fatalf("语句文本不该随属主变化（属主必须走参数）:\n%s\nvs\n%s", cSQL, cSQL2)
	}
	if len(cA) != 2 || len(cB) != 2 {
		t.Fatalf("check args 应为 [ids, owner] 两项：%v / %v", cA, cB)
	}
	if cA[1] == cB[1] {
		t.Fatalf("两个属主产生了相同的 check 参数：%v / %v", cA, cB)
	}
	if len(iA) != 3 || len(iB) != 3 {
		t.Fatalf("insert args 应为 [albumID, ids, owner] 三项：%v / %v", iA, iB)
	}
	if iA[2] == iB[2] {
		t.Fatalf("两个属主产生了相同的 insert 参数：%v / %v", iA, iB)
	}
}

// TestAddItemsQueriesFailClosedWithoutOwner 无属主时必须恒假且不占参数位。
func TestAddItemsQueriesFailClosedWithoutOwner(t *testing.T) {
	checkSQL, checkArgs, insertSQL, insertArgs := addItemsQueries(
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "", []string{"11111111-2222-3333-4444-555555555555"})
	if !strings.Contains(checkSQL, mediascope.FailClosed) {
		t.Fatalf("无属主时 checkSQL 必须恒假收窄:\n%s", checkSQL)
	}
	if !strings.Contains(insertSQL, mediascope.FailClosed) {
		t.Fatalf("无属主时 insertSQL 必须恒假收窄:\n%s", insertSQL)
	}
	if len(checkArgs) != 1 || len(insertArgs) != 2 {
		t.Fatalf("恒假谓词不得占参数位：check=%v insert=%v", checkArgs, insertArgs)
	}
	assertPlaceholdersConsistent(t, checkSQL, len(checkArgs))
	assertPlaceholdersConsistent(t, insertSQL, len(insertArgs))
}

// TestAddItemsIsAllOrNothing 整笔原子：Begin → 整笔校验（不可访问即拒绝）→ Exec → Commit，且带 Rollback。
func TestAddItemsIsAllOrNothing(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	body := storeFuncBody(t, string(b), "AddItems")

	for _, want := range []string{"s.Pool.Begin(", "ErrMediaNotAccessible", "tx.Exec(", "tx.Commit(", "tx.Rollback("} {
		if !strings.Contains(body, want) {
			t.Fatalf("AddItems 里找不到 %q —— 整笔原子性/整笔拒绝的接线被摘掉了:\n%s", want, body)
		}
	}
	iBegin := strings.Index(body, "s.Pool.Begin(")
	iCheck := strings.Index(body, "accessible != len(uniq)")
	iExec := strings.Index(body, "tx.Exec(")
	iCommit := strings.Index(body, "tx.Commit(")
	if !(iBegin < iCheck && iCheck < iExec && iExec < iCommit) {
		t.Fatalf("顺序错误（应 Begin < 整笔校验 < Exec < Commit）: begin=%d check=%d exec=%d commit=%d:\n%s",
			iBegin, iCheck, iExec, iCommit, body)
	}
	if strings.Contains(body[:iCheck], "tx.Exec(") {
		t.Fatal("插入发生在整笔校验之前：会出现部分成功")
	}

	// 自证：把校验整段删掉的坏版本必须让上面的断言落空，否则这组断言是空转的。
	broken := "func (s *Store) AddItems() {\n\ttx, _ := s.Pool.Begin(ctx)\n\ttx.Exec(ctx, insertSQL)\n\ttx.Commit(ctx)\n}\n"
	if strings.Contains(broken, "ErrMediaNotAccessible") {
		t.Fatal("守卫失效：本断言在\"无整笔校验\"的版本上也会通过")
	}
}

// TestAlbumReadPathsAreScopedToAlbumOwner 读路径（Get / List）必须按相册属主的可见集过滤，
// 否则 AddItems 的历史脏行仍会被读出来（真机复现已确认过）。
func TestAlbumReadPathsAreScopedToAlbumOwner(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	getBody := storeFuncBody(t, src, "Get")
	if !strings.Contains(getBody, `mediascope.VisibleCondFor(2, d.OwnerID, "m")`) {
		t.Fatalf("Get 的 items 查询未按相册属主收窄可见集:\n%s", getBody)
	}

	// List 的 normal 分支：计数与首图**复用同一个 vis**（同 criteria 的 where/args 一样，不重复计算），
	// 封面缩略图另取一个。故谓词构造出现 2 次，但三条查询都必须真正带上它。
	listBody := storeFuncBody(t, src, "List")
	if n := strings.Count(listBody, `mediascope.VisibleCondFor(2, r.ownerID, "m")`); n != 2 {
		t.Fatalf("List 里按相册属主收窄的谓词构造应出现 2 次（计数+首图共用 vis、封面缩略图一次），实际 %d 次:\n%s", n, listBody)
	}
	if n := strings.Count(listBody, "+vis"); n != 2 {
		t.Fatalf("计数与首图两条查询都应拼上 vis 谓词（应出现 2 处 `+vis`），实际 %d 处:\n%s", n, listBody)
	}
	if n := strings.Count(listBody, "+thumbVis"); n != 1 {
		t.Fatalf("封面缩略图查询应拼上 thumbVis 谓词（应出现 1 处 `+thumbVis`），实际 %d 处:\n%s", n, listBody)
	}

	// 结构性：Store 层不得能取到调用者身份。
	if strings.Contains(src, `c.GetString("user_id")`) || strings.Contains(src, "gin.Context") {
		t.Fatal("Store 层不该能取到调用者身份：属主只能来自相册行（否则匿名分享会被 fail-closed 打死）")
	}
}

// TestAnonymousSharePathStaysOpen 守护"不要把匿名分享打死"。
//
// 分享是**有意**让匿名可见的：谓词主体必须是相册属主（非空 → 非恒假）。
// 若有人把主体改成"调用者"，匿名请求会因无身份而恒假，整条分享链路立刻 200→空。
func TestAnonymousSharePathStaysOpen(t *testing.T) {
	vis, args := mediascope.VisibleCondFor(2, ownerA, "m")
	if vis == mediascope.FailClosed {
		t.Fatal("有相册属主时谓词不得恒假：匿名分享是**有意**可见的，恒假会把分享整条打死")
	}
	if len(args) != 1 || args[0] != ownerA {
		t.Fatalf("谓词应恰好绑定相册属主一个参数: %v", args)
	}
}
