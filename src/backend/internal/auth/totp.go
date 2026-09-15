package auth

// TOTP（RFC 6238）与 HOTP（RFC 4226）实现。
//
// # 为什么自己实现而不引第三方库
//
// 算法本体只需要 HMAC-SHA1 + base32 + 一次模运算，标准库已经完全够用。
// 为本项目对**依赖面**一向保守（ffmpeg 只走 subprocess、Redis 换 Valkey、
// 人脸弃用 insightface 换 Apache-2.0 的 OpenCV Zoo），而一个 60 行的纯算法
// 引入第三方包，只会扩大供应链与许可审查面 —— 收益不成比例。
//
// # 与认证器的兼容性
//
// 严格按 RFC 实现，因此与 Google Authenticator / Authy / 1Password / Bitwarden 等互通：
//   - 密钥是 **base32（RFC 4648 标准字母表、无填充）** —— 带 '=' 填充的密钥会被多数认证器拒绝；
//   - 默认 **30s 步长、6 位数字、HMAC-SHA1**（认证器生态的事实标准）；
//   - 动态截断按 RFC 4226 §5.3：offset 取最后一字节低 4 位，从 offset 起取 4 字节后掩掉最高位。
//
// # 正确性依据
//
// 有**官方向量**可对：RFC 6238 附录 B 给出了 20 字节 ASCII 密钥
// `12345678901234567890` 在若干时刻的 8 位口令。单测直接跑这些向量
// （见 totp_test.go），所以"实现对不对"不靠自证，而靠标准文档。

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// TOTPPeriod 步长；RFC 6238 推荐 30s，也是认证器生态的默认值。
	TOTPPeriod = 30 * time.Second

	// TOTPDigits 口令位数；主流认证器默认 6 位。
	TOTPDigits = 6

	// TOTPSkewSteps 允许的步数偏移（±1 步 = 接受前/后各一个 30s 窗口）。
	//
	// 为什么必须留窗口：客户端时钟与服务端不可能严格同步，且用户在口令即将过期时
	// 输入、网络往返后到达服务端，都可能已经跨到下一个窗口 —— 不留窗口会表现为
	// "有时能登、有时登不上"，是最难排查也最伤用户的一类问题。
	//
	// 为什么只留 ±1：每放宽一步，可用口令就从 1 个变成 2k+1 个，暴力猜测成功率线性上升。
	// ±1 是安全性与可用性的通行折中。
	TOTPSkewSteps = 1

	// totpSecretBytes 生成密钥的随机字节数。RFC 4226 建议 HMAC-SHA1 密钥不短于 128 bit，
	// 取 160 bit 与哈希输出等长。
	totpSecretBytes = 20

	// minTOTPDigits / maxTOTPDigits 可接受的口令位数。
	// 下限 4 太弱、上限 10 会让模数溢出错位，故显式设边界并拒绝越界值。
	minTOTPDigits = 4
	maxTOTPDigits = 10
)

// base32NoPad TOTP 密钥编码器：RFC 4648 标准字母表、**无填充**。
//
// ⚠️ 生成与解析必须共用这一个编码器：若生成时无填充、解析时按标准（带填充）解，
// 会得到"能生成但永远校验不过"的密钥，且错误现象是空泛的 base32 解码失败。
var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret 生成新的 base32 密钥（无填充）。
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("auth: 生成 TOTP 密钥失败: %w", err)
	}
	return base32NoPad.EncodeToString(buf), nil
}

// decodeSecret 把用户可见的密钥还原成字节。
//
// 容错：认证器在界面上常把密钥按 4 位分组显示、或小写粘贴，故这里去掉空白与连字符、
// 统一大写后再解码 —— 用户手抄密钥时极易带上这些字符。
func decodeSecret(secret string) ([]byte, error) {
	s := strings.ToUpper(strings.TrimSpace(secret))
	s = strings.NewReplacer(" ", "", "\t", "", "-", "").Replace(s)
	if s == "" {
		return nil, fmt.Errorf("auth: TOTP 密钥为空")
	}
	key, err := base32NoPad.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("auth: TOTP 密钥不是合法 base32: %w", err)
	}
	return key, nil
}

