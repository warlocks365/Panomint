// Package agentllm 的 SSRF 防线（Job000141，审查报告 H-1）。
//
// 为什么需要：base_url 由**管理员在界面上填**（PUT /admin/agent/llm-config），
// 服务端会拿它发真实 HTTP 请求（TestConfig 的 GET {base}/models、ProxyChat 的
// POST {base}/chat/completions）。「管理员可配 URL」等价于「给了一个面向内网的
// 请求原语」—— 一旦 admin 账号被弱口令/会话劫持拿下，攻击面直达内网与其他云服务。
// 因此按 SSRF 威胁模型处置，而不是「管理员自己 responsibly 配置」的信任模型。
//
// 三道防线（缺一不可，单层都可被绕过）：
//
//  1. validateUpstreamURL —— 字面量检查：协议白名单（仅 http/https）+ 主机非空。
//     挡住 `file:///etc/passwd`、`gopher://`、`dict://` 这类协议滥用。
//  2. samePrivate —— 目标 IP 判定：环回/私网/链路本地/未指定地址一律拒绝。
//     挡住 `http://127.0.0.1:5432/`、`http://169.254.169.254/`（云元数据）、
//     `http://10.x/`、`http://192.168.x/`、`http://[::1]/`。
//  3. guardedTransport —— 出站 dialer：**域名解析后再判**。这是最关键的一层：
//     `http://evil.com` 若把 A 记录指向 `127.0.0.1`，前两层都看不出问题
//     （第 2 层只看字面量 IP），只有拨号那一刻才知道真实目的地址。
//     即「DNS 重绑定 / rebinding」的标准防御位置。
//
// 已知取舍与演进（2026-10-11 用户裁决）：
//
//	最初此处**一刀切拒绝所有私网**，理由是「管理员可配 URL」等价于「面向内网的请求原语」。
//	但本项目有**真实且必要的本地大模型部署需求**（上游 llama.cpp 在 192.168.1.42:8888），
//	一刀切使该场景完全不可用。已改为**按 host 精确白名单**：
//
//	环境变量 AGENT_LLM_ALLOWED_HOSTS = "192.168.1.42,llama.internal"（逗号分隔，可含端口）
//	  · 默认空 = **行为与原来完全一致**（仍拒绝全部私网），FailClosed 优先；
//	  · 列出的 host 才允许指向私网/环回；未列出的一律拒绝；
//	  · 精确匹配而非「放开私网段」——admin 仍拿不到任意内网请求能力，
//	    除非运维在环境变量里显式加入某个 host。
//
//	⚠️ 云元数据地址（169.254.169.254 / 100.100.100.200 / 192.0.0.192）
//	   **永远不可加入白名单**——拿到即等于拿到云账号权限，与本地模型需求无关。
//	   loadAllowedHosts 已硬性排除，见 metadataHosts。
package agentllm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// upstreamTimeout 上游请求超时。与 Handler.client() 的 120s 保持一致。
const upstreamTimeout = 120 * time.Second

// allowedHostsEnv 私网 host 白名单环境变量名。
const allowedHostsEnv = "AGENT_LLM_ALLOWED_HOSTS"

// metadataHosts 云厂商元数据地址：**任何情况下都不得**加入白名单。
//
// 为什么单独硬编码：白名单是运维可控的输入，而元数据服务返回的是实例角色的
// 临时凭证（AWS IAM / GCP 服务账号 / 阿里云 RAM）。一旦放行，攻陷 admin 就等于
// 拿到云账号权限，比访问内网其它服务严重一个量级。这与「本地模型在私网」是两件事，
// 不应被同一个开关顺带打开。
var metadataHosts = map[string]struct{}{
	"169.254.169.254": {}, // AWS / GCP / Azure / OpenStack 通用
	"100.100.100.200": {}, // 阿里云
	"192.0.0.192":      {}, // Oracle Cloud
}

