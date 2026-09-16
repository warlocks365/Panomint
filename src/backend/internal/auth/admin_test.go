package auth

import (
	"errors"
	"strings"
	"testing"
)

// 本文件覆盖「管理后台用户改动」的纯逻辑：输入归一化、两条硬守卫、SET 子句拼装。
//
// 为什么值得单独测：这两条守卫（不许改自己的角色/状态、不许移除最后一个可用 owner）
// 属于"改错了就再也进不去、且只能靠直接改库恢复"的那类逻辑。它们必须能在 CI 里穷举，
// 而不是靠"部署后手动试一次"——手动试一次的成本是**把管理员锁在门外**。

func ptrS(s string) *string { return &s }

// TestNormalizeUserUpdate 输入归一化与校验。
func TestNormalizeUserUpdate(t *testing.T) {
	t.Run("去空白 + 角色小写", func(t *testing.T) {
		got, err := NormalizeUserUpdate(UserUpdate{DisplayName: ptrS("  阿明  "), Role: ptrS("  MEMBER ")})
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if *got.DisplayName != "阿明" {
			t.Fatalf("display_name 应去首尾空白，实际 %q", *got.DisplayName)
		}
		if *got.Role != "member" {
			t.Fatalf("role 应转小写，实际 %q", *got.Role)
		}
	})

	t.Run("状态白名单", func(t *testing.T) {
		// users.status **没有 CHECK 约束**，所以"合法状态"只能靠这里把关
		for _, ok := range []string{"active", "disabled", " ACTIVE ", "Disabled"} {
			if _, err := NormalizeUserUpdate(UserUpdate{Status: ptrS(ok)}); err != nil {
				t.Fatalf("状态 %q 应被接受: %v", ok, err)
			}
		}
		for _, bad := range []string{"", "  ", "deleted", "banned", "1"} {
			if _, err := NormalizeUserUpdate(UserUpdate{Status: ptrS(bad)}); err == nil {
				t.Fatalf("状态 %q 应被拒绝", bad)
			}
		}
	})

	t.Run("空角色被拒", func(t *testing.T) {
		if _, err := NormalizeUserUpdate(UserUpdate{Role: ptrS("   ")}); err == nil {
			t.Fatal("空 role 应被拒绝（否则会写出 role_id=NULL 触 NOT NULL）")
		}
	})

	t.Run("密码长度", func(t *testing.T) {
		short := strings.Repeat("a", MinPasswordLen-1)
		if _, err := NormalizeUserUpdate(UserUpdate{Password: &short}); err == nil {
			t.Fatalf("低于 %d 位的密码应被拒绝", MinPasswordLen)
		}
		good := strings.Repeat("a", MinPasswordLen)
		if _, err := NormalizeUserUpdate(UserUpdate{Password: &good}); err != nil {
			t.Fatalf("正好 %d 位应被接受: %v", MinPasswordLen, err)
		}
	})

	t.Run("空输入", func(t *testing.T) {
		got, err := NormalizeUserUpdate(UserUpdate{})
		if err != nil {
			t.Fatalf("空输入不应报错: %v", err)
		}
		if !got.Empty() {
			t.Fatal("空输入应判为 Empty")
		}
		if (UserUpdate{Status: ptrS("disabled")}).Empty() {
			t.Fatal("有字段时不应判为 Empty")
		}
	})

	t.Run("ChangesRoleOrStatus 只认角色与状态", func(t *testing.T) {
		if (UserUpdate{DisplayName: ptrS("x")}).ChangesRoleOrStatus() {
			t.Fatal("只改昵称不应算作触及角色/状态")
		}
		if (UserUpdate{Password: ptrS("12345678")}).ChangesRoleOrStatus() {
			t.Fatal("只改密码不应算作触及角色/状态")
		}
		if !(UserUpdate{Role: ptrS("member")}).ChangesRoleOrStatus() {
			t.Fatal("改角色应算作触及")
		}
		if !(UserUpdate{Status: ptrS("disabled")}).ChangesRoleOrStatus() {
			t.Fatal("改状态应算作触及")
		}
	})
}

