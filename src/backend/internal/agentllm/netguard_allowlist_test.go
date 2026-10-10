package agentllm

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 私网白名单（2026-10-11 用户裁决：必须支持本地大模型部署）
//
// 背景：一刀切拒绝私网导致 llama.cpp（192.168.1.42:8888）这类**本地部署模型**
// 完全无法配置。改为按 host 精确白名单：
//   · 默认空 = 行为与原来完全一致（FailClosed）；
//   · 白名单内的 host 放行私网/环回；
//   · 云元数据地址**永远**不可放行（拿到即等于拿到云账号权限）。
//
// 这些用例直接跑 t.Setenv + 重新 loadAllowedHosts()，因为白名单在包初始化时
// 从环境变量读取；测试必须能构造不同的环境配置，而不是依赖开发机环境。
// ---------------------------------------------------------------------------

// withAllowedHosts 临时设置白名单环境变量并重建白名单，返回清理函数。
func withAllowedHosts(t *testing.T, v string) {
	t.Helper()
	t.Setenv(allowedHostsEnv, v)

	allowedHostsMu.Lock()
	prev := allowedHosts
	allowedHosts = loadAllowedHosts()
	allowedHostsMu.Unlock()

	t.Cleanup(func() {
		allowedHostsMu.Lock()
		allowedHosts = prev
		allowedHostsMu.Unlock()
	})
}

func TestLoadAllowedHostsParsing(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want map[string]string // host -> 期望端口（空串 = 任意端口）
	}{
		{"空 = 无白名单", "", map[string]string{}},
		{"纯空格 = 无白名单", "   ", map[string]string{}},
		{"单个私网 IP", "192.168.1.42", map[string]string{"192.168.1.42": ""}},
		{"host:port 形式", "192.168.1.42:8888", map[string]string{"192.168.1.42": "8888"}},
		{"逗号分隔多项", "192.168.1.42,10.0.0.5", map[string]string{
			"192.168.1.42": "", "10.0.0.5": "",
		}},
		{"分号与空格混用", "192.168.1.42; 10.0.0.5:11434", map[string]string{
			"192.168.1.42": "", "10.0.0.5": "11434",
		}},
		{"IPv6 回环", "[::1]:8080", map[string]string{"::1": "8080"}},
		{"大小写不敏感", "LOCALHOST", map[string]string{"localhost": ""}},
		// 🔴 元数据地址必须被静默丢弃，即使运维误配
		{"AWS 元数据被拒", "169.254.169.254", map[string]string{}},
		{"阿里云元数据被拒", "100.100.100.200", map[string]string{}},
		{"Oracle 元数据被拒", "192.0.0.192", map[string]string{}},
		{"元数据与正常项混合", "192.168.1.42,169.254.169.254", map[string]string{"192.168.1.42": ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(allowedHostsEnv, tc.env)
			defer os.Unsetenv(allowedHostsEnv)
			got := loadAllowedHosts()
			if len(got) != len(tc.want) {
				t.Fatalf("白名单条数 = %d，期望 %d（实际 %v）", len(got), len(tc.want), got)
			}
			for h, p := range tc.want {
				gp, ok := got[h]
				if !ok {
					t.Errorf("缺少 host %q，实际 %v", h, got)
					continue
				}
				if gp != p {
					t.Errorf("host %q 端口 = %q，期望 %q", h, gp, p)
				}
			}
		})
	}
}

func TestHostAllowed(t *testing.T) {
	t.Run("空白名单 = 全部不通过", func(t *testing.T) {
		withAllowedHosts(t, "")
		for _, h := range []string{"192.168.1.42:8888", "127.0.0.1:9", "[::1]:8080", "localhost:8080"} {
			if hostAllowed(h) {
				t.Errorf("空白名单时 hostAllowed(%q) = true，期望 false", h)
			}
		}
	})

	t.Run("精确匹配命中", func(t *testing.T) {
		withAllowedHosts(t, "192.168.1.42")
		if !hostAllowed("192.168.1.42:8888") {
			t.Error("白名单内的 192.168.1.42 应放行")
		}
		// 🔴 关键：精确匹配 —— 同段其它地址**不得**顺带放行
		if hostAllowed("192.168.1.43:8888") {
			t.Error("192.168.1.43 不在白名单，却放行了 —— 白名单退化成放开私网段")
		}
		if hostAllowed("192.168.1.99:1") {
			t.Error("未列出的同段地址不应放行")
		}
	})

	t.Run("指定端口时端口必须一致", func(t *testing.T) {
		withAllowedHosts(t, "192.168.1.42:8888")
		if !hostAllowed("192.168.1.42:8888") {
			t.Error("端口一致时应放行")
		}
		if hostAllowed("192.168.1.42:9999") {
			t.Error("端口不一致却放行了 —— 「只为某端口开洞」失效")
		}
	})

	t.Run("元数据地址永不放行", func(t *testing.T) {
		// 即使明确写进环境变量也必须不放行（loadAllowedHosts 也会丢弃，
		// 这里再加一道断言，防止将来有人改成直接读 map 而绕过丢弃逻辑）。
		withAllowedHosts(t, "169.254.169.254,100.100.100.200,192.0.0.192")
		for _, h := range []string{"169.254.169.254:80", "100.100.100.200:80", "192.0.0.192:80"} {
			if hostAllowed(h) {
				t.Errorf("元数据地址 %s 被放行了 —— 这是最严重的安全回归", h)
			}
		}
	})
}