var (
	allowedHostsMu sync.RWMutex
	allowedHosts   = loadAllowedHosts()
)

// loadAllowedHosts 从环境变量读取白名单。
//
// 解析规则：逗号/分号/空格分隔；每项可写 host 或 host:port；host 大小写不敏感；
// 项内带 :port 时要求实际端口也一致才算命中（避免「只为某端口开洞」被误当成
// 「整个 host 都开」）；空项忽略；元数据地址静默丢弃。
func loadAllowedHosts() map[string]string {
	raw := os.Getenv(allowedHostsEnv)
	out := make(map[string]string)
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	}) {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		host, port := item, ""
		if h, p, err := net.SplitHostPort(item); err == nil {
			host, port = h, p
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		if host == "" {
			continue
		}
		if _, bad := metadataHosts[host]; bad {
			continue // 元数据地址：永不放入白名单
		}
		out[host] = port
	}
	return out
}

// allowedHostSnapshot 返回当前白名单副本（诊断与测试用）。
func allowedHostSnapshot() map[string]string {
	allowedHostsMu.RLock()
	defer allowedHostsMu.RUnlock()
	out := make(map[string]string, len(allowedHosts))
	for k, v := range allowedHosts {
		out[k] = v
	}
	return out
}

// hostAllowed 判定某 host（可含端口）是否被显式白名单放行。
//
// 只认精确匹配：白名单写 192.168.1.42 就只放行这个地址，不放行 192.168.1.0/24。
func hostAllowed(hostport string) bool {
	h, port, err := net.SplitHostPort(hostport)
	if err != nil {
		h, port = hostport, ""
	}
	h = strings.ToLower(strings.Trim(h, "[]"))
	if h == "" {
		return false
	}
	// 元数据地址永不放行：即使误配进环境变量也不认。
	if _, bad := metadataHosts[h]; bad {
		return false
	}
	allowedHostsMu.RLock()
	want, ok := allowedHosts[h]
	allowedHostsMu.RUnlock()
	if !ok {
		return false
	}
	// 白名单未指定端口 = 该 host 任意端口；指定了则必须一致。
	return want == "" || want == port
}

// validateUpstreamURL 校验上游 base_url：仅 http/https + 主机名合法。
//
// 只做**字面量**层面的判定（协议 + 直接写出的 IP）。域名不在此层判定 ——
// 那需要解析，且解析结果随时可变（重绑定），判定必须放在拨号那一刻（见 guardedTransport）。
// 这里提前挡掉协议滥用与「直接写内网 IP」这两类最粗的滥用，让错误在配置时立刻可见。
//
// 私网例外（2026-10-11）：host 命中 AGENT_LLM_ALLOWED_HOSTS 白名单时放行，
// 以支持本地大模型部署；未命中仍按原逻辑拒绝。详见 loadAllowedHosts。
func validateUpstreamURL(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("地址为空")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("地址格式非法")
	}
	// 协议白名单：只放行 http/https。file:// 可读本地文件、gopher:// 可打 Redis
	// 等，均属协议滥用（Go 的 http.Client 本就只支持 http/https，但显式拒绝
	// 能在配置入口就给出明确错误，而不是等到出站时才失败）。
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("仅允许 http/https 协议")
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("缺少主机名")
	}
	// 字面量 IP：直接判定（127.0.0.1 / 169.254.169.254 / [::1] / 10.x / 192.168.x …）。
	// 域名不在此拦截，交由 dialer 在解析后判定。
	if ip := net.ParseIP(host); ip != nil && isBlockedIP(ip) {
		if hostAllowed(u.Host) {
			return u, nil
		}
		return nil, fmt.Errorf("禁止指向本机或内网地址（如需访问本地模型，请将主机加入环境变量 %s）", allowedHostsEnv)
	}
	return u, nil
}

