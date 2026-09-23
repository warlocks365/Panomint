package auth

import (
	"strings"
	"testing"
)

// TestValidateChangePassword 自助改密的入参校验（纯函数穷举）。
//
// 与 NormalizeUserUpdate 的密码用例互补：那里钉"管理员设密"的约束，这里钉
// "自助改密"特有的两条 —— 必须有当前密码、新密码不得与当前密码相同。
func TestValidateChangePassword(t *testing.T) {
	good := strings.Repeat("a", MinPasswordLen)

	t.Run("合法组合", func(t *testing.T) {
		if err := validateChangePassword("old-password-1", good); err != nil {
			t.Fatalf("不应报错: %v", err)
		}
	})

	t.Run("缺当前密码", func(t *testing.T) {
		if err := validateChangePassword("", good); err == nil {
			t.Fatal("空当前密码必须被拒绝 —— 否则捡到已登录设备即可换锁")
		}
	})

	t.Run("缺新密码", func(t *testing.T) {
		if err := validateChangePassword("old-password-1", ""); err == nil {
			t.Fatal("空新密码必须被拒绝")
		}
	})

	t.Run("新密码过短", func(t *testing.T) {
		short := strings.Repeat("a", MinPasswordLen-1)
		if err := validateChangePassword("old-password-1", short); err == nil {
			t.Fatalf("低于 %d 位必须被拒绝", MinPasswordLen)
		}
		// 边界：恰好等于下限应通过。
		if err := validateChangePassword("old-password-1", good); err != nil {
			t.Fatalf("恰好 %d 位应通过: %v", MinPasswordLen, err)
		}
	})

	t.Run("新旧相同", func(t *testing.T) {
		if err := validateChangePassword(good, good); err == nil {
			t.Fatal("新密码与当前密码相同必须被拒绝（改了等于没改，还白吊销一轮会话）")
		}
	})

	t.Run("当前密码不做长度下限", func(t *testing.T) {
		// 旧密码可能是在 MinPasswordLen 引入前设置的短密码；这里只要求非空，
		// 正确性交给查库比对（VerifyPassword），不对历史短密码"关死改密通道"。
		if err := validateChangePassword("x", good); err != nil {
			t.Fatalf("短历史旧密码不应在入参层被拒: %v", err)
		}
	})
}

// TestGenerateAppPassword 应用密码明文生成的格式与唯一性（Job000098）。
//
// 应用密码是服务端随机生成、只下发一次的凭据：格式错误（客户端不兼容）或
// 可预测（熵不足）都会让"独立于主密码的泄漏面更小"这个设计目标落空，所以钉死：
// ① 前缀可识别 ② hex 字符集零客户端兼容风险 ③ 熵足够 ④ 两次生成绝不重复。
func TestGenerateAppPassword(t *testing.T) {
	const prefix = "pano-"

	t.Run("格式：前缀 + 40 位 hex", func(t *testing.T) {
		p, err := GenerateAppPassword()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if !strings.HasPrefix(p, prefix) {
			t.Fatalf("缺前缀 %q: %q", prefix, p)
		}
		body := strings.TrimPrefix(p, prefix)
		if len(body) != 40 {
			t.Fatalf("hex 体应为 40 字符（20 字节），实得 %d: %q", len(body), body)
		}
		for _, r := range body {
			if !('0' <= r && r <= '9' || 'a' <= r && r <= 'f') {
				t.Fatalf("hex 体含非小写十六进制字符 %q: %q", string(r), body)
			}
		}
	})

	t.Run("唯一性：200 次无重复", func(t *testing.T) {
		seen := make(map[string]struct{}, 200)
		for i := 0; i < 200; i++ {
			p, err := GenerateAppPassword()
			if err != nil {
				t.Fatalf("第 %d 次生成失败: %v", i, err)
			}
			if _, dup := seen[p]; dup {
				t.Fatalf("第 %d 次生成重复: %q", i, p)
			}
			seen[p] = struct{}{}
		}
	})

	t.Run("熵：前 8 位 hex 不全相同（弱随机一眼可见的退化）", func(t *testing.T) {
		p, err := GenerateAppPassword()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		body := strings.TrimPrefix(p, prefix)
		head := body[:8]
		allSame := true
		for _, r := range head[1:] {
			if r != rune(head[0]) {
				allSame = false
				break
			}
		}
		if allSame {
			t.Fatalf("前 8 位全同，疑似随机源退化: %q", head)
		}
	})
}
