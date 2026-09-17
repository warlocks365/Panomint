package geo

// 地图四端点（/geo/items、/geo/clusters、/geo/histogram、/geo/places）的可见性守卫。
//
// 背景：baseCond 原先只有 gps / deleted_at / bbox 三个条件，**没有任何属主或空间条件**，
// 而四个端点全部从它派生。实测：一个只持 media:read 的普通账号把 bbox 放大到全世界后，
// /geo/items 返回 owner 的全部 GPS 媒体（含 filename 与精确经纬度），ID 集合哈希与 owner
// 视角完全相同；clusters/histogram/places 同样泄漏。这与 /media 系列的越权事故同形 ——
// "作用域默认为全部"，漏加条件不会报错、只会多返回数据。
//
// 本文件用三种互补方式钉住修复（对应任务的三条验证要求）：
//
//	1. 无身份时 WHERE 必须含**恒假条件**（而不是"根本没有条件"）—— fail-closed 的靶心；
//	2. 可见性谓词必须**逐字节来自 mediascope**（唯一真源），且占位符编号与 len(args) 自洽；
//	3. 四个查询**逐个**断言都把调用者身份接进了 baseCond（只覆盖两个等于另外两个还可能漏）。
//
// 第 3 条用源码形状守卫（与 internal/search/query_visible_test.go 同一手法）：
// 本仓库没有 pgx mock 设施，四个函数都直接吃 *pgxpool.Pool，无法在无数据库时运行查询；
// 因此断言"每个函数体都调用了带身份的 baseCond"是这里能达到的最强证据。

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"panoalbum/internal/mediascope"
)

// testBBox 全世界范围的 bbox —— 正是越权实测时用的那一发。
func testBBox() BBox {
	return BBox{MinLng: -180, MinLat: -90, MaxLng: 180, MaxLat: 90}
}

var geoPHRe = regexp.MustCompile(`\$(\d+)`)

// geoPlaceholders 提取 conds 里出现过的占位符编号（含重复：shared 臂的两个 EXISTS 绑同一编号）。
func geoPlaceholders(conds []string) []int {
	var out []int
	for _, c := range conds {
		for _, m := range geoPHRe.FindAllStringSubmatch(c, -1) {
			n, _ := strconv.Atoi(m[1])
			out = append(out, n)
		}
	}
	return out
}

// geoMaxPlaceholder 返回 conds 里最大的占位符编号（没有则 0）。
func geoMaxPlaceholder(conds []string) int {
	max := 0
	for _, n := range geoPlaceholders(conds) {
		if n > max {
			max = n
		}
	}
	return max
}

// 要求 1：userID 为空时生成的 WHERE 含恒假条件，而不是"根本没有条件"。
func TestBaseCondFailClosedWithoutUser(t *testing.T) {
	conds, args := baseCond(testBBox(), "")
	where := strings.Join(conds, " AND ")

	// 靶心：恒假条件必须是 conds 的**一个元素**（而不是恰好没写条件）。
	found := false
	for _, c := range conds {
		if c == mediascope.FailClosed {
			found = true
		}
	}
	if !found {
		t.Fatalf("无身份时必须显式带上恒假条件 %q（而不是\"根本没有条件\"）：%v",
			mediascope.FailClosed, conds)
	}

	// 无身份时可见性谓词不占参数位（bbox 四元组之外的参数都不该出现）。
	if len(args) != 4 {
		t.Fatalf("无身份时不得为可见性占参数位：args=%v", args)
	}
	if strings.Contains(where, "$5") {
		t.Fatalf("无身份时不应出现 $5：%s", where)
	}

	// 更硬的语义断言：无身份时**不得**出现任何绑定不了主体的可见性臂。
	// （恒假是唯一安全的输出；若返回一段裸的 space='personal' 就是历史事故的同形。）
	for _, bad := range []string{"owner_id", "shared_space", "space ="} {
		if strings.Contains(where, bad) {
			t.Fatalf("无身份时 WHERE 里出现了绑定不了主体的可见性臂 %q：%s", bad, where)
		}
	}

	// 前提自证：本断言不是空转 —— 有身份时同一个 baseCond 就**不该**含恒假。
	if withUser, _ := baseCond(testBBox(), "u-1"); idHasFalse(withUser) {
		t.Fatal("守卫失效：带身份的 baseCond 也产出了恒假条件，说明该断言无法区分两条路径")
	}
}

func idHasFalse(conds []string) bool {
	for _, c := range conds {
		if c == mediascope.FailClosed {
			return true
		}
	}
	return false
}

