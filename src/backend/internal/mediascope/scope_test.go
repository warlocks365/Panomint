package mediascope

import (
	"regexp"
	"strings"
	"testing"
)

// 可见性谓词「单一真源」的回归网。
//
// 背景：同一段「调用者本人可见集合」的谓词曾在 internal/search/query.go 被**第二次手写**，
// 且手写版本只判 shared_space_members、漏掉 shared_space.owner_id 臂，与时间轴口径漂移。
// 本文件钉住三类性质：
//  1. fail-closed：没有主体时恒假 —— **绝不返回不绑定主体的谓词**（历史越权事故的同形）；
//  2. 占位符不变量：编号从 start 起且 personal 臂与两个 EXISTS 复用同一个编号，
//     返回的 args 恰为 1 个元素（个数不一致 ⇒ pgx 运行时报 "expected N arguments"）；
//  3. 单一真源：VisibleCond 与 Conds 的 shared 臂出自同一个 sharedVerdict，逐字节相同。

// ---- 1. fail-closed ----

func TestVisibleCondFailClosedWithoutUser(t *testing.T) {
	if FailClosed != "false" {
		t.Fatalf("FailClosed 必须是恒假字面量 false，实际 %q", FailClosed)
	}
	for _, start := range []int{1, 5} {
		cond, args := VisibleCond(start, "")
		if cond != FailClosed {
			t.Fatalf("start=%d 且无身份时必须恒假 %q，实际 %q —— "+
				"返回任何带主体的谓词都等于放行全库，这正是历史越权事故的同形", start, FailClosed, cond)
		}
		if args != nil {
			t.Fatalf("start=%d 且无身份时不得产生参数，实际 %v", start, args)
		}
	}
}

// ---- 2. 占位符编号与参数个数 ----

var placeholderRe = regexp.MustCompile(`\$(\d+)`)

func TestVisibleCondPlaceholderNumberingAndArgCount(t *testing.T) {
	const uid = "u1"
	// 用 start=5 这种非 1 的值才能证明"编号真的跟随 start"，而不是碰巧写死成 $1。
	cond, args := VisibleCond(5, uid)

	nums := placeholderRe.FindAllStringSubmatch(cond, -1)
	if len(nums) == 0 {
		t.Fatalf("谓词里没有任何占位符: %q", cond)
	}
	for _, m := range nums {
		if m[1] != "5" {
			t.Fatalf("占位符 %s 应为 $5（编号必须从 start 起）: %q", m[0], cond)
		}
	}
	// personal 臂 1 处 + shared 的两个 EXISTS 各 1 处 + 目录授予 EXISTS 1 处，复用同一个 $5。
	if len(nums) != 4 {
		t.Fatalf("应恰好 4 处 $5（personal 1 + 成员 EXISTS 1 + 属主 EXISTS 1 + 目录授予 EXISTS 1），实际 %d 处: %q",
			len(nums), cond)
	}
	if strings.Contains(cond, "$1") {
		t.Fatalf("start=5 时不得出现 $1（否则参数会错位到别的列上）: %q", cond)
	}
	// 这正是本用例要钉死的不变量：占位符只占用 1 个参数位，args 必须恰好 1 个。
	if len(args) != 1 || args[0] != uid {
		t.Fatalf("args 必须恰好为 [%s]（三个占位符共用一个参数位），实际 %v", uid, args)
	}
}

// ---- 3. 单一真源：VisibleCond 与 Conds 的 shared 臂同源 ----

