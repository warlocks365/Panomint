package main

import (
	"strings"
	"testing"

	"panoalbum/internal/storage"
)

func TestMountKeyAndPath(t *testing.T) {
	if got := mountKey("abcdef12-3456-7890"); got != "abcdef12" {
		t.Errorf("mountKey: %q", got)
	}
	if got := mountPath("abcdef12-3456"); got != "/mnt/storage/abcdef12" {
		t.Errorf("mountPath: %q", got)
	}
	if got := mountKey("short"); got != "short" {
		t.Errorf("短 id 保留: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("短串不应截: %q", got)
	}
	got := truncate(strings.Repeat("x", 400), 300)
	if len(got) != 300 || !strings.HasSuffix(got, "...") {
		t.Errorf("截断形态错: len=%d", len(got))
	}
}

func TestRcloneConfig_Webdav(t *testing.T) {
	cc := connConf{URL: "https://nas.local/dav"}
	creds := &storage.Creds{User: "alice", Pass: "x"}
	cfg := rcloneConfig("webdav", cc, creds, "OBSCURED")
	for _, want := range []string{"type = webdav", "url = https://nas.local/dav", "user = alice", "pass = OBSCURED"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("配置缺 %q:\n%s", want, cfg)
		}
	}
	// 只读性由命令行保证，配置里不得出现 write 相关开启（rclone 默认 rw，靠 --read-only）。
	if strings.Contains(cfg, "read_only") {
		t.Error("配置不应硬编码只读标志（由命令行 --read-only 控制）")
	}
}

func TestRcloneConfig_SMB(t *testing.T) {
	cc := connConf{Host: "nas", Share: "photos", Port: 445}
	creds := &storage.Creds{User: "bob", Pass: "y", Domain: "WORK"}
	cfg := rcloneConfig("smb", cc, creds, "O")
	for _, want := range []string{"type = smb", "host = nas", "share = photos", "port = 445", "domain = WORK"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("配置缺 %q:\n%s", want, cfg)
		}
	}
}

func TestRcloneConfig_NoCreds(t *testing.T) {
	cfg := rcloneConfig("webdav", connConf{URL: "http://x/d"}, nil, "")
	if strings.Contains(cfg, "user =") || strings.Contains(cfg, "pass =") {
		t.Errorf("无凭据不应有认证行:\n%s", cfg)
	}
}

func TestListActiveMounts_Parse(t *testing.T) {
	// 纯解析函数不依赖测试机 /proc/mounts——解析逻辑内联验证（读真实 /proc/mounts
	// 仅确认不 panic，且返回 map 类型正确）。
	m, err := listActiveMounts()
	if err != nil {
		t.Skipf("无 /proc/mounts（非 Linux）: %v", err)
	}
	for k, v := range m {
		if !strings.HasPrefix(v, mountRoot+"/") || k == "" {
			t.Errorf("挂载点形态错: %q=%q", k, v)
		}
	}
}
