package shares

import (
	"encoding/hex"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestGenToken token 生成：64 位 hex、合法字符集、千次不重复。
func TestGenToken(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		tok, err := genToken()
		if err != nil {
			t.Fatalf("genToken err: %v", err)
		}
		if len(tok) != 64 {
			t.Fatalf("token 长度应为 64，实际 %d", len(tok))
		}
		if _, err := hex.DecodeString(tok); err != nil {
			t.Fatalf("token 非合法 hex: %v", err)
		}
		if _, dup := seen[tok]; dup {
			t.Fatal("token 重复")
		}
		seen[tok] = struct{}{}
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
func ptrInt(v int) *int              { return &v }
func ptrStr(s string) *string        { return &s }

// TestCheckAccess 过期/超量/密码三类拒绝与放行边界。
func TestCheckAccess(t *testing.T) {
	now := time.Now()
	hash, _ := bcrypt.GenerateFromPassword([]byte("s3cret"), bcrypt.MinCost)

	cases := []struct {
		name string
		sh   *Share
		pwd  string
		want string
	}{
		{"无限制放行", &Share{}, "", ""},
		{"未过期放行", &Share{ExpireAt: ptrTime(now.Add(time.Hour))}, "", ""},
		{"恰好过期拒绝", &Share{ExpireAt: ptrTime(now.Add(-time.Second))}, "", "EXPIRED"},
		{"未超量放行", &Share{MaxViews: ptrInt(3), AccessCount: 2}, "", ""},
		{"达上限拒绝", &Share{MaxViews: ptrInt(3), AccessCount: 3}, "", "MAX_VIEWS"},
		{"超限拒绝", &Share{MaxViews: ptrInt(3), AccessCount: 10}, "", "MAX_VIEWS"},
		{"密码缺失", &Share{PasswordHash: ptrStr(string(hash))}, "", "PASSWORD_REQUIRED"},
		{"密码错误", &Share{PasswordHash: ptrStr(string(hash))}, "bad", "WRONG_PASSWORD"},
		{"密码正确", &Share{PasswordHash: ptrStr(string(hash))}, "s3cret", ""},
		{"过期优先于密码", &Share{ExpireAt: ptrTime(now.Add(-time.Second)), PasswordHash: ptrStr(string(hash))}, "s3cret", "EXPIRED"},
		{"超量优先于密码", &Share{MaxViews: ptrInt(1), AccessCount: 1, PasswordHash: ptrStr(string(hash))}, "s3cret", "MAX_VIEWS"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkAccess(tc.sh, tc.pwd, now); got != tc.want {
				t.Fatalf("checkAccess = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStatus 列表状态：active|expired|exhausted。
func TestStatus(t *testing.T) {
	now := time.Now()
	if got := (&Share{}).Status(now); got != "active" {
		t.Fatalf("默认应为 active，实际 %s", got)
	}
	if got := (&Share{ExpireAt: ptrTime(now.Add(-time.Minute))}).Status(now); got != "expired" {
		t.Fatalf("应为 expired，实际 %s", got)
	}
	if got := (&Share{MaxViews: ptrInt(5), AccessCount: 5}).Status(now); got != "exhausted" {
		t.Fatalf("应为 exhausted，实际 %s", got)
	}
	// 过期优先于超量
	sh := &Share{ExpireAt: ptrTime(now.Add(-time.Minute)), MaxViews: ptrInt(1), AccessCount: 9}
	if got := sh.Status(now); got != "expired" {
		t.Fatalf("过期应优先，实际 %s", got)
	}
}

// TestMediaInShareMediaKind media 类分享纯归属判定（不触库分支）。
func TestMediaInShareMediaKind(t *testing.T) {
	s := &Store{} // kind=media 分支不触库，Pool 可为 nil
	sh := &Share{Kind: "media", TargetID: "m-1"}
	ok, err := s.MediaInShare(t.Context(), sh, "m-1")
	if err != nil || !ok {
		t.Fatalf("同 id 应属于分享: ok=%v err=%v", ok, err)
	}
	ok, err = s.MediaInShare(t.Context(), sh, "m-2")
	if err != nil || ok {
		t.Fatalf("异 id 应拒绝: ok=%v err=%v", ok, err)
	}
}
