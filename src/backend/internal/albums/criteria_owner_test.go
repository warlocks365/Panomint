package albums

// 智能相册的属主收敛（本次 IDOR 修复）回归网。
//
// 为什么必须有这组断言：buildCriteriaWhere 的 owner 约束一旦**传错成调用者身份**，
// 编译、vet、以及既有的条件生成单测都不会红 —— 只有匿名分享请求打到 smart 相册详情时
// 才会整条 fail-closed；反过来，条件被悄悄去掉时 SQL 也不会报错、只是多返回别人的照片。
// 所以这里钉三类性质：
//  1. 传谁就用谁：不同 ownerID 产出不同的 args，且 args 里那个值就是传进来的属主；
//  2. 占位符编号与 args 个数自洽（不自洽只在运行期报 "expected N arguments"）；
//  3. 结构性：本包拼 SQL 的地方**拿不到**调用者身份，"属主只能是相册属主"才不是靠约定。

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	ownerA = "11111111-1111-1111-1111-111111111111"
	ownerB = "22222222-2222-2222-2222-222222222222"
)

var placeholderRe = regexp.MustCompile(`\$(\d+)`)

// assertPlaceholdersConsistent 断言 SQL 里的 $k 全部落在 1..nargs，且最大编号恰好是 nargs。
func assertPlaceholdersConsistent(t *testing.T, where string, nargs int) {
	t.Helper()
	ms := placeholderRe.FindAllStringSubmatch(where, -1)
	max := 0
	for _, m := range ms {
		k, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatalf("占位符无法解析: %q", m[1])
		}
		if k < 1 || k > nargs {
			t.Fatalf("占位符 $%d 越界（args 只有 %d 个，错配只在运行期报 expected %d arguments）: %s",
				k, nargs, nargs, where)
		}
		if k > max {
			max = k
		}
	}
	if max != nargs {
		t.Fatalf("占位符最大编号 $%d 与 args 个数 %d 不自洽（运行期会报 expected %d arguments）: %s",
			max, nargs, nargs, where)
	}
}

// TestBuildCriteriaWhereScopedToAlbumOwner 表格驱动：两个不同属主ID 必须产出跟着变的 SQL/args。
func TestBuildCriteriaWhereScopedToAlbumOwner(t *testing.T) {
	cases := []struct {
		name  string
		owner string
		c     *Criteria
	}{
		{"ownerA/无条件", ownerA, nil},
		{"ownerB/无条件", ownerB, nil},
		{"ownerA/photo", ownerA, &Criteria{Type: "photo"}},
		{"ownerB/photo", ownerB, &Criteria{Type: "photo"}},
		{"ownerA/日期", ownerA, &Criteria{DateFrom: "2026-01-01", DateTo: "2026-03-31"}},
		{"ownerB/地点收藏", ownerB, &Criteria{Place: "杭州", Favorites: true}},
		{"ownerA/360", ownerA, &Criteria{Type: "360"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			where, args := buildCriteriaWhere(tc.c, tc.owner)
			// 1) 必须有属主条件，且它的编号就是最后一个占位符
			wantCond := "m.owner_id = $" + strconv.Itoa(len(args))
			if !strings.Contains(where, wantCond) {
				t.Fatalf("WHERE 缺少属主约束 %q: %s", wantCond, where)
			}
			// 2) 该占位符对应的实际参数必须**就是传进来的相册属主**
			if args[len(args)-1] != tc.owner {
				t.Fatalf("属主参数传错：want %q，实际 args=%v", tc.owner, args)
			}
			// 3) 编号自洽
			assertPlaceholdersConsistent(t, where, len(args))
		})
	}
}

// TestBuildCriteriaWhereOwnerIsParameterized 同一条件、两个属主：SQL 文本相同、args 必须不同。
// 这条专门抓"属主被写死/被换成别的身份"这类改动 —— 它不会编译失败，只会静默越权。
func TestBuildCriteriaWhereOwnerIsParameterized(t *testing.T) {
	c := &Criteria{Type: "video"}
	whereA, argsA := buildCriteriaWhere(c, ownerA)
	whereB, argsB := buildCriteriaWhere(c, ownerB)
	if whereA != whereB {
		t.Fatalf("条件文本不该随属主变化（属主必须走参数）: %q vs %q", whereA, whereB)
	}
	if len(argsA) != 2 || len(argsB) != 2 {
		t.Fatalf("args 应为 [type, owner] 两项: %v / %v", argsA, argsB)
	}
	if argsA[1] == argsB[1] {
		t.Fatalf("两个属主产生了相同的参数：%v / %v", argsA, argsB)
	}
	if strings.Contains(whereA, ownerA) || strings.Contains(whereB, ownerB) {
		t.Fatal("属主必须参数化，不得拼进 SQL 文本")
	}
}

