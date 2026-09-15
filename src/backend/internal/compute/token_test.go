package compute

import (
	"encoding/hex"
	"testing"
	"time"
)

// TestGenerateAgentToken 令牌生成：64 位 hex、合法字符集、千次不重复、明文与哈希一致。
func TestGenerateAgentToken(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		plain, hash, err := GenerateAgentToken()
		if err != nil {
			t.Fatalf("GenerateAgentToken 出错: %v", err)
		}
		if len(plain) != AgentTokenBytes*2 {
			t.Fatalf("明文令牌长度应为 %d，实际 %d", AgentTokenBytes*2, len(plain))
		}
		if _, err := hex.DecodeString(plain); err != nil {
			t.Fatalf("明文令牌非合法 hex: %v", err)
		}
		if len(hash) != 64 {
			t.Fatalf("哈希长度应为 64（VARCHAR(64)），实际 %d", len(hash))
		}
		if hash != HashAgentToken(plain) {
			t.Fatal("返回的哈希与 HashAgentToken(明文) 不一致")
		}
		if hash == plain {
			t.Fatal("哈希与明文相同——说明根本没有哈希")
		}
		if _, dup := seen[plain]; dup {
			t.Fatal("令牌重复")
		}
		seen[plain] = struct{}{}
	}
}

// TestHashAgentToken 哈希的三条不变量：幂等、长度 64（正好落进 VARCHAR(64)）、不同输入不碰撞。
func TestHashAgentToken(t *testing.T) {
	if HashAgentToken("pano") != HashAgentToken("pano") {
		t.Fatal("同一输入两次哈希结果不同")
	}
	if HashAgentToken("pano") == HashAgentToken("pano ") {
		t.Fatal("不同输入得到相同哈希")
	}
	if len(HashAgentToken("")) != 64 {
		t.Fatal("空串哈希长度也应为 64")
	}
}

// TestTokenValid 令牌校验：正例、错令牌、空值、永不过期、已过期、恰好到期。
func TestTokenValid(t *testing.T) {
	plain, hash, err := GenerateAgentToken()
	if err != nil {
		t.Fatalf("生成令牌失败: %v", err)
	}
	now := time.Now()

	past := now.Add(-time.Second)
	future := now.Add(time.Hour)

	cases := []struct {
		name       string
		storedHash string
		presented  string
		expiresAt  *time.Time
		want       bool
	}{
		{"正例：令牌正确且未过期", hash, plain, nil, true},
		{"正例：令牌正确且未到过期", hash, plain, &future, true},
		{"误：令牌错误", hash, plain + "x", nil, false},
		{"误：完全不同的令牌", hash, "deadbeef", nil, false},
		{"误：expiresAt 已过", hash, plain, &past, false},
		{"误：expiresAt 恰好等于 now（到期即失效）", hash, plain, &now, false},
		{"误：库中无哈希（节点未启用 agent）", "", plain, nil, false},
		{"误：来访令牌为空", hash, "", nil, false},
		{"误：库中无哈希且来访为空", "", "", nil, false},
		{"误：拿哈希当明文来撞（明文不得等于哈希）", hash, hash, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TokenValid(tc.storedHash, tc.presented, tc.expiresAt, now); got != tc.want {
				t.Fatalf("TokenValid() = %v，期望 %v", got, tc.want)
			}
		})
	}
}
