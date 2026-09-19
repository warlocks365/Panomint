package mediascope

// ReadCond（P2-01）的回归网。
//
// ReadCond 是单条媒体「读」访问的**单一真源**（Detail/Thumb/Download 共用），
// 口径 = 属主 ∪（space='shared' 且调用者是共享空间成员或属主）∪ owner/admin。
// 这是扩大可见面的改动，所以本文件既钉放行面（谓词形状），也钉收紧面：
//   - 非成员靠**谓词本身**排除（属主等式与成员 EXISTS 都绑定调用者 $start，
//     非成员两行 EXISTS 都不命中 → allowed=false）；
//   - userID 为空 → FailClosed（历史越权事故的同形防护，见包文档）；
//   - owner/admin → "true" 短路（刻意的管理员语义）。

import (
	"strings"
	"testing"
)

func TestRolePrivileged(t *testing.T) {
	for _, r := range []string{"owner", "admin"} {
		if !RolePrivileged(r) {
			t.Fatalf("role=%q 应为特权角色", r)
		}
	}
	for _, r := range []string{"", "member", "viewer", "Owner", "ADMIN"} {
		if RolePrivileged(r) {
			t.Fatalf("role=%q 不应为特权角色（大小写敏感，取值必须与会话签发一致）", r)
		}
	}
}

func TestReadCondPrivilegedShortCircuit(t *testing.T) {
	for _, role := range []string{"owner", "admin"} {
		cond, args := ReadCond(2, "u-any", role, "")
		if cond != "true" || len(args) != 0 {
			t.Fatalf("role=%s 应短路为 (true, 无参数)，实际 cond=%q args=%v", role, cond, args)
		}
	}
}

func TestReadCondMissingUserFailClosed(t *testing.T) {
	// 无主体时唯一安全的输出是「谁也看不见」——绝不能返回不绑定主体的谓词。
	cond, args := ReadCond(1, "", "member", "")
	if cond != FailClosed || len(args) != 0 {
		t.Fatalf("空 userID 应 FailClosed 且无参数，实际 cond=%q args=%v", cond, args)
	}
}

func TestReadCondMemberPredicateShape(t *testing.T) {
	cond, args := ReadCond(1, "u1", "member", "")
	if len(args) != 1 || args[0] != "u1" {
		t.Fatalf("args 应恰好 1 个元素（调用者 userID），实际 %v", args)
	}
	// 放行面：属主等式 + shared 成员判定（与列表 shared 臂同一个 sharedVerdict）。
	for _, want := range []string{
		"owner_id = $1",    // 属主臂（不受 space 限制）
		"space = 'shared'", // shared 臂的空间限定
		"shared_space_members sm WHERE sm.user_id = $1", // 成员 EXISTS（绑定调用者）
		"shared_space ss WHERE ss.owner_id = $1",        // 空间属主 EXISTS（与列表口径一致）
	} {
		if !strings.Contains(cond, want) {
			t.Fatalf("谓词缺少 %q —— 放行面与列表口径漂移？实际: %s", want, cond)
		}
	}
	// 收紧面：属主等式与两个 EXISTS 都绑**同一个**占位符 $1（调用者），
	// 不出现第二个占位符（参数个数与占位符不一致会在运行期炸 pgx）。
	if strings.Contains(cond, "$2") {
		t.Fatalf("谓词出现 $2 —— 占位符应只有 $start 一个（args 只有 1 个元素）: %s", cond)
	}
}

func TestReadCondStartPlaceholderAndAlias(t *testing.T) {
	// start 偏移：谓词内全部占位符必须跟随 start（拼进更大查询时不撞号）。
	cond, args := ReadCond(3, "u1", "member", "m")
	if len(args) != 1 {
		t.Fatalf("args 应 1 个元素，实际 %v", args)
	}
	for _, want := range []string{"m.owner_id = $3", "m.space = 'shared'", "sm.user_id = $3", "ss.owner_id = $3"} {
		if !strings.Contains(cond, want) {
			t.Fatalf("start=3/alias=m 时谓词缺少 %q: %s", want, cond)
		}
	}
	if strings.Contains(cond, "$1") || strings.Contains(cond, "$2") {
		t.Fatalf("start=3 时不应出现 $1/$2: %s", cond)
	}
	// alias 为空 = 裸列名（未加别名的单表查询可用，与 qual 的约定一致）。
	cond2, _ := ReadCond(1, "u1", "member", "")
	if !strings.Contains(cond2, "owner_id = $1") || strings.Contains(cond2, "m.owner_id") {
		t.Fatalf("alias 为空应为裸列名: %s", cond2)
	}
}

func TestReadCondSharedArmMatchesListScope(t *testing.T) {
	// 单一真源断言：ReadCond 的 shared 臂与 Conds(shared) 的可见性臂**逐字节相同**
	// （同出 sharedVerdict）。若两侧漂移，「列表可见、详情 403」的断裂会复发。
	read, _ := ReadCond(1, "u1", "member", "m")
	_, listArgs := CondsFor(Scope{Space: "shared", MemberID: "u1"}, "m")
	if len(listArgs) != 1 || listArgs[0] != "u1" {
		t.Fatalf("Conds(shared) args 异常: %v", listArgs)
	}
	if !strings.Contains(read, sharedVerdict(1)) {
		t.Fatal("ReadCond 未复用 sharedVerdict —— 单条读判定与列表判定又各写了一份")
	}
}
