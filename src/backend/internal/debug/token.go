package debug

// 双段凭据生成（设计 §3.2）：强制 crypto/rand，禁用 math/rand / 时间戳 / UUID。
// 全部是包级纯函数，便于穷举单测（字符集/长度/1e4 次零重复）。

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"time"
)

// ChannelIDBytes channel_id 随机字节数（12 字节 → 24 位小写 hex，96-bit）。
const ChannelIDBytes = 12

// AccessKeyBytes access_key 随机字节数（32 字节 → 256-bit 熵）。
const AccessKeyBytes = 32

// GenerateChannelID 生成通道标识：12 字节 CSPRNG → 24 位小写 hex。
// 单独知道 channel_id 无任何价值（不敏感段，出现在 URL/日志/审计）。
func GenerateChannelID() (string, error) {
	b := make([]byte, ChannelIDBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateAccessKey 生成调试密钥：32 字节 CSPRNG → base64url（RFC 4648，无 padding）→ 43 字符。
// 字符集 [A-Za-z0-9_-]，URL 安全免编码。明文仅此一次出现在 enable/rotate 响应里。
func GenerateAccessKey() (string, error) {
	b := make([]byte, AccessKeyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashAccessKey 计算密钥的 sha256 hex（64 字符，lower(hex(sha256))，设计 §3.2）。
// 与 internal/compute.HashAgentToken 同构：库中只存摘要，校验先哈希再比对。
func HashAccessKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// KeyValid 校验来访密钥是否匹配库中摘要且通道未过期。
//
//   - storedDigest 为空 → 拒绝（避免空密钥撞上空摘要）；
//   - 过期边界取"到期即失效"（now >= expiresAt 即假），不留 1ns 模糊地带；
//   - subtle.ConstantTimeCompare 防时序侧信道（设计 §3.2）。
func KeyValid(storedDigest, presented string, expiresAt time.Time, now time.Time) bool {
	if storedDigest == "" || presented == "" {
		return false
	}
	if !now.Before(expiresAt) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashAccessKey(presented)), []byte(storedDigest)) == 1
}

// Fingerprint 通道指纹 = channel_id 前 8 位（设计 §7.3）。
// 贯穿审计全链（谁开启→谁接入→执行了哪些命令→何时断开）的可追溯标识；
// **不是**密钥摘要（key_digest 命中脱敏名单 hash 子串，禁入 detail）。
func Fingerprint(channelID string) string {
	if len(channelID) < 8 {
		return channelID
	}
	return channelID[:8]
}
