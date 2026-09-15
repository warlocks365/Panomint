package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 附录 B 的官方测试向量。
//
// 这是本文件的核心：TOTP 的"实现对不对"不该由我自己论证，而应对齐标准文档给出的
// 已知答案。密钥是 ASCII 字符串 "12345678901234567890"（20 字节），
// 下表的 8 位口令即 RFC 原文值 —— 我们的实现必须逐条命中。
//
// 另注：RFC 的向量是 8 位。6 位是"同一个动态截断值再对 10^6 取模"，
// 即**等于 8 位值的后 6 位**，故同一条向量可以同时校验 6 位实现（见下方断言）。
var rfc6238Vectors = []struct {
	name    string
	unix    int64
	digits8 string // RFC 原文给出的 8 位口令
}{
	{"T=59", 59, "94287082"},
	{"T=1111111109", 1111111109, "07081804"},
	{"T=1111111111", 1111111111, "14050471"},
	{"T=1234567890", 1234567890, "89005924"},
	{"T=2000000000", 2000000000, "69279037"},
	{"T=20000000000", 20000000000, "65353130"},
}

// rfc6238SecretASCII 是 RFC 6238 附录 B 用的 20 字节 ASCII 密钥。
const rfc6238SecretASCII = "12345678901234567890"

// rfc6238SecretBase32 是上面那段 ASCII 的 base32（无填充）表示。
const rfc6238SecretBase32 = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

// TestRFC6238OfficialVectors 逐条命中 RFC 6238 附录 B 的官方向量。
func TestRFC6238OfficialVectors(t *testing.T) {
	// 先自检：向量里那段 base32 必须真的是那段 ASCII —— 否则下面测的是"另一把钥匙"，
	// 会给出看似通过实则错误的结论。
	raw, err := base32NoPad.DecodeString(rfc6238SecretBase32)
	if err != nil {
		t.Fatalf("向量密钥不是合法 base32: %v", err)
	}
	if string(raw) != rfc6238SecretASCII {
		t.Fatalf("向量密钥与 RFC 的 ASCII 密钥不一致：解码得到 %q，期望 %q", string(raw), rfc6238SecretASCII)
	}

	for _, c := range rfc6238Vectors {
		t.Run(c.name, func(t *testing.T) {
			at := time.Unix(c.unix, 0).UTC()

			got8, err := TOTP(rfc6238SecretBase32, at, 8)
			if err != nil {
				t.Fatalf("TOTP(8) 报错: %v", err)
			}
			if got8 != c.digits8 {
				t.Fatalf("8 位口令与 RFC 6238 不符：得到 %s，RFC 给出 %s", got8, c.digits8)
			}

			// 6 位 = 8 位值对 10^6 取模 = 后 6 位。
			got6, err := TOTP(rfc6238SecretBase32, at, 6)
			if err != nil {
				t.Fatalf("TOTP(6) 报错: %v", err)
			}
			if want := c.digits8[len(c.digits8)-6:]; got6 != want {
				t.Fatalf("6 位口令应为 8 位值的后 6 位：得到 %s，期望 %s", got6, want)
			}
			if len(got6) != 6 {
				t.Fatalf("6 位口令必须定长补零，实际 %q（长度 %d）", got6, len(got6))
			}
		})
	}
}

// TestTOTPStableWithinPeriod 同一个 30s 窗口内口令必须不变，跨窗口必须变。
//
// 这条守的是"步长到底有没有真的生效"：若误用秒级计数器，窗口内就会不停变化，
// 用户会看到"刚输入就过期"。
func TestTOTPStableWithinPeriod(t *testing.T) {
	base := time.Unix(1234567890, 0).UTC()
	a, _ := TOTP(rfc6238SecretBase32, base, 6)
	b, _ := TOTP(rfc6238SecretBase32, base.Add(29*time.Second), 6)
	if a != b {
		t.Fatalf("同一 30s 窗口内口令应相同：%s vs %s", a, b)
	}
	c, _ := TOTP(rfc6238SecretBase32, base.Add(30*time.Second), 6)
	if a == c {
		t.Fatal("跨窗口后口令应改变（相邻窗口恰好相同的概率极低，此处视为步长未生效）")
	}
}

