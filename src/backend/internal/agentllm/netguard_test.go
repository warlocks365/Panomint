package agentllm

import (
	"context"
	"net"
	"strings"
	"testing"
)

// validateUpstreamURL 字面量层的拒绝用例。
// 覆盖三类：协议滥用 / 缺主机 / 内网与环回字面量 IP。
func TestValidateUpstreamURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr string // 空串 = 期望通过
	}{
		// ---- 通过 ----
		{"公网 https", "https://api.openai.com/v1", ""},
		{"公网 http", "http://api.example.com", ""},
		{"带端口", "https://api.example.com:8443/v1", ""},
		{"公网 IP 字面量", "http://8.8.8.8:80", ""},

		// ---- 协议滥用（H-1 的原始绕过面）----
		{"file 协议", "file:///etc/passwd", "仅允许 http/https"},
		{"gopher 协议", "gopher://127.0.0.1:6379/_SET", "仅允许 http/https"},
		{"dict 协议", "dict://127.0.0.1:11211/stat", "仅允许 http/https"},
		{"ftp 协议", "ftp://example.com/x", "仅允许 http/https"},
		{"缺协议", "api.example.com/v1", "仅允许 http/https"},

		// ---- 环回 / 本机 ----
		{"环回 IPv4", "http://127.0.0.1:5432/", "本机或内网"},
		{"环回别名 localhost", "http://localhost:8080/", ""}, // 字面量非 IP，域名交 dialer 判定
		{"环回 IPv6", "http://[::1]:8080/", "本机或内网"},
		{"0.0.0.0", "http://0.0.0.0:9000/", "本机或内网"},

		// ---- 云元数据（必须拦：拿到即等于拿到云账号权限）----
		{"AWS 元数据", "http://169.254.169.254/latest/meta-data/", "本机或内网"},
		{"GCP 元数据", "http://169.254.169.254/computeMetadata/v1/", "本机或内网"},
		{"阿里云元数据", "http://100.100.100.200/latest/meta-data/", "本机或内网"},

		// ---- RFC1918 私网 ----
		{"10 段", "http://10.0.0.1/", "本机或内网"},
		{"172.16 段", "http://172.16.0.1/", "本机或内网"},
		{"192.168 段", "http://192.168.1.1/", "本机或内网"},
		{"IPv6 ULA", "http://[fd00::1]/", "本机或内网"},
		{"IPv6 链路本地", "http://[fe80::1]/", "本机或内网"},

		// ---- 其它 ----
		{"空串", "", "地址为空"},
		{"纯空格", "   ", "地址为空"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateUpstreamURL(tc.in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("期望通过，实际被拒: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("期望拒绝（%s），实际通过", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("拒绝理由不含 %q，实际: %v", tc.wantErr, err)
			}
		})
	}
}

// isBlockedIP 的分段判定。
func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"10.1.2.3", "172.16.0.1", "172.31.255.255", "192.168.0.1",
		"169.254.169.254", "fe80::1", "fd00::1",
		"0.0.0.0", "::",
		// CGNAT 100.64.0.0/10 —— Go 的 IsPrivate() 不覆盖，必须手工判。
		// 阿里云元数据 100.100.100.200 正在此段（实测漏网点）。
		"100.64.0.0", "100.100.100.200", "100.127.255.255",
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("测试数据 %q 不是合法 IP", s)
		}
		if !isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = false，期望 true", s)
		}
	}

	allowed := []string{
		"8.8.8.8", "1.1.1.1", "172.32.0.1", // 172.32 已出 /12 私网段
		"172.15.0.1", // 172.15 在 /12 之下，属公网
		"100.63.255.255", // CGNAT 段下界之外（100.63 不是 CGNAT）
		"100.128.0.1",   // CGNAT 段上界之外
		"2001:4860:4860::8888",
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("测试数据 %q 不是合法 IP", s)
		}
		if isBlockedIP(ip) {
			t.Errorf("isBlockedIP(%s) = true，期望 false", s)
		}
	}
}

// guardedTransport 的 dialer 是 SSRF 防线里最关键的一层（DNS 解析后判定）。
// 这里验证：字面量内网 IP 在拨号那一刻被拦，且错误信息可辨识。
func TestGuardedTransportBlocksInternalDial(t *testing.T) {
	tr := guardedTransport()
	if tr == nil || tr.DialContext == nil {
		t.Fatal("guardedTransport 未装配 DialContext")
	}

	// 环回：必须被拦（且不能真的建立连接）
	_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	if err == nil {
		t.Fatal("拨号 127.0.0.1 竟成功了 —— SSRF 防线失效")
	}
	if !strings.Contains(err.Error(), "已拒绝") {
		t.Fatalf("拒绝理由不含「已拒绝」，实际: %v", err)
	}

	// 云元数据地址：最关键的一条，必须拦
	_, err = tr.DialContext(context.Background(), "tcp", "169.254.169.254:80")
	if err == nil {
		t.Fatal("拨号 169.254.169.254 竟成功了 —— SSRF 防线失效")
	}
}

// 反向断言：域名形态的 localhost 在字面量层放行（由 dialer 负责），
// 明确记录这个分层职责，避免后人误以为 validateUpstreamURL 漏了 localhost。
func TestLocalhostHandledByDialerNotValidator(t *testing.T) {
	if _, err := validateUpstreamURL("http://localhost:8080/"); err != nil {
		t.Fatalf("localhost 应放行到 dialer 层判定，实际被字面量层拦下: %v", err)
	}
	// 真实拨号时 localhost 会解析成 127.0.0.1 并被拦 —— 这里只验证判定函数本身。
	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", "localhost")
	if err != nil {
		t.Skipf("本环境无法解析 localhost，跳过：%v", err)
	}
	sawLoopback := false
	for _, ip := range ips {
		if isBlockedIP(ip) {
			sawLoopback = true
			break
		}
	}
	if !sawLoopback {
		t.Skip("localhost 未解析到环回地址（取决于本机 hosts 配置），跳过")
	}
}