func TestVisibleCondSharesSharedArmWithConds(t *testing.T) {
	const uid = "u1"
	sharedConds, sharedArgs := Conds(Scope{Space: "shared", MemberID: uid})
	if len(sharedConds) != 2 {
		t.Fatalf("shared 档应为 2 条条件，实际 %v", sharedConds)
	}
	sharedArm := sharedConds[1]

	// 两臂都必须真的在（成员 ∪ 属主）——这是与 search 收敛后的共同口径。
	if !strings.Contains(sharedArm, "shared_space_members sm WHERE sm.user_id = $1") {
		t.Fatalf("shared 臂缺少成员判定: %q", sharedArm)
	}
	if !strings.Contains(sharedArm, "shared_space ss WHERE ss.owner_id = $1") {
		t.Fatalf("shared 臂缺少属主判定: %q", sharedArm)
	}
	if n := strings.Count(sharedArm, "$1"); n != 2 {
		t.Fatalf("两个 EXISTS 必须绑同一个 $1，实际 %d 处: %q", n, sharedArm)
	}

	visible, vArgs := VisibleCond(1, uid)
	if !strings.Contains(visible, sharedArm) {
		t.Fatalf("VisibleCond 的 shared 臂必须与 Conds 的 shared 臂逐字节相同（同出 sharedVerdict）。\n"+
			"Conds   = %q\nVisible = %q", sharedArm, visible)
	}
	if len(sharedArgs) != 1 || len(vArgs) != 1 {
		t.Fatalf("两侧都应只占 1 个参数位：Conds=%v Visible=%v", sharedArgs, vArgs)
	}
	// 前提自证：把 shared 臂换成"只判成员"的手写版本后，上面的 Contains 必须不成立，
	// 否则本用例是空转的（拦不住"search 侧又抄回一份漏掉属主臂的谓词"）。
	handwritten := "(EXISTS(SELECT 1 FROM shared_space_members sm WHERE sm.user_id = $1))"
	if strings.Contains(handwritten, sharedArm) {
		t.Fatal("守卫失效：只判成员的手写臂也包含了 Conds 的 shared 臂，本断言拦不住回归")
	}
}

// ---- 4. Conds 既有语义回归（原 media/scope_test.go 的口径）----

func TestCondsPersonalBindsOwner(t *testing.T) {
	conds, args := Conds(Scope{Space: "personal", OwnerID: "u1"})
	got := strings.Join(conds, " AND ")
	if !strings.Contains(got, "m.space = 'personal'") || !strings.Contains(got, "m.owner_id = $1") {
		t.Fatalf("本人作用域谓词不对: %q", got)
	}
	if len(args) != 1 || args[0] != "u1" {
		t.Fatalf("args 应为 [u1]，实际 %v", args)
	}
}

func TestCondsUnprovableScopeIsFailClosed(t *testing.T) {
	for _, s := range []Scope{
		{},                  // 零值：忘了解析作用域
		{Space: "personal"}, // 缺主体
		{Space: "shared"},   // 缺主体
		{Space: "bogus", OwnerID: "u1"},
		{Space: "shared", OwnerID: "u1"}, // 主体放错字段：共享档要 MemberID
	} {
		conds, args := Conds(s)
		if len(args) != 0 {
			t.Fatalf("%+v 应不产生参数，实际 %v", s, args)
		}
		if got := strings.Join(conds, " AND "); got != FailClosed {
			t.Fatalf("%+v 应收敛为 %q（空结果），实际 %q —— 未证明安全的作用域绝不允许放行",
				s, FailClosed, got)
		}
	}
}

func TestResolveMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, space, userID string
		want                Scope
		wantErr             error
	}{
		{"缺省 → 本人个人空间", "", "u1", Scope{Space: "personal", OwnerID: "u1"}, nil},
		{"显式 personal → 本人", "personal", "u1", Scope{Space: "personal", OwnerID: "u1"}, nil},
		{"shared → 以本人身份做成员判定", "shared", "u1", Scope{Space: "shared", MemberID: "u1"}, nil},
		{"缺省且无身份 → 拒绝", "", "", Scope{}, ErrMissingUser},
		{"shared 且无身份 → 拒绝", "shared", "", Scope{}, ErrMissingUser},
		{"枚举外 → 拒绝", "bogus", "u1", Scope{}, ErrInvalidSpace},
		{"大小写不宽容", "Personal", "u1", Scope{}, ErrInvalidSpace},
		{"前导空白不 trim", " personal", "u1", Scope{}, ErrInvalidSpace},
	} {
		got, err := Resolve(tc.space, tc.userID)
		if err != tc.wantErr {
			t.Fatalf("%s: err=%v，期望 %v", tc.name, err, tc.wantErr)
		}
		if tc.wantErr != nil {
			continue
		}
		if got != tc.want {
			t.Fatalf("%s: got=%+v，期望 %+v", tc.name, got, tc.want)
		}
	}
}