// hotp 按 RFC 4226 计算 counter 对应的动态口令。
func hotp(key []byte, counter uint64, digits int) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	// 动态截断（RFC 4226 §5.3）：offset = 最后一字节低 4 位；
	// 取 4 字节后与 0x7f 掩码（去掉最高位，避免符号歧义）。
	offset := sum[len(sum)-1] & 0x0f
	val := uint64(sum[offset]&0x7f)<<24 |
		uint64(sum[offset+1])<<16 |
		uint64(sum[offset+2])<<8 |
		uint64(sum[offset+3])

	mod := uint64(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	// 左侧补零：RFC 要求定长输出（如 6 位时 "012345" 不能变成 "12345"）。
	return fmt.Sprintf("%0*d", digits, val%mod)
}

// TOTP 计算 t 时刻的口令（digits <= 0 时取 TOTPDigits）。
func TOTP(secret string, t time.Time, digits int) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	digits, err = normalizeOTPDigits(digits)
	if err != nil {
		return "", err
	}
	unix := t.Unix()
	if unix < 0 {
		// 1970 之前没有有意义的 TOTP；显式拒绝而不是让 uint64 转换把负数变成天文数字。
		return "", fmt.Errorf("auth: TOTP 不接受 1970 之前的时间")
	}
	return hotp(key, uint64(unix)/uint64(TOTPPeriod.Seconds()), digits), nil
}

// normalizeOTPDigits 归一化并校验位数。
func normalizeOTPDigits(digits int) (int, error) {
	if digits <= 0 {
		return TOTPDigits, nil
	}
	if digits < minTOTPDigits || digits > maxTOTPDigits {
		return 0, fmt.Errorf("auth: TOTP 位数 %d 超出支持范围 %d..%d", digits, minTOTPDigits, maxTOTPDigits)
	}
	return digits, nil
}

// VerifyTOTP 校验口令是否有效（允许 ±TOTPSkewSteps 步偏差）。
//
// 返回值 matchedStep 是**命中的时间步**（自 epoch 起的步序号），供调用方做防重放与审计；
// 未通过时返回 0。调用方通常只关心 ok。
//
// 实现要点：
//   - 口令先做容错归一（认证器显示为 "123 456" 时用户会连空格一起抄）；
//   - 比较用 crypto/subtle 常量时间比较，避免通过响应时间逐位试探；
//   - 逐窗口比较时**不做短路**（即使已匹配也继续算完），否则"第几个窗口命中"会从耗时上泄漏。
func VerifyTOTP(secret, code string, now time.Time) (uint64, bool, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return 0, false, err
	}
	digits, err := normalizeOTPDigits(0)
	if err != nil {
		return 0, false, err
	}
	norm := normalizeOTPCode(code)
	if len(norm) != digits {
		return 0, false, nil
	}
	unix := now.Unix()
	if unix < 0 {
		return 0, false, fmt.Errorf("auth: TOTP 不接受 1970 之前的时间")
	}
	current := int64(unix) / int64(TOTPPeriod.Seconds())

	var matched uint64
	found := false
	for off := int64(-TOTPSkewSteps); off <= int64(TOTPSkewSteps); off++ {
		step := current + off
		if step < 0 {
			continue
		}
		want := hotp(key, uint64(step), digits)
		// 常量时间比较；先算完再决定，避免命中位置影响耗时。
		if subtle.ConstantTimeCompare([]byte(want), []byte(norm)) == 1 && !found {
			matched = uint64(step)
			found = true
		}
	}
	return matched, found, nil
}

// normalizeOTPCode 去掉口令里的空白与连字符（认证器界面的分组显示会带上它们）。
func normalizeOTPCode(code string) string {
	return strings.NewReplacer(" ", "", "\t", "", "-", "").Replace(strings.TrimSpace(code))
}

// OTPAuthURL 生成供认证器扫码/手动录入的 otpauth:// 链接（Key Uri Format）。
//
// issuer 是显示在认证器里的服务名，account 通常是邮箱 —— 两者都出现在链接的 label 里，
// 故必须 url 转义（邮箱里的 '@' 可保留，但服务名里的空格/斜杠必须转义）。
func OTPAuthURL(issuer, account, secret string, digits int) string {
	if d, err := normalizeOTPDigits(digits); err == nil {
		digits = d
	} else {
		digits = TOTPDigits
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(digits))
	q.Set("period", strconv.Itoa(int(TOTPPeriod.Seconds())))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}
