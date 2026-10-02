package storage

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// 固定测试密钥（32 字节 hex）——只用于单测进程，绝不与生产密钥同值。
const testKeyHex = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"

// resetCipherKey 清空进程级密钥缓存与 sync.Once，使下一个用例能重新读 env。
func resetCipherKey() {
	cipherKeyOnce = sync.Once{}
	cipherKey = nil
}

func withKey(t *testing.T) {
	t.Helper()
	t.Setenv("STORAGE_CIPHER_KEY", testKeyHex)
	resetCipherKey()
	t.Cleanup(resetCipherKey)
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	withKey(t)
	enc, err := EncryptCreds(&Creds{User: "alice", Pass: "s3cret", Domain: "WORK"})
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if enc == "" {
		t.Fatal("密文不应为空")
	}
	// 密文必须不含明文特征。
	if strings.Contains(enc, "s3cret") || strings.Contains(enc, "alice") {
		t.Fatal("密文泄露明文")
	}
	got, err := DecryptCreds(enc)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got.User != "alice" || got.Pass != "s3cret" || got.Domain != "WORK" {
		t.Fatalf("往返不一致: %+v", got)
	}
}

func TestEncryptCreds_NilAndEmpty(t *testing.T) {
	withKey(t)
	// 全空 → 空串（无凭据挂载）。
	enc, err := EncryptCreds(nil)
	if err != nil || enc != "" {
		t.Fatalf("nil 凭据应得空串: %q %v", enc, err)
	}
	enc, err = EncryptCreds(&Creds{})
	if err != nil || enc != "" {
		t.Fatalf("空凭据应得空串: %q %v", enc, err)
	}
	// 仅 pass（token 形态）允许。
	enc, err = EncryptCreds(&Creds{Pass: "tok"})
	if err != nil || enc == "" {
		t.Fatalf("仅 pass 应加密: %q %v", enc, err)
	}
}

func TestEncrypt_NoKey_FailClosed(t *testing.T) {
	t.Setenv("STORAGE_CIPHER_KEY", "")
	resetCipherKey()
	t.Cleanup(resetCipherKey)
	_, err := EncryptCreds(&Creds{User: "a", Pass: "b"})
	if err != ErrNoCipherKey {
		t.Fatalf("无密钥必须 ErrNoCipherKey，实际 %v", err)
	}
	// 未配置密钥时空凭据仍放行（无凭据挂载不依赖密钥）。
	enc, err := EncryptCreds(nil)
	if err != nil || enc != "" {
		t.Fatalf("无凭据挂载不应依赖密钥: %q %v", enc, err)
	}
}

func TestEncrypt_BadKeyLength(t *testing.T) {
	t.Setenv("STORAGE_CIPHER_KEY", "aabb") // 非 32 字节
	resetCipherKey()
	t.Cleanup(resetCipherKey)
	if CipherKeyConfigured() {
		t.Fatal("长度错误的密钥不得视为已配置")
	}
}

func TestDecrypt_Tampered(t *testing.T) {
	withKey(t)
	enc, _ := EncryptCreds(&Creds{User: "a", Pass: "b"})
	// 篡改密文中段。
	b := []byte(enc)
	if len(b) > 20 {
		b[10] = b[10] ^ 0xFF
		b[11] = b[11] ^ 0x0F
	}
	if _, err := DecryptCreds(string(b)); err == nil {
		t.Fatal("篡改密文必须解密失败（GCM 完整性校验）")
	}
	// 垃圾输入。
	if _, err := DecryptCreds("not-base64!!!"); err == nil {
		t.Fatal("非法 base64 必须失败")
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	withKey(t)
	enc, _ := EncryptCreds(&Creds{User: "a", Pass: "b"})
	// 换密钥（模拟密钥轮换后旧密文）。
	t.Setenv("STORAGE_CIPHER_KEY", "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100")
	resetCipherKey()
	if _, err := DecryptCreds(enc); err == nil {
		t.Fatal("异密钥解密必须失败")
	}
}

func TestNormalizeConn(t *testing.T) {
	// webdav
	if _, err := normalizeConn("webdav", conn{URL: "http://nas.local/dav"}); err != nil {
		t.Fatalf("合法 webdav 被拒: %v", err)
	}
	if _, err := normalizeConn("webdav", conn{URL: "ftp://x"}); err == nil {
		t.Fatal("ftp 协议必须拒绝")
	}
	if _, err := normalizeConn("webdav", conn{}); err == nil {
		t.Fatal("空 url 必须拒绝")
	}
	// smb/nfs 必填
	if _, err := normalizeConn("smb", conn{Host: "h"}); err == nil {
		t.Fatal("smb 缺 share 必须拒绝")
	}
	if _, err := normalizeConn("nfs", conn{Host: "h", Export: "/vol"}); err != nil {
		t.Fatalf("合法 nfs 被拒: %v", err)
	}
	if _, err := normalizeConn("bogus", conn{}); err != nil {
		t.Fatalf("未知类型的 conn 无必填校验应放行: %v", err)
	}
}

func TestCredsMask_Constant(t *testing.T) {
	if credMask == "" || credMask == "s3cret" {
		t.Fatal("掩码常量异常")
	}
}

func TestMain(m *testing.M) {
	// 保险：单测进程绝不留缓存密钥。
	resetCipherKey()
	os.Exit(m.Run())
}