// TestCheckUserPatchSelfLockout 不许改**自己**的角色/状态，但改自己的资料/密码不限。
//
// 自锁的代价：管理员把自己降级或禁用后可能当场失去管理权限（甚至立刻登不进来），
// 而恢复只能靠直接改库。这是纯自伤，没有正当使用场景。
func TestCheckUserPatchSelfLockout(t *testing.T) {
	const me = "actor-1"
	self := &User{ID: me, Role: "owner", Status: StatusActive}

	t.Run("改自己的角色 → 拒绝", func(t *testing.T) {
		err := CheckUserPatch(me, self, UserUpdate{Role: ptrS("member")}, 5)
		if !errors.Is(err, ErrSelfLockout) {
			t.Fatalf("应返回 ErrSelfLockout，实际 %v", err)
		}
	})
	t.Run("改自己的状态 → 拒绝", func(t *testing.T) {
		err := CheckUserPatch(me, self, UserUpdate{Status: ptrS("disabled")}, 5)
		if !errors.Is(err, ErrSelfLockout) {
			t.Fatalf("应返回 ErrSelfLockout，实际 %v", err)
		}
	})
	t.Run("只改自己的昵称 → 允许", func(t *testing.T) {
		if err := CheckUserPatch(me, self, UserUpdate{DisplayName: ptrS("新名字")}, 5); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("只改自己的密码 → 允许", func(t *testing.T) {
		if err := CheckUserPatch(me, self, UserUpdate{Password: ptrS("12345678")}, 5); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("改别人的角色 → 允许", func(t *testing.T) {
		other := &User{ID: "user-2", Role: "member", Status: StatusActive}
		if err := CheckUserPatch(me, other, UserUpdate{Role: ptrS("viewer")}, 5); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
}

// TestCheckUserPatchLastOwner 不许把**最后一个** active owner 降级/禁用。
//
// 为什么必须拦：owner 是唯一带 admin:system 的角色。把最后一个可用 owner 弄没了，
// 这台服务器就再没有人能管理，同样只能靠改库恢复。
func TestCheckUserPatchLastOwner(t *testing.T) {
	const actor = "admin-1"
	// 目标是"另一个 owner"（不是操作者自己，以隔离出自锁守卫）
	owner := &User{ID: "owner-2", Role: "owner", Status: StatusActive}

	t.Run("最后一个 owner 被降级 → 拒绝", func(t *testing.T) {
		err := CheckUserPatch(actor, owner, UserUpdate{Role: ptrS("member")}, 0)
		if !errors.Is(err, ErrLastOwner) {
			t.Fatalf("应返回 ErrLastOwner，实际 %v", err)
		}
	})
	t.Run("最后一个 owner 被禁用 → 拒绝", func(t *testing.T) {
		err := CheckUserPatch(actor, owner, UserUpdate{Status: ptrS("disabled")}, 0)
		if !errors.Is(err, ErrLastOwner) {
			t.Fatalf("应返回 ErrLastOwner，实际 %v", err)
		}
	})
	t.Run("还有别的 owner → 允许", func(t *testing.T) {
		if err := CheckUserPatch(actor, owner, UserUpdate{Role: ptrS("member")}, 1); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("只改 owner 的昵称 → 允许（不减少可用 owner 数）", func(t *testing.T) {
		if err := CheckUserPatch(actor, owner, UserUpdate{DisplayName: ptrS("x")}, 0); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("非 owner 被禁用 → 允许", func(t *testing.T) {
		m := &User{ID: "user-3", Role: "member", Status: StatusActive}
		if err := CheckUserPatch(actor, m, UserUpdate{Status: ptrS("disabled")}, 0); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("已是 disabled 的 owner 再改 → 允许（它本来就不算可用）", func(t *testing.T) {
		dead := &User{ID: "owner-9", Role: "owner", Status: StatusDisabled}
		if err := CheckUserPatch(actor, dead, UserUpdate{Role: ptrS("member")}, 0); err != nil {
			t.Fatalf("应允许，实际 %v", err)
		}
	})
	t.Run("角色改为 owner（提升）→ 允许", func(t *testing.T) {
		m := &User{ID: "user-4", Role: "member", Status: StatusActive}
		if err := CheckUserPatch(actor, m, UserUpdate{Role: ptrS("owner")}, 0); err != nil {
			t.Fatalf("提升为 owner 不减少可用 owner 数，应允许，实际 %v", err)
		}
	})
	t.Run("目标为空 → ErrUserNotFound", func(t *testing.T) {
		if err := CheckUserPatch(actor, nil, UserUpdate{}, 0); !errors.Is(err, ErrUserNotFound) {
			t.Fatalf("应返回 ErrUserNotFound，实际 %v", err)
		}
	})
}

// TestBuildUserUpdate SET 子句拼装：占位符连续、未提供的字段不出现、密码哈希按需写入。
func TestBuildUserUpdate(t *testing.T) {
	t.Run("只改昵称：一条 SET", func(t *testing.T) {
		sets, args := buildUserUpdate(UserUpdate{DisplayName: ptrS("n")}, "")
		if len(sets) != 1 || len(args) != 1 || sets[0] != "display_name = $1" {
			t.Fatalf("实际 sets=%v args=%v", sets, args)
		}
	})

	t.Run("多字段：占位符从 $1 连续", func(t *testing.T) {
		sets, args := buildUserUpdate(UserUpdate{
			DisplayName: ptrS("n"), Role: ptrS("member"), Status: ptrS("disabled"),
		}, "")
		if len(sets) != 3 || len(args) != 3 {
			t.Fatalf("应有 3 条 SET / 3 个参数，实际 sets=%v args=%v", sets, args)
		}
		for i, want := range []string{"$1", "$2", "$3"} {
			if !strings.Contains(sets[i], want) {
				t.Fatalf("第 %d 条应含占位符 %s，实际 %q", i, want, sets[i])
			}
		}
		// role 必须走子查询按名字解析（表里存的是 role_id）
		if !strings.Contains(sets[1], "(SELECT id FROM roles WHERE name = $2)") {
			t.Fatalf("role 应写为按名字解析的子查询，实际 %q", sets[1])
		}
	})

	t.Run("密码：有哈希才写，且键名是 password_hash", func(t *testing.T) {
		in := UserUpdate{Password: ptrS("12345678")}
		sets, args := buildUserUpdate(in, "")
		if len(sets) != 0 {
			t.Fatalf("未传哈希时不应产出 SET（bcrypt 由 handler 生成），实际 %v", sets)
		}
		sets, args = buildUserUpdate(in, "$2a$10$fakehash")
		if len(sets) != 1 || args[0] != "$2a$10$fakehash" {
			t.Fatalf("应写入传入的哈希，实际 sets=%v args=%v", sets, args)
		}
		if sets[0] != "password_hash = $1" {
			t.Fatalf("列名应为 password_hash，实际 %q", sets[0])
		}
	})

	t.Run("未提供的字段绝不进 SET", func(t *testing.T) {
		sets, _ := buildUserUpdate(UserUpdate{Status: ptrS("disabled")}, "")
		joined := strings.Join(sets, ", ")
		for _, bad := range []string{"display_name", "role_id", "password_hash"} {
			if strings.Contains(joined, bad) {
				t.Fatalf("未提供的字段 %q 不该出现：%s", bad, joined)
			}
		}
	})
}

// TestValidUserStatus 状态白名单（users.status 无 CHECK 约束，这是唯一防线）。
func TestValidUserStatus(t *testing.T) {
	for _, s := range []string{"active", "disabled"} {
		if !ValidUserStatus(s) {
			t.Fatalf("%q 应合法", s)
		}
	}
	for _, s := range []string{"", "Active", "enabled", "banned", "deleted"} {
		if ValidUserStatus(s) {
			t.Fatalf("%q 不应合法（注意大小写敏感：写入前已归一化为小写）", s)
		}
	}
}