// 要求 2（前半）：可见性谓词逐字节来自 mediascope 单一真源，且 alias 取 ""（裸列名）。
func TestBaseCondVisibilityComesFromMediascope(t *testing.T) {
	uid := "u-1"
	conds, args := baseCond(testBBox(), uid)

	if len(args) != 5 || args[4] != uid {
		t.Fatalf("可见性应恰占 1 个参数位（$5 = userID）：args=%v", args)
	}

	want, wantArgs := mediascope.VisibleCondFor(5, uid, "")
	// 前提自证：若 mediascope 的"恰 1 个参数"约定变了，本用例的期望也要跟着变。
	if len(wantArgs) != 1 || wantArgs[0] != uid {
		t.Fatalf("前提不成立：VisibleCondFor 不再返回恰 1 个 userID 参数：%v", wantArgs)
	}

	got := conds[len(conds)-1]
	if got != want {
		t.Fatalf("可见性谓词必须逐字节来自 mediascope.VisibleCondFor(..., alias=\"\")：\n得到 %q\n期望 %q", got, want)
	}

	// alias 依据：四处查询都是 `FROM media` 的裸列名单表查询（无 JOIN、无表别名），
	// 故谓词必须输出裸列名；带 m. 前缀在此处会直接是 SQL 错误（column m.space does not exist）。
	//
	// ⚠️ 只查 "m.space"/"m.owner_id" 这两个 media 列，**不能**用 Contains(got, "m.") ——
	// shared 臂的子查询别名 sm./ss. 也含 "m."，那样会误报（本断言最初就这么错过一次）。
	for _, bad := range []string{"m.space", "m.owner_id"} {
		if strings.Contains(got, bad) {
			t.Fatalf("geo 的 FROM media 未起别名，谓词不得出现 %q：%q", bad, got)
		}
	}
	if !strings.Contains(got, "space = 'personal'") || !strings.Contains(got, "owner_id = $5") {
		// 裸列名的正向前提：若谓词带别名，上面两条会因 "m.space = 'personal'" 而失配。
		t.Fatalf("谓词未以裸列名绑定主体（应含 \"space = 'personal'\" 与 \"owner_id = $5\"）：%q", got)
	}
	// 共享臂的两条分支（成员 ∪ 属主）都必须在，漏任一条 = 少看到 / 多看到。
	if !strings.Contains(got, "shared_space_members") || !strings.Contains(got, "shared_space ss") {
		t.Fatalf("共享臂必须在（members OR owner），实际：%q", got)
	}
}

// 要求 2（后半）：占位符编号与 len(args) 自洽 —— 这是运行期 "expected N arguments" 的唯一防线。
func TestGeoWherePlaceholderNumberingSelfConsistent(t *testing.T) {
	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, uid := range []string{"", "u-1"} {
		for _, kind := range []string{KindAll, KindPhoto} {
			for _, useTime := range []bool{false, true} {
				t.Run(fmt.Sprintf("uid=%q/kind=%s/time=%v", uid, kind, useTime), func(t *testing.T) {
					// 按四个查询的真实顺序组装：baseCond → appendTimeRange → appendKind → 尾部参数。
					conds, args := baseCond(testBBox(), uid)
					if useTime {
						conds, args = appendTimeRange(conds, args, &from, &to)
					}
					conds, args = appendKind(conds, args, kind)

					// ① conds 自身的编号必须**紧接着 bbox** 之后：
					//    bbox 占 $1-$4；有身份时可见性占 $5；时间范围再按 len(args) 续编。
					expect := 4
					if uid != "" {
						expect = 5
					}
					if useTime {
						expect += 2
					}
					if got := geoMaxPlaceholder(conds); got != expect {
						t.Fatalf("conds 最高占位符应为 $%d，实际 $%d —— 编号错位只会在运行期报 "+
							"expected N arguments：\nconds=%v\nargs=%v",
							expect, got, conds, args)
					}
					if uid != "" && args[4] != uid {
						t.Fatalf("args[4] 应为可见性谓词的 userID %q，实际 %v", uid, args)
					}

					// ② 再模拟四个查询尾部追加的 LIMIT / 网格尺寸参数（调用方按 len(args) 续编），
					//    断言"最高占位符 == args 个数"，这是 pgx 真正会校验的不变量。
					args = append(args, 200)
					tail := fmt.Sprintf("$%d", len(args))
					phs := geoPlaceholders(append(append([]string{}, conds...), tail))
					max, seen := 0, map[int]bool{}
					for _, n := range phs {
						seen[n] = true
						if n > max {
							max = n
						}
					}
					if max != len(args) {
						t.Fatalf("最高占位符 $%d ≠ args 个数 %d（运行期会报 expected %d arguments）：\nargs=%v\nwhere=%s",
							max, len(args), max, args, strings.Join(conds, " AND "))
					}
					// 编号不得跳号：跳号意味着某段条件被漏拼或编号算错。
					for n := 1; n <= max; n++ {
						if !seen[n] {
							t.Fatalf("占位符 $%d 从未出现，编号有跳号：%v", n, phs)
						}
					}
				})
			}
		}
	}
}