// TestVerifyTOTPAcceptsSkewWindow 允许 ±1 步偏差、拒绝 ±2 步。
//
// 窗口的必要性：客户端时钟偏差与网络往返会让"本地算出的码"落到相邻窗口。
// 窗口的上限意义：每放宽一步，可用口令数量线性增长，暴力空间随之变小。
func TestVerifyTOTPAcceptsSkewWindow(t *testing.T) {
	now := time.Unix(2000000000, 0).UTC()

	cases := []struct {
		name   string
		at     time.Time
		wantOK bool
	}{
		{"当前窗口", now, true},
		{"上一个窗口（时钟略慢）", now.Add(-TOTPPeriod), true},
		{"下一个窗口（时钟略快）", now.Add(+TOTPPeriod), true},
		{"上两个窗口（超出容差）", now.Add(-2 * TOTPPeriod), false},
		{"下两个窗口（超出容差）", now.Add(+2 * TOTPPeriod), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, err := TOTP(rfc6238SecretBase32, c.at, 6)
			if err != nil {
				t.Fatalf("生成口令失败: %v", err)
			}
			_, ok, err := VerifyTOTP(rfc6238SecretBase32, code, now)
			if err != nil {
				t.Fatalf("校验报错: %v", err)
			}
			if ok != c.wantOK {
				t.Fatalf("偏差 %s 的校验结果应为 %v，实际 %v", c.at.Sub(now), c.wantOK, ok)
			}
		})
	}
}

// TestVerifyTOTPReturnsMatchedStep 命中时必须返回命中的时间步（供防重放/审计使用）。
func TestVerifyTOTPReturnsMatchedStep(t *testing.T) {
	now := time.Unix(2000000000, 0).UTC()
	prev := now.Add(-TOTPPeriod)
	code, _ := TOTP(rfc6238SecretBase32, prev, 6)

	step, ok, err := VerifyTOTP(rfc6238SecretBase32, code, now)
	if err != nil || !ok {
		t.Fatalf("应校验通过，实际 ok=%v err=%v", ok, err)
	}
	want := uint64(prev.Unix()) / uint64(TOTPPeriod.Seconds())
	if step != want {
		t.Fatalf("应返回命中的步 %d（上一个窗口），实际 %d", want, step)
	}
}

// TestVerifyTOTPRejectsBadInput 畸形输入必须被拒，且不得误通过。
func TestVerifyTOTPRejectsBadInput(t *testing.T) {
	now := time.Unix(2000000000, 0).UTC()
	good, _ := TOTP(rfc6238SecretBase32, now, 6)

	cases := []struct {
		name string
		code string
	}{
		{"空串", ""},
		{"位数不足", good[:5]},
		{"位数过多", good + "0"},
		{"非数字", "abcdef"},
		{"全零（几乎不可能命中）", "000000"},
		{"带非法字符", "12a456"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok, err := VerifyTOTP(rfc6238SecretBase32, c.code, now); ok {
				t.Fatalf("口令 %q 不应通过校验", c.code)
			} else if err != nil && c.name != "空串" {
				// 非空畸形输入应安静地判不通过，而不是抛错打断登录流程
				t.Fatalf("口令 %q 应判为不通过而不是报错: %v", c.code, err)
			}
		})
	}

	// 正确口令仍须通过（防止上面的"一律拒绝"把功能测死）。
	if _, ok, _ := VerifyTOTP(rfc6238SecretBase32, good, now); !ok {
		t.Fatal("正确口令未通过校验")
	}
}

// TestNormalizeOTPCodeToleratesFormatting 认证器界面按 4 位分组显示（"123 456"），
// 用户手抄常带上空格/连字符/小写 —— 这些都应被容错。
func TestNormalizeOTPCodeToleratesFormatting(t *testing.T) {
	now := time.Unix(2000000000, 0).UTC()
	code, _ := TOTP(rfc6238SecretBase32, now, 6)
	formatted := code[:3] + " " + code[3:]

	if _, ok, _ := VerifyTOTP(rfc6238SecretBase32, formatted, now); !ok {
		t.Fatalf("带空格的输入 %q 应被容错接受", formatted)
	}

	// 密钥侧的容错：小写 + 分组 + 连字符
	lower := strings.ToLower(rfc6238SecretBase32)
	if _, ok, err := VerifyTOTP(lower, code, now); !ok || err != nil {
		t.Fatalf("小写密钥应可用，实际 ok=%v err=%v", ok, err)
	}
}