// isBlockedIP 判定一个 IP 是否属于「不该由本服务主动访问」的目标。
//
// 覆盖：
//   - 环回（含 127.0.0.0/8、::1）
//   - 私网（10/8、172.16/12、192.168/16、fc00::/7）
//   - 链路本地（169.254/16，含云元数据 169.254.169.254；fe80::/10）
//   - CGNAT（100.64.0.0/10）—— ⚠️ **Go 的 IsPrivate() 不涵盖此段**，必须手工判
//   - 未指定地址（0.0.0.0、::）
//
// ⚠️ 两条必须手工加的段（都不是 Go 标准库能覆盖的，且都是真实攻击面）：
//   169.254.0.0/16 —— 云环境元数据服务（AWS/GCP 的 169.254.169.254），
//     拿到凭证即等于拿到云账号权限。
//   100.64.0.0/10  —— **阿里云元数据服务是 100.100.100.200**（CGNAT 段内）。
//     这个地址是实测发现的：最初只依赖 IsPrivate()/IsLinkLocalUnicast()，
//     单元测试立刻逮到阿里云那条漏网（AWS/GCP 两条已覆盖）。
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	// CGNAT：100.64.0.0/10（100.64.0.0 – 100.127.255.255）。
	// 阿里云元数据 100.100.100.200 正在此段。
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
	}
	return false
}

// guardedTransport 返回带内网拦截的 Transport。
//
// DialContext 先解析目标主机，把**解析结果**逐个送 isBlockedIP 判定；
// 只要有一个 A/AAAA 记录落在内网/环回/链路本地，整次拨号立即失败。
//
// 为什么必须放这里（而不是只做 URL 字面量校验）：
//
//	攻击者控制 DNS，先把 evil.example 解析到公网 IP 通过校验，
//	再在第二次请求把 A 记录改成 127.0.0.1 —— 这是 DNS rebinding。
//	字面量校验完全看不出变化，只有「真正拨号的那一刻」才知道真实目的地。
//
// 判定放在解析后、拨号前，中间不留窗口：拿到 ips 后立即判，立即返回错误。
//
// 私网例外（2026-10-11）：host 命中 AGENT_LLM_ALLOWED_HOSTS 白名单时跳过拦截，
// 以支持本地大模型部署。白名单按 addr（含端口）判定，且对
// 「字面量 IP」与「域名解析到私网」两条路径同等生效 —— 后者是关键：
// 白名单里写主机名、它解析到 192.168.x.x 时，字面量层已放行，此处若不认白名单
// 就会把刚放行的合法上游重新拦掉。元数据地址在任何分支都不放行。
func guardedTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			// 白名单按原始 addr（含端口）判定一次，结果整条拨号复用 ——
			// 必须在这里算，不能等到判定内网时才算，否则「host 在白名单但域名
			// 解析到私网」这条路径会漏判（此时 host 字面量不是 IP，上面已放过）。
			allowed := hostAllowed(addr)
			// 若是字面量 IP，无需解析，直接判定。
			if ip := net.ParseIP(host); ip != nil {
				if isBlockedIP(ip) && !allowed {
					return nil, fmt.Errorf("目标 %s 属于本机/内网地址，已拒绝", ip)
				}
			} else {
				ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
				if err != nil {
					return nil, err
				}
				if len(ips) == 0 {
					return nil, fmt.Errorf("目标 %s 未解析到任何地址", host)
				}
				for _, ip := range ips {
					if isBlockedIP(ip) && !allowed {
						// 任何一个记录落在内网就整体拒绝：多记录主机只要有一条
						// 指向内网，攻击者就能用它当跳板。
						return nil, fmt.Errorf("目标 %s 解析到本机/内网地址 %s，已拒绝", host, ip)
					}
				}
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		MaxIdleConns:          32,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// guardedClient 默认上游客户端：出站经 SSRF 防线。
var guardedClient = &http.Client{
	Timeout:   upstreamTimeout,
	Transport: guardedTransport(),
}
