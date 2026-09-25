package debug

// 凭据生成纯函数的穷举单测（设计 §6.1/§12·B1）：
// 字符集合规、长度钉死、1e4 次零重复、摘要确定性、过期边界、指纹截取。

import (
	"regexp"
	"testing"
	"time"
)

func TestGenerateChannelID(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	re := regexp.MustCompile(`^[a-f0-9]{24}$`)
	for i := 0; i < n; i++ {
		id, err := GenerateChannelID()
		if err != nil {
			t.Fatalf("第 %d 次生成失败: %v", i, err)
		}
		if !re.MatchString(id) {
			t.Fatalf("channel_id %q 不合规（需 24 位 [a-f0-9]）", id)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("channel_id 重复: %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestGenerateAccessKey(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	re := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	for i := 0; i < n; i++ {
		k, err := GenerateAccessKey()
		if err != nil {
			t.Fatalf("第 %d 次生成失败: %v", i, err)
		}
		if !re.MatchString(k) {
			t.Fatalf("access_key %q 不合规（需 43 位 base64url [A-Za-z0-9_-]）", k)
		}
		if _, dup := seen[k]; dup {
			t.Fatalf("access_key 重复: %q", k)
		}
		seen[k] = struct{}{}
	}
}

func TestHashAccessKey(t *testing.T) {
	re := regexp.MustCompile(`^[a-f0-9]{64}$`)
	h := HashAccessKey("test-key")
	if !re.MatchString(h) {
		t.Fatalf("摘要 %q 不合规（需 64 位小写 hex）", h)
	}
	if HashAccessKey("test-key") != h {
		t.Fatal("摘要不确定（同输入不同输出）")
	}
	if HashAccessKey("other") == h {
		t.Fatal("不同输入摘要相同")
	}
}

func TestKeyValid(t *testing.T) {
	key, err := GenerateAccessKey()
	if err != nil {
		t.Fatal(err)
	}
	digest := HashAccessKey(key)
	now := time.Now()
	future := now.Add(time.Hour)
	past := now.Add(-time.Second)

	if !KeyValid(digest, key, future, now) {
		t.Fatal("合法密钥+未过期 应通过")
	}
	if KeyValid("", key, future, now) {
		t.Fatal("空摘要 应拒绝")
	}
	if KeyValid(digest, "", future, now) {
		t.Fatal("空密钥 应拒绝")
	}
	if KeyValid(digest, key, past, now) {
		t.Fatal("已过期 应拒绝（到期即失效）")
	}
	if KeyValid(digest, key, now, now) {
		t.Fatal("now==expiresAt 边界 应拒绝（不留模糊地带）")
	}
	wrong, _ := GenerateAccessKey()
	if KeyValid(digest, wrong, future, now) {
		t.Fatal("错误密钥 应拒绝")
	}
}

func TestFingerprint(t *testing.T) {
	if got := Fingerprint("abcdef1234567890"); got != "abcdef12" {
		t.Fatalf("指纹截取错误: %q", got)
	}
	if got := Fingerprint("ab"); got != "ab" {
		t.Fatalf("短 id 应原样返回: %q", got)
	}
}

func TestValidTTL(t *testing.T) {
	for _, h := range []int{1, 8, 24, 72} {
		if !ValidTTL(h) {
			t.Fatalf("合法档位 %d 被判非法", h)
		}
	}
	for _, h := range []int{0, 2, 7, 9, 25, 73, -1, 100} {
		if ValidTTL(h) {
			t.Fatalf("非法档位 %d 被判合法", h)
		}
	}
}
