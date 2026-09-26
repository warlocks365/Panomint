package auth

import (
	"strings"
	"testing"
	"time"
)

// Job000128 纯函数穷举测试：密码策略校验 / 锁定判定 / 默认策略一致性 / 邀请码格式。
// 这些是"账号安全"的第一道门——全部用硬编码期望值当参照物（守卫测试纪律：
// 拿产品函数互比会"真源错两边一起绿"，所以这里只断言字面量）。

// --- ValidatePasswordPolicy ---

func TestValidatePasswordPolicy(t *testing.T) {
	base := AccountPolicy{PwdMinLength: 8}

	t.Run("满足策略：通过", func(t *testing.T) {
		p := base
		p.PwdRequireUpper, p.PwdRequireLower, p.PwdRequireDigit, p.PwdRequireSpecial = true, true, true, true
		for _, pwd := range []string{"Abcdef1!xyz", "A1!aaaaa"} {
			if err := ValidatePasswordPolicy(pwd, p); err != nil {
				t.Fatalf("%q 应通过全要求策略: %v", pwd, err)
			}
		}
	})

	t.Run("长度不足（按 rune 计，中文密码按字算）", func(t *testing.T) {
		if err := ValidatePasswordPolicy("Ab1!", AccountPolicy{PwdMinLength: 8}); err == nil ||
			!strings.Contains(err.Error(), "至少 8 位") {
			t.Fatalf("应报至少 8 位: %v", err)
		}
		// 4 个中文字 = 4 rune（len 按 UTF-8 是 12 字节，若按字节计会误通过 min 8）
		if err := ValidatePasswordPolicy("你好世界", AccountPolicy{PwdMinLength: 8}); err == nil {
			t.Fatal("4 个中文（4 rune < 8）必须被拒——长度必须按 rune 计")
		}
		// 8 个中文字应通过（rune 口径）
		if err := ValidatePasswordPolicy("一二三四五六七八", AccountPolicy{PwdMinLength: 8}); err != nil {
			t.Fatalf("8 个中文字（8 rune）应通过: %v", err)
		}
	})

	t.Run("缺类别：错误文案列出缺失项", func(t *testing.T) {
		p := base
		p.PwdRequireUpper, p.PwdRequireDigit, p.PwdRequireSpecial = true, true, true
		err := ValidatePasswordPolicy("abcdefgh", p) // 全小写：缺大写/数字/特殊
		if err == nil {
			t.Fatal("必须被拒")
		}
		for _, want := range []string{"大写字母", "数字", "特殊字符"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("文案须含 %q: %v", want, err)
			}
		}
		if strings.Contains(err.Error(), "小写字母") {
			t.Fatalf("已有小写不得出现在缺失清单: %v", err)
		}
	})

	t.Run("未勾选的类别不设限", func(t *testing.T) {
		// 只要求长度：纯数字 8 位也通过
		if err := ValidatePasswordPolicy("12345678", base); err != nil {
			t.Fatalf("无复杂度要求时纯数字应通过: %v", err)
		}
	})
}

// --- EvaluateLock ---

func TestEvaluateLock(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	t.Run("nil = 未锁定", func(t *testing.T) {
		if ls := EvaluateLock(nil, now); ls.Locked {
			t.Fatal("nil 必须判定未锁定")
		}
	})

	t.Run("过去时刻 = 已解除", func(t *testing.T) {
		past := now.Add(-time.Minute)
		if ls := EvaluateLock(&past, now); ls.Locked {
			t.Fatal("过去时刻必须判定已解除")
		}
	})

	t.Run("恰好到期（== now）= 已解除（不冤枉多锁一秒）", func(t *testing.T) {
		exact := now
		if ls := EvaluateLock(&exact, now); ls.Locked {
			t.Fatal("locked_until == now 必须视为已解除")
		}
	})

	t.Run("未来时刻 = 锁定且剩余分钟向上取整", func(t *testing.T) {
		cases := []struct {
			remaining time.Duration
			wantMin   int
		}{
			{15 * time.Minute, 15},
			{14 * time.Minute, 14},
			{90 * time.Second, 2},  // 1.5 分钟 → 显示 2
			{30 * time.Second, 1},  // 还剩半分钟也显示约 1 分钟
			{1 * time.Second, 1},   // 最小 1
		}
		for _, tc := range cases {
			until := now.Add(tc.remaining)
			ls := EvaluateLock(&until, now)
			if !ls.Locked {
				t.Fatalf("remaining=%v 必须判定锁定", tc.remaining)
			}
			if ls.RemainingMinutes != tc.wantMin {
				t.Fatalf("remaining=%v 应显示 %d 分钟，实际 %d", tc.remaining, tc.wantMin, ls.RemainingMinutes)
			}
		}
	})
}