// 白名单生效时，validateUpstreamURL 必须放行私网 IP（这是用户报的那条报错的修复点）。
func TestValidateUpstreamURLPrivateAllowedByWhitelist(t *testing.T) {
	t.Run("默认仍拒绝私网", func(t *testing.T) {
		withAllowedHosts(t, "")
		// 用户报的原始报错场景
		if _, err := validateUpstreamURL("http://192.168.1.42:8888/v1"); err == nil {
			t.Fatal("空白名单时应仍拒绝私网 —— FailClosed 被破坏了")
		} else if !strings.Contains(err.Error(), "AGENT_LLM_ALLOWED_HOSTS") {
			t.Errorf("报错文案应提示白名单环境变量，实际: %v", err)
		}
	})

	t.Run("白名单内私网放行", func(t *testing.T) {
		withAllowedHosts(t, "192.168.1.42")
		if _, err := validateUpstreamURL("http://192.168.1.42:8888/v1"); err != nil {
			t.Fatalf("白名单内的私网应放行，实际被拒: %v", err)
		}
	})

	t.Run("白名单内回环放行", func(t *testing.T) {
		withAllowedHosts(t, "127.0.0.1")
		if _, err := validateUpstreamURL("http://127.0.0.1:11434/v1"); err != nil {
			t.Fatalf("白名单内的回环应放行（本地 Ollama 场景），实际被拒: %v", err)
		}
	})

	t.Run("白名单不含时仍拒绝其它私网", func(t *testing.T) {
		withAllowedHosts(t, "192.168.1.42")
		if _, err := validateUpstreamURL("http://10.0.0.5:11434/v1"); err == nil {
			t.Fatal("未列入白名单的 10 段地址应仍被拒")
		}
	})

	t.Run("元数据地址永远拒绝", func(t *testing.T) {
		withAllowedHosts(t, "169.254.169.254")
		if _, err := validateUpstreamURL("http://169.254.169.254/latest/meta-data/"); err == nil {
			t.Fatal("云元数据地址必须永远拒绝 —— 这是最严重的安全回归")
		}
	})

	t.Run("协议白名单不因私网放行而失效", func(t *testing.T) {
		withAllowedHosts(t, "127.0.0.1,192.168.1.42")
		for _, bad := range []string{
			"file:///etc/passwd",
			"gopher://127.0.0.1:6379/_SET",
			"ftp://192.168.1.42/x",
		} {
			if _, err := validateUpstreamURL(bad); err == nil {
				t.Errorf("%s 应仍被协议白名单拒绝", bad)
			}
		}
	})
}

// 白名单必须在**拨号层**同样生效 —— 否则「白名单写主机名、它解析到私网」
// 这条路径会在 validateUpstreamURL 放行后又被 dialer 拦掉（合法上游被误杀）。
// 反过来，未在白名单的仍必须被拦。
func TestGuardedTransportWithWhitelist(t *testing.T) {
	t.Run("白名单内回环不再被拨号层拦（能真正连出去）", func(t *testing.T) {
		withAllowedHosts(t, "127.0.0.1:9")
		tr := guardedTransport()
		// 端口 9 无监听 → 期望「连接被拒绝」而非「SSRF 已拒绝」。
		// 这里只断言**不是** SSRF 拦截，不要求真的连上。
		_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:9")
		if err != nil && strings.Contains(err.Error(), "已拒绝") {
			t.Fatalf("白名单内的回环仍被 SSRF 层拒绝: %v", err)
		}
	})

	t.Run("白名单外仍被拨号层拦", func(t *testing.T) {
		withAllowedHosts(t, "192.168.1.42")
		tr := guardedTransport()
		_, err := tr.DialContext(context.Background(), "tcp", "127.0.0.1:9")
		if err == nil {
			t.Fatal("白名单外的回回环应仍被拒绝")
		}
		if !strings.Contains(err.Error(), "已拒绝") {
			t.Fatalf("拒绝理由不含「已拒绝」，实际: %v", err)
		}
	})

	t.Run("元数据地址在拨号层仍被拦", func(t *testing.T) {
		withAllowedHosts(t, "169.254.169.254")
		tr := guardedTransport()
		_, err := tr.DialContext(context.Background(), "tcp", "169.254.169.254:80")
		if err == nil {
			t.Fatal("云元数据地址必须永远被拒")
		}
	})
}

// 反向验证（mutation）：把 hostAllowed 的白名单判定短路成「恒 false」，
// 上面的「放行」用例应立刻变红。人工确认此性质后移除。
func TestHostAllowedRejectsWhenWhitelistEmptyRegressionGuard(t *testing.T) {
	withAllowedHosts(t, "")
	if hostAllowed("192.168.1.42:8888") {
		t.Fatal("空白名单必须拒绝所有 host")
	}
	// 反证：同样条件下 isBlockedIP 判定为真，说明拒绝来自白名单而非 IP 判定
	// 本身 —— 两者职责不同，都必须保留。
	if !isBlockedIP(net.ParseIP("192.168.1.42")) {
		t.Fatal("192.168.1.42 应仍属 isBlockedIP 判定范围（判定函数本身不应被放宽）")
	}
}
