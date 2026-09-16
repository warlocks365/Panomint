package auth

import (
	"errors"
	"testing"
)

// 本文件覆盖角色管理的纯逻辑。重点在 **PermCovered**：它是"admin:users 不能自我提权到
// admin:system"这条不变量的唯一实现，必须穷举。

func TestNormalizeRoleInputValid(t *testing.T) {
	got, err := NormalizeRoleInput(RoleInput{
		Name:        "  Family_Member ",
		Description: "  家人：可上传不可删  ",
		Permissions: []string{" MEDIA:READ ", "media:read", "album:read"},
	})
	if err != nil {
		t.Fatalf("应通过: %v", err)
	}
	if got.Name != "family_member" {
		t.Fatalf("name 应去空白转小写，实际 %q", got.Name)
	}
	if got.Description != "家人：可上传不可删" {
		t.Fatalf("description 应去空白，实际 %q", got.Description)
	}
	// 去重保持首次出现顺序
	if len(got.Permissions) != 2 || got.Permissions[0] != "media:read" || got.Permissions[1] != "album:read" {
		t.Fatalf("权限应去重并经白名单归一，实际 %v", got.Permissions)
	}
}

func TestNormalizeRoleInputBadName(t *testing.T) {
	cases := []string{
		"",                                     // 空
		"a",                                    // 太短（要求 ≥2）
		"1abc",                                 // 数字开头
		"has space",                            // 空格
		"has-dash",                             // 连字符
		"has.dot",                              // 点
		"中文角色",                                 // 非 ASCII
		"averyveryverylongrolename_1234567890", // 超长
	}
	for _, n := range cases {
		if _, err := NormalizeRoleInput(RoleInput{Name: n, Permissions: []string{"media:read"}}); err == nil {
			t.Fatalf("角色名 %q 应被拒绝", n)
		}
	}
}

func TestNormalizeRoleInputPermWhitelist(t *testing.T) {
	// 拼错的权限串不会报错、只会永远匹配不上（表现为"给了权限却仍 403"），故必须在写入时拒掉
	for _, bad := range []string{"media:reed", "Media:Read2", "admin:everything", "*", "media", "media:read:extra"} {
		if _, err := NormalizeRoleInput(RoleInput{Name: "tester", Permissions: []string{bad}}); !errors.Is(err, ErrPermNotAllowed) {
			t.Fatalf("权限 %q 应被白名单拒绝，实际 %v", bad, err)
		}
	}
	// 白名单内 11 个全部可用
	for _, p := range KnownPerms() {
		if _, err := NormalizeRoleInput(RoleInput{Name: "tester", Permissions: []string{p}}); err != nil {
			t.Fatalf("白名单权限 %q 应被接受: %v", p, err)
		}
	}
}

func TestNormalizeRoleInputRequiresPermissions(t *testing.T) {
	// 空权限角色 = 能登录但什么都做不了，几乎必然是漏填
	for _, in := range [][]string{nil, {}, {"  ", ""}} {
		if _, err := NormalizeRoleInput(RoleInput{Name: "tester", Permissions: in}); err == nil {
			t.Fatalf("permissions=%v 应被拒绝", in)
		}
	}
}

func TestPermCovered(t *testing.T) {
	owner := []string{"media:*", "album:*", "share:*", "space:*", "admin:users", "admin:system"}
	limited := []string{"admin:users"} // 只持用户管理，**没有** admin:system

	t.Run("同权可授", func(t *testing.T) {
		if !PermCovered(limited, "admin:users") {
			t.Fatal("持有 admin:users 应可授予 admin:users")
		}
	})

	t.Run("⚠️ 提权守卫：admin:users 不得授予 admin:system", func(t *testing.T) {
		// 这是本文件最重要的一条：漏了它，只持 admin:users 的人就能建含 admin:system 的角色、
		// 再建账号用它登录，完成自我提权（POST /admin/users 允许按名字指定任意角色）。
		if PermCovered(limited, "admin:system") {
			t.Fatal("admin:users 不得覆盖 admin:system —— 否则是一条自我提权路径")
		}
	})

	t.Run("通配符覆盖同族", func(t *testing.T) {
		if !PermCovered([]string{"media:*"}, "media:read") || !PermCovered([]string{"media:*"}, "media:write") {
			t.Fatal("media:* 应覆盖 media 族全部权限")
		}
		if PermCovered([]string{"media:*"}, "album:read") {
			t.Fatal("media:* 不应覆盖其它族")
		}
	})

	t.Run("窄权限不得授予通配符", func(t *testing.T) {
		// 只有 media:read 却想授出 media:* → 授出的是自己都没有的能力，属提权
		if PermCovered([]string{"media:read"}, "media:*") {
			t.Fatal("media:read 不应覆盖 media:*")
		}
	})

	t.Run("owner 全量可授", func(t *testing.T) {
		for _, p := range KnownPerms() {
			if !PermCovered(owner, p) {
				t.Fatalf("owner 应可授予全部白名单权限，但 %q 未被覆盖", p)
			}
		}
	})

	t.Run("空权限集一律不可授", func(t *testing.T) {
		for _, p := range KnownPerms() {
			if PermCovered(nil, p) {
				t.Fatalf("无任何权限时不应可授予 %q", p)
			}
		}
		if PermCovered(owner, "") {
			t.Fatal("空 want 不应被覆盖")
		}
	})

	t.Run("全局通配前向兼容", func(t *testing.T) {
		if !PermCovered([]string{"*"}, "admin:system") {
			t.Fatal("持有 * 应覆盖一切")
		}
	})

	t.Run("大小写与空白归一", func(t *testing.T) {
		if !PermCovered([]string{"MEDIA:*"}, " media:read ") {
			t.Fatal("比较前应归一大小写与空白")
		}
	})
}

// TestKnownPermsMatchesSeedVocabulary 白名单必须覆盖库内种子用的全部权限名。
//
// 词表是手写的（`role_permissions.perm` 是自由文本列、没有外键可依赖），
// 所以要有测试盯住"别把某个真实在用的权限名漏掉"——漏掉会导致无法建出等价于内置角色的角色。
func TestKnownPermsMatchesSeedVocabulary(t *testing.T) {
	seeded := []string{
		"media:read", "media:write", "media:*",
		"album:read", "album:write", "album:*",
		"share:create", "share:*", "space:*",
		"admin:users", "admin:system",
	}
	for _, p := range seeded {
		if !isKnownPerm(p) {
			t.Fatalf("库内种子权限 %q 不在白名单内 —— 无法建出等价角色", p)
		}
	}
	if len(knownPerms) != len(seeded) {
		t.Fatalf("白名单数量 %d 与种子权限数量 %d 不一致（可能多出无人使用的权限名）",
			len(knownPerms), len(seeded))
	}
}

func TestKnownPermsReturnsCopy(t *testing.T) {
	a := KnownPerms()
	a[0] = "被改坏了"
	if KnownPerms()[0] == "被改坏了" {
		t.Fatal("KnownPerms 必须返回副本，调用方改动不得影响内部白名单")
	}
}
