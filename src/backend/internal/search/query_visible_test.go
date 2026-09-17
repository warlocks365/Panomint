package search

import (
	"os"
	"strings"
	"testing"

	"panoalbum/internal/mediascope"
)

// 可见性谓词「单一真源」的两道守卫。
//
// 背景：internal/search/query.go 曾把 /media 系列的授权谓词**第二次手写**，且手写版本
// 只判 shared_space_members、漏掉 shared_space.owner_id 臂（见 internal/media/scope.go
// 原先登记的差异）。本文件用两种互补方式钉住收敛：
//
//  1. 行为侧：search 侧最终 WHERE 里的可见性谓词必须与 mediascope.Conds 的对应臂
//     **逐字节相同**（即同一段代码产出，不是"看起来差不多"）；
//  2. 源码形状侧：query.go 里不得再出现手写谓词的标志串 shared_space_members
//     （与 internal/media/audit_wiring_test.go 同一手法：读源码文本做断言）。
//
// 两条守卫都自带**前提自证**，且都实测过"把 query.go 改回手写版本时会 FAIL"。

func TestSearchVisibilityComesFromMediascope(t *testing.T) {
	where, _, _, args, whereN := buildWhere(SearchParams{UserID: "u1"}, nil, nil)

	sharedConds, _ := mediascope.Conds(mediascope.Scope{Space: "shared", MemberID: "u1"})
	sharedArm := sharedConds[1]
	if !strings.Contains(where, sharedArm) {
		t.Fatalf("搜索侧可见性谓词必须与 mediascope.Conds 的 shared 臂逐字节相同（同一段代码产出）。\n"+
			"缺少的臂 = %q\n实际 WHERE = %s", sharedArm, where)
	}
	persConds, _ := mediascope.Conds(mediascope.Scope{Space: "personal", OwnerID: "u1"})
	if !strings.Contains(where, persConds[1]) {
		t.Fatalf("personal 臂也必须来自同一真源（%q）: %s", persConds[1], where)
	}
	if len(args) != 1 || args[0] != "u1" || whereN != 1 {
		t.Fatalf("可见性只应占 1 个参数位（$1 = u1）：args=%v whereN=%d", args, whereN)
	}

	// 前提自证：改回"手写版本"（只判 shared_space_members、无属主臂）时本断言必须失败，
	// 否则这个守卫是空转的、拦不住回归。这里用旧实现的原文做自证。
	handwritten := "((m.space = 'personal' AND m.owner_id = $1)\n" +
		"\t\tOR (m.space = 'shared' AND EXISTS(\n" +
		"\t\t\tSELECT 1 FROM shared_space_members sm WHERE sm.user_id = $1)))"
	if strings.Contains(handwritten, sharedArm) {
		t.Fatal("守卫失效：手写旧谓词也包含了 shared 臂，本断言拦不住回归")
	}
	if !strings.Contains(handwritten, "m.space = 'personal' AND m.owner_id = $1") {
		t.Fatal("守卫失效：自证用的手写文本与旧实现不一致，本用例的前提不成立")
	}
}

func TestSearchQueryHasNoHandWrittenVisibilityPredicate(t *testing.T) {
	b, err := os.ReadFile("query.go")
	if err != nil {
		t.Fatalf("读不到 query.go（测试需在包目录下运行）: %v", err)
	}
	src := string(b)

	if n := strings.Count(src, "shared_space_members"); n != 0 {
		t.Fatalf("search/query.go 里仍有 %d 处 shared_space_members —— 可见性谓词必须来自 "+
			"mediascope 单一真源，不得再手写一份（手写版曾漏掉 shared_space.owner_id 臂，"+
			"两处各自维护时改了一边另一边无从得知）", n)
	}
	if !strings.Contains(src, "mediascope.VisibleCond(") {
		t.Fatal("search/query.go 未调用 mediascope.VisibleCond：接线被摘掉了")
	}
	// 语义并集分支必须继续复用可见性谓词：历史上"忘了带上它"就是一次越权
	// （embed.SearchByVector 只过滤 deleted_at/embedding，TopK 可命中全库任何人的媒体）。
	if n := strings.Count(src, "visibleCond"); n < 3 {
		t.Fatalf("visibleCond 只出现 %d 次：它必须被 ①声明 ②加入 conds ③在语义并集分支复用，"+
			"少一处即越权（并集只写 m.id = ANY(...) 时会绕过授权与软删）", n)
	}
	// 前提自证：旧实现里这个字符串必然出现，否则上面的 count==0 断言是空转的。
	legacy := "SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $%[1]d"
	if strings.Count(legacy, "shared_space_members") != 1 {
		t.Fatal("守卫失效：用于自证的旧文本不含目标字符串，断言空转")
	}
}
