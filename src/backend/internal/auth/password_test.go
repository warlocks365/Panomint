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