// --- DefaultPolicy 与迁移 00043 DDL 默认值逐项一致（fail-closed 回退的正确性钉） ---

func TestDefaultPolicyMatchesDDL(t *testing.T) {
	p := DefaultPolicy()
	if p.AllowRegistration {
		t.Fatal("注册开关默认必须为关闭（DDL DEFAULT FALSE）——自托管相册默认私有")
	}
	if p.InviteRequired {
		t.Fatal("邀请开关默认必须为关闭（DDL DEFAULT FALSE）")
	}
	if p.PwdMinLength != 8 {
		t.Fatalf("密码最小长度默认 8（= MinPasswordLen / DDL DEFAULT 8），实际 %d", p.PwdMinLength)
	}
	if p.PwdRequireUpper || p.PwdRequireLower || p.PwdRequireDigit || p.PwdRequireSpecial {
		t.Fatal("四类复杂度默认全不勾选（DDL DEFAULT FALSE）——开启与否交给管理员")
	}
	if p.LoginMaxAttempts != 5 {
		t.Fatalf("失败次数上限默认 5（DDL DEFAULT 5），实际 %d", p.LoginMaxAttempts)
	}
	if p.LoginLockMinutes != 15 {
		t.Fatalf("锁定时长默认 15 分钟（DDL DEFAULT 15），实际 %d", p.LoginLockMinutes)
	}
}

// --- GenerateInviteCode 格式与唯一性 ---

func TestGenerateInviteCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code, err := GenerateInviteCode()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if !strings.HasPrefix(code, "inv-") {
			t.Fatalf("前缀必须为 inv-: %q", code)
		}
		hexPart := strings.TrimPrefix(code, "inv-")
		if len(hexPart) != 16 {
			t.Fatalf("hex 部分必须 16 字符（8 字节）：%q", code)
		}
		for _, c := range hexPart {
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
			if !isHex {
				t.Fatalf("必须是小写 hex: %q", code)
			}
		}
		if seen[code] {
			t.Fatalf("100 次生成出现重复（64 位熵下概率≈0，出现即 RNG 故障）: %q", code)
		}
		seen[code] = true
	}
}

// --- normalizeAccountPolicy 范围校验（与 DDL CHECK 一致） ---

func TestNormalizeAccountPolicyRange(t *testing.T) {
	valid := accountPolicyReq{
		PwdMinLength: 8, LoginMaxAttempts: 5, LoginLockMinutes: 15,
	}
	if _, err := normalizeAccountPolicy(valid); err != nil {
		t.Fatalf("合法输入不应报错: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*accountPolicyReq)
	}{
		{"长度过短", func(r *accountPolicyReq) { r.PwdMinLength = 4 }},
		{"长度过长", func(r *accountPolicyReq) { r.PwdMinLength = 100 }},
		{"尝试次数为 0", func(r *accountPolicyReq) { r.LoginMaxAttempts = 0 }},
		{"尝试次数过大", func(r *accountPolicyReq) { r.LoginMaxAttempts = 99 }},
		{"锁定时长为 0", func(r *accountPolicyReq) { r.LoginLockMinutes = 0 }},
		{"锁定时长超一天", func(r *accountPolicyReq) { r.LoginLockMinutes = 1441 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mut(&in)
			if _, err := normalizeAccountPolicy(in); err == nil {
				t.Fatalf("%s 必须被拒", tc.name)
			}
		})
	}
}