// 要求 3（存储层）：四个查询**逐个**断言都带上了可见性条件（表格驱动，覆盖全部四处）。
func TestAllFourGeoStoreQueriesCarryVisibility(t *testing.T) {
	b, err := os.ReadFile("media_store.go")
	if err != nil {
		t.Fatalf("读不到 media_store.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	// 普查：media_store.go 里恰好 4 处 media 表数据源，且都不是 JOIN ——
	// 出现第 5 处（或改成 JOIN media）时必须同步接上可见性谓词，故这里显式钉住数量。
	// 用"行首两 tab + FROM media + 换行"匹配 SQL 行，避免把注释里提到的 FROM media 算进来。
	if n := strings.Count(src, "\n\t\tFROM media\n"); n != 4 {
		t.Fatalf("media_store.go 里 SQL 的 \"FROM media\" 出现 %d 次，预期 4。"+
			"新增/moved 的 media 查询必须同样接上可见性谓词（见 baseCond）", n)
	}
	if n := strings.Count(src, "JOIN media"); n != 0 {
		t.Fatalf("media_store.go 里出现 %d 处 \"JOIN media\"：请确认新查询也接上了可见性谓词，"+
			"然后更新本断言", n)
	}

	// 前提自证：旧签名 baseCond(b)（无身份）必须已不存在，否则本组断言拦不住回退。
	if strings.Contains(src, "baseCond(b)") {
		t.Fatal("守卫失效/回退：baseCond 仍在「无身份」形式下被调用")
	}

	for _, fn := range []string{"Clusters", "Items", "Histogram", "Places"} {
		t.Run(fn, func(t *testing.T) {
			body := geoFuncBody(src, "func (s *MediaStore) "+fn+"(")
			if body == "" {
				t.Fatalf("找不到 %s 的函数体（接收者或函数名改了？）", fn)
			}
			if !strings.Contains(body, "userID string") {
				t.Fatalf("%s 的签名里没有 userID 参数：调用者身份无从传入", fn)
			}
			if n := strings.Count(body, "baseCond(b, userID)"); n != 1 {
				t.Fatalf("%s 里 \"baseCond(b, userID)\" 出现 %d 次，预期恰好 1 次"+
					"（漏掉即该端点仍返回全站 GPS 媒体）:\n%s", fn, n, body)
			}
		})
	}
}

// 要求 3（HTTP 层）：四个 handler 逐个断言把会话身份传给了 store。
// 漏传 userID 会让 baseCond 收到空串 → 恒假 → 端点变空（fail-closed，不漏数据但功能坏掉），
// 因此这一处也需要被钉住，否则"修好了但不接线"是不可见的。
func TestAllFourGeoHandlersPassIdentity(t *testing.T) {
	b, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("读不到 handlers.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	for _, fn := range []string{"Clusters", "Items", "Histogram", "Places"} {
		t.Run(fn, func(t *testing.T) {
			body := geoFuncBody(src, "func (h *Handler) "+fn+"(")
			if body == "" {
				t.Fatalf("找不到 handler %s 的函数体", fn)
			}
			if !strings.Contains(body, `c.GetString("user_id")`) {
				t.Fatalf("handler %s 未把会话身份传给 store（应传 c.GetString(\"user_id\")）:\n%s", fn, body)
			}
			if !strings.Contains(body, "h.Media."+fn+"(") {
				t.Fatalf("handler %s 未调用 h.Media.%s：接线被摘掉了", fn, fn)
			}
		})
	}
}

// geoFuncBody 取出以 prefix 开头、到下一个顶层 func 声明为止的源码片段。
func geoFuncBody(src, prefix string) string {
	i := strings.Index(src, prefix)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest[1:], "\nfunc "); j >= 0 {
		return rest[:j+1]
	}
	return rest
}