// TestGenerateTOTPSecret 生成的密钥必须是"认证器能读、且每次不同"的 base32。
func TestGenerateTOTPSecret(t *testing.T) {
	s1, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	s2, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if s1 == s2 {
		t.Fatal("两次生成的密钥不应相同（随机性缺失会让所有用户共用密钥）")
	}
	if strings.Contains(s1, "=") {
		t.Fatalf("密钥不得带 base32 填充（多数认证器会拒绝）：%q", s1)
	}
	raw, err := base32NoPad.DecodeString(s1)
	if err != nil {
		t.Fatalf("生成的密钥必须可被 base32 解码: %v", err)
	}
	if len(raw) != totpSecretBytes {
		t.Fatalf("密钥应为 %d 字节，实际 %d", totpSecretBytes, len(raw))
	}
	// 生成的密钥必须能真正用起来
	now := time.Unix(1700000000, 0).UTC()
	code, err := TOTP(s1, now, 6)
	if err != nil {
		t.Fatalf("用生成的密钥算口令失败: %v", err)
	}
	if _, ok, _ := VerifyTOTP(s1, code, now); !ok {
		t.Fatal("用生成的密钥算出的口令应能通过校验")
	}
}

// TestTOTPDigitsBounds 位数边界：越界必须报错，0 取默认值。
func TestTOTPDigitsBounds(t *testing.T) {
	at := time.Unix(1234567890, 0).UTC()

	if _, err := TOTP(rfc6238SecretBase32, at, 3); err == nil {
		t.Fatal("位数 3 应报错（过短，且不在支持范围）")
	}
	if _, err := TOTP(rfc6238SecretBase32, at, 11); err == nil {
		t.Fatal("位数 11 应报错（会超出 uint64 模数安全范围）")
	}
	got, err := TOTP(rfc6238SecretBase32, at, 0)
	if err != nil {
		t.Fatalf("位数 0 应取默认值，实际报错: %v", err)
	}
	if len(got) != TOTPDigits {
		t.Fatalf("位数 0 应得到 %d 位，实际 %q", TOTPDigits, got)
	}
}

// TestTOTPRejectsPre1970 1970 之前的时间必须报错，而不是被 uint64 转换吞成天文数字。
func TestTOTPRejectsPre1970(t *testing.T) {
	if _, err := TOTP(rfc6238SecretBase32, time.Unix(-1, 0), 6); err == nil {
		t.Fatal("1970 之前的时间应报错")
	}
	if _, _, err := VerifyTOTP(rfc6238SecretBase32, "123456", time.Unix(-1, 0)); err == nil {
		t.Fatal("1970 之前的校验应报错")
	}
}

// TestDecodeSecretRejectsBadBase32 非法密钥必须报错（而不是当成空密钥继续算）。
func TestDecodeSecretRejectsBadBase32(t *testing.T) {
	for _, bad := range []string{"", "   ", "!!!!!!!!", "1"} {
		if _, err := decodeSecret(bad); err == nil {
			t.Fatalf("非法密钥 %q 应报错", bad)
		}
	}
}

// TestOTPAuthURL 链接必须含认证器所需的全部参数，且 label 被正确转义。
func TestOTPAuthURL(t *testing.T) {
	u := OTPAuthURL("Panomint 全景相册", "owner@pano.local", rfc6238SecretBase32, 6)

	if !strings.HasPrefix(u, "otpauth://totp/") {
		t.Fatalf("前缀应为 otpauth://totp/，实际 %q", u)
	}
	for _, want := range []string{
		"secret=" + rfc6238SecretBase32,
		"issuer=",
		"algorithm=SHA1",
		"digits=6",
		"period=30",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("链接缺少 %q：%s", want, u)
		}
	}
	// label 里的空格必须被转义，否则认证器解析出的服务名会截断
	if strings.Contains(u, "otpauth://totp/Panomint 全景相册:") {
		t.Fatalf("label 中的空格未转义：%s", u)
	}
	// 账号名应保留在 label 里
	if !strings.Contains(u, "owner%40pano.local") && !strings.Contains(u, "owner@pano.local") {
		t.Fatalf("label 应包含账号名：%s", u)
	}
}
