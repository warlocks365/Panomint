// Package storage 网络挂载管理（Job000070 F4）。
//
// 凭据红线：挂载凭据（密码/token）是敏感数据——
//   - 明文只存在于创建/更新请求的内存中，AES-256-GCM 加密后立即丢弃；
//   - 密文落库（creds_enc），密钥来自 env STORAGE_CIPHER_KEY（32 字节，hex 编码），
//     绝不入库、绝不进日志、绝不通过 API 输出（列表/详情只回 has_creds）；
//   - 未配置密钥时，一切带凭据的写操作 FailClosed（400 CIPHER_KEY_MISSING），
//     无凭据挂载（guest/nfs）不受限。
package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// ErrNoCipherKey 未配置 STORAGE_CIPHER_KEY 却尝试加密凭据。
var ErrNoCipherKey = errors.New("STORAGE_CIPHER_KEY 未配置")

// ErrCiphertext 密文形态非法（解密失败/被篡改/密钥轮换后旧密文）。
var ErrCiphertext = errors.New("凭据密文不可解密")

// cipherKey 进程级缓存的密钥（惰性加载一次）。
var cipherKey []byte

// loadCipherKey 从 env 读取并解码 32 字节密钥（hex）。未配置或长度错→nil。
func loadCipherKey() []byte {
	if cipherKey != nil {
		return cipherKey
	}
	raw := os.Getenv("STORAGE_CIPHER_KEY")
	if raw == "" {
		return nil
	}
	k, err := hex.DecodeString(raw)
	if err != nil || len(k) != 32 {
		return nil
	}
	cipherKey = k
	return cipherKey
}

// CipherKeyConfigured 密钥可用性（handler 用于 400 前置提示）。
func CipherKeyConfigured() bool { return loadCipherKey() != nil }

// encrypt 加密明文 → base64(nonce||ciphertext)。GCM 自带完整性校验。
func encrypt(plaintext []byte) (string, error) {
	k := loadCipherKey()
	if k == nil {
		return "", ErrNoCipherKey
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// decrypt 解密 base64(nonce||ciphertext)。失败统一 ErrCiphertext（不区分原因防 Oracle）。
func decrypt(s string) ([]byte, error) {
	k := loadCipherKey()
	if k == nil {
		return nil, ErrNoCipherKey
	}
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, ErrCiphertext
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return nil, ErrCiphertext
	}
	pt, err := gcm.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, ErrCiphertext
	}
	return pt, nil
}

// Creds 挂载凭据明文（请求体内短暂存在；json 序列化用于加密前的规范化）。
type Creds struct {
	User   string `json:"user"`
	Pass   string `json:"pass"`
	Domain string `json:"domain,omitempty"`
}

// EncryptCreds 凭据明文 → 密文。无凭据（全空）→ 空串（无凭据挂载）。
func EncryptCreds(c *Creds) (string, error) {
	if c == nil || (c.User == "" && c.Pass == "" && c.Domain == "") {
		return "", nil
	}
	b, err := jsonMarshal(c)
	if err != nil {
		return "", err
	}
	return encrypt(b)
}

// DecryptCreds 密文 → 凭据明文（仅 worker 执行器本地使用，绝不回传 API）。
func DecryptCreds(enc string) (*Creds, error) {
	if enc == "" {
		return nil, nil
	}
	b, err := decrypt(enc)
	if err != nil {
		return nil, err
	}
	var c Creds
	if err := jsonUnmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCiphertext, err)
	}
	return &c, nil
}