// TestCriteriaBuilderCannotSeeCaller 结构性断言：criteria.go 里没有任何途径取到调用者身份。
//
// 这是"属主必须是相册属主"的**结构性**保证：只要能在这里读到 user_id，
// 口径就退化成"靠约定"；读不到，才排除掉"偷偷改用调用者"的改法。
// 同时它也钉住匿名分享安全 —— shares.ListItems 以匿名身份走到这里，函数不依赖任何主体。
func TestCriteriaBuilderCannotSeeCaller(t *testing.T) {
	b, err := os.ReadFile("criteria.go")
	if err != nil {
		t.Fatalf("读不到 criteria.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	if !strings.Contains(src, "func buildCriteriaWhere(c *Criteria, albumOwnerID string)") {
		t.Fatalf("buildCriteriaWhere 的签名应显式接收 albumOwnerID（相册属主）")
	}
	for _, banned := range []string{"gin.Context", "c.GetString", "user_id", "Context"} {
		if strings.Contains(src, banned) {
			t.Fatalf("criteria.go 里出现了 %q：智能相册的属主只能由调用点传入**相册属主**；"+
				"一旦这里能取到调用者身份，匿名分享链路（shares.ListItems，无主体）会被误杀，"+
				"而分享是**有意**让匿名可见的。", banned)
		}
	}
}

// TestCriteriaCallSitesPassAlbumOwner 调用点断言：两处调用都必须传相册属主。
//
// 事实修正（由本用例自身发现）：store.go 里 buildCriteriaWhere 只有 **2 处调用** ——
// List 的智能分支调用一次（store.go:152 附近）拿到 where/args 后，计数与"取首图"两个查询
// **复用同一组** where/args（不是各调一次）；另一处在 Store.Get 的智能分支。
// 所以"三处查询"共享两次调用，属主参数一处传错就同时影响两个查询。
//
// 传成"调用者 userID"时这里会红 —— 那正是匿名分享被打死、且语义被偷换的错法。
func TestCriteriaCallSitesPassAlbumOwner(t *testing.T) {
	b, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatalf("读不到 store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)
	calls := regexp.MustCompile(`buildCriteriaWhere\(([^)]*)\)`).FindAllStringSubmatch(src, -1)
	if len(calls) != 2 {
		t.Fatalf("buildCriteriaWhere 调用点应为 2 处（List 智能分支、Get 智能分支），实际 %d 处 —— "+
			"若有人把取首图查询改成再调一次，请连同本断言一起复核参数是否仍传相册属主；"+
			"若减少到 0/1 处，说明智能相册的属主收敛被绕过了", len(calls))
	}
	for _, m := range calls {
		parts := strings.Split(m[1], ",")
		if len(parts) != 2 {
			t.Fatalf("调用点应传 2 个参数（条件 + 相册属主），实际 %q", m[1])
		}
		if got := strings.TrimSpace(parts[1]); got != "r.ownerID" && got != "d.OwnerID" {
			t.Fatalf("第二个参数应为相册属主（r.ownerID / d.OwnerID），实际 %q —— "+
				"若传成调用者身份，匿名分享链路会被 fail-closed 整条打死", got)
		}
	}
	// List 的智能分支必须把同一组 where/args 用在计数与首图两个查询上（没有第二处调用即已保证）。
	if n := strings.Count(src, "SELECT count(*)::int FROM media m WHERE "); n != 1 {
		t.Fatalf("智能相册计数查询应为 1 处，实际 %d 处", n)
	}
	if strings.Contains(src, `c.GetString("user_id")`) {
		t.Fatal(`store.go 不该出现 c.GetString("user_id")：Store 层没有 gin 上下文，属主必须来自相册行`)
	}
}
