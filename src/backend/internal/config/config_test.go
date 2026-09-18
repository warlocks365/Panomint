package config

import (
	"regexp"
	"strings"
	"testing"
)

// ipv4Re 匹配任意 IPv4 字面量。
var ipv4Re = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)

// credentialRe 匹配 URL 里"用户名:口令@"这种内嵌凭据形态。
var credentialRe = regexp.MustCompile(`://[^/@:]+:[^/@]+@`)

// TestDefaultsAreGeneric 是本文件最重要的一条：它把"默认值必须是通用且安全的"变成可执行的断言。
//
// 为什么用"非环回 IP"而不是"不含任何 IP"：`127.0.0.1` 恰恰是**通用部署**该有的默认值
// （本机），要禁的是"指向**另一台**机器"—— 历史上 PG_DSN 默认指向某台内网机器，
// 导致任何人在别处忘设环境变量就会连到别人的库。
func TestDefaultsAreGeneric(t *testing.T) {
	cases := map[string]string{
		"DefaultPGDSN":      DefaultPGDSN,
		"DefaultValkeyAddr": DefaultValkeyAddr,
		"DefaultMediaRoot":  DefaultMediaRoot,
		"DefaultCORS":       DefaultCORS,
	}
	for name, v := range cases {
		// ① 不得指向"另一台机器"：出现的 IPv4 只允许是环回地址。
		for _, ip := range ipv4Re.FindAllString(v, -1) {
			if !strings.HasPrefix(ip, "127.") {
				t.Errorf("%s 的默认值里出现了非环回地址 %q（应改为 127.0.0.1 或由环境变量提供）：%q", name, ip, v)
			}
		}
		// ② 不得内嵌凭据。
		if credentialRe.MatchString(v) {
			t.Errorf("%s 的默认值里内嵌了「用户名:口令@」形态的凭据：%q", name, v)
		}
	}

	// ③ MediaRoot 不得再指向测试数据目录（否则生产误用会把测试数据当媒体根）。
	if strings.Contains(DefaultMediaRoot, "testdata") {
		t.Errorf("DefaultMediaRoot 不得指向测试数据目录：%q", DefaultMediaRoot)
	}
}

// TestSplitOrigins 覆盖真实边界缺陷：`strings.Split("", ",")` 返回 `[""]` 而不是空切片，
// 于是"显式把 CORS_ORIGINS 设为空"会多出一个空串允许源条目 —— 语义上应当是"不放行任何跨域源"。
func TestSplitOrigins(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},                             // 显式置空 ⇒ 不放行任何跨域源
		{",", []string{}},                            // 只有分隔符
		{"  ,  ", []string{}},                        // 只有空白与分隔符
		{"http://a.test", []string{"http://a.test"}}, // 单个
		{"http://a.test, http://b.test", []string{"http://a.test", "http://b.test"}},  // 容忍空格
		{"http://a.test,,http://b.test,", []string{"http://a.test", "http://b.test"}}, // 丢弃空段
	}
	for _, c := range cases {
		got := splitOrigins(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitOrigins(%q) = %v（%d 项），期望 %v（%d 项）", c.in, got, len(got), c.want, len(c.want))
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitOrigins(%q)[%d] = %q，期望 %q", c.in, i, got[i], c.want[i])
			}
		}
	}
	// 反向自证：空输入绝不能产出"含一个空串"的切片（这正是修复前的行为）。
	if got := splitOrigins(""); len(got) != 0 {
		t.Errorf("空输入必须得到 0 项，实际 %d 项：%v", len(got), got)
	}
}

// TestProdGateRejectsDefaults 生产环境仍用默认值时必须报错（启动即失败，
// 而不是静默连到 127.0.0.1 上的陌生库）。
func TestProdGateRejectsDefaults(t *testing.T) {
	// 清掉可能存在的 .env 影响：显式设成默认值本身。
	t.Setenv("APP_ENV", EnvProd)
	t.Setenv("PG_DSN", DefaultPGDSN)
	t.Setenv("VALKEY_ADDR", DefaultValkeyAddr)
	t.Setenv("MEDIA_ROOT", DefaultMediaRoot)

	_, err := Load()
	if err == nil {
		t.Fatal("APP_ENV=prod 且三项关键配置都是默认值时必须返回错误，实际没有")
	}
	// 错误信息要点名是哪些键没设 —— 否则使用者不知道去改什么。
	msg := err.Error()
	for _, key := range []string{"PG_DSN", "VALKEY_ADDR", "MEDIA_ROOT"} {
		if !strings.Contains(msg, key) {
			t.Errorf("错误信息应点名 %s，实际：%s", key, msg)
		}
	}
	// 错误信息不得出现凭据形态（错误本身也会进日志采集）。
	if credentialRe.MatchString(msg) {
		t.Errorf("错误信息里出现了凭据形态：%s", msg)
	}
}

// TestProdGateAcceptsExplicitConfig 生产环境显式配置后必须放行。
func TestProdGateAcceptsExplicitConfig(t *testing.T) {
	t.Setenv("APP_ENV", EnvProd)
	t.Setenv("PG_DSN", "postgres://someone@example.internal:5432/pano_album")
	t.Setenv("VALKEY_ADDR", "valkey.internal:6379")
	t.Setenv("MEDIA_ROOT", "/data/media")

	if _, err := Load(); err != nil {
		t.Fatalf("生产环境显式设置了全部关键项后不应报错，实际：%v", err)
	}
}

// TestDevAllowsDefaults 开发/未设置环境用默认值必须放行 ——
// 加了门禁不能把本地开发堵死（本项目已多次强调"别把正常路径打死"）。
func TestDevAllowsDefaults(t *testing.T) {
	for _, env := range []string{"", "dev", "test", "staging"} {
		t.Setenv("APP_ENV", env)
		t.Setenv("PG_DSN", DefaultPGDSN)
		t.Setenv("VALKEY_ADDR", DefaultValkeyAddr)
		t.Setenv("MEDIA_ROOT", DefaultMediaRoot)

		if _, err := Load(); err != nil {
			t.Errorf("APP_ENV=%q 用默认值不应报错，实际：%v", env, err)
		}
	}
}

// TestLoadParsesOriginsFromEnv 确认 CORS_ORIGINS 从环境变量取值时同样过滤空段。
func TestLoadParsesOriginsFromEnv(t *testing.T) {
	t.Setenv("APP_ENV", "dev")
	t.Setenv("PG_DSN", DefaultPGDSN)
	t.Setenv("VALKEY_ADDR", DefaultValkeyAddr)
	t.Setenv("MEDIA_ROOT", DefaultMediaRoot)
	t.Setenv("CORS_ORIGINS", "  , http://a.test ,")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load 失败：%v", err)
	}
	if len(c.CORSOrigins) != 1 || c.CORSOrigins[0] != "http://a.test" {
		t.Errorf("CORSOrigins 解析错误：%v（期望恰好 [http://a.test]）", c.CORSOrigins)
	}
}
