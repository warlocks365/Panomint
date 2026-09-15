package compute

import (
	"testing"
	"time"
)

// TestEffectiveStatus 离线判定的全边界。
//
// 规则（见 offline.go）：显式下线 > 从未心跳 > 超时 > 保持存储态；
// 超时取**严格大于**，恰好等于阈值仍算在线。
func TestEffectiveStatus(t *testing.T) {
	now := time.Now()
	within := now.Add(-time.Second)                  // 刚心跳过
	exact := now.Add(-DefaultOfflineAfter)           // 恰好落在阈值上
	over := now.Add(-DefaultOfflineAfter - time.Second) // 超出阈值 1s

	cases := []struct {
		name          string
		stored        Status
		lastHeartbeat *time.Time
		want          Status
	}{
		{"刚心跳过 + online → online", StatusOnline, &within, StatusOnline},
		{"刚心跳过 + busy → busy", StatusBusy, &within, StatusBusy},
		{"从未心跳（nil）+ online → offline", StatusOnline, nil, StatusOffline},
		{"从未心跳（nil）+ busy → offline", StatusBusy, nil, StatusOffline},
		{"超时 + online → offline", StatusOnline, &over, StatusOffline},
		{"超时 + busy → offline", StatusBusy, &over, StatusOffline},
		{"恰好等于阈值 → 仍 online（取严格大于）", StatusOnline, &exact, StatusOnline},
		{"恰好等于阈值 → 仍 busy", StatusBusy, &exact, StatusBusy},
		{"存储为 offline + 心跳新鲜 → 仍 offline（人的下线决定优先）", StatusOffline, &within, StatusOffline},
		{"存储为 offline + 从未心跳 → offline", StatusOffline, nil, StatusOffline},
		{"存储为 offline + 超时 → offline", StatusOffline, &over, StatusOffline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveStatus(tc.stored, tc.lastHeartbeat, now, DefaultOfflineAfter); got != tc.want {
				t.Fatalf("EffectiveStatus() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestEffectiveStatusCustomThreshold 自定义阈值（COMPUTE_OFFLINE_AFTER_SECONDS 生效时）。
func TestEffectiveStatusCustomThreshold(t *testing.T) {
	now := time.Now()
	hb := now.Add(-90 * time.Second)

	// 90s 前的心跳：默认阈值 180s 下在线；阈值收到 60s 后离线。
	if got := EffectiveStatus(StatusOnline, &hb, now, DefaultOfflineAfter); got != StatusOnline {
		t.Fatalf("默认阈值下应为 online，实际 %q", got)
	}
	if got := EffectiveStatus(StatusOnline, &hb, now, 60*time.Second); got != StatusOffline {
		t.Fatalf("60s 阈值下应为 offline，实际 %q", got)
	}
}

// TestOfflineAfterFromEnv 环境变量覆盖：缺省/合法/非法/非正/小于心跳周期。
func TestOfflineAfterFromEnv(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"未设置 → 默认 180s", "", DefaultOfflineAfter},
		{"合法值 90s → 采用", "90", 90 * time.Second},
		{"合法值 300s → 采用", "300", 300 * time.Second},
		{"非数字 → 回落默认", "abc", DefaultOfflineAfter},
		{"零 → 回落默认", "0", DefaultOfflineAfter},
		{"负数 → 回落默认", "-30", DefaultOfflineAfter},
		{"小于一个心跳周期（60s）→ 回落默认，避免正常节点在两拍之间被误判离线", "30", DefaultOfflineAfter},
		{"恰好等于心跳周期 60s → 采用", "60", 60 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("COMPUTE_OFFLINE_AFTER_SECONDS", tc.env)
			if got := OfflineAfterFromEnv(); got != tc.want {
				t.Fatalf("OfflineAfterFromEnv() = %s，期望 %s", got, tc.want)
			}
		})
	}
}

// TestProtocolTimingConstants 协议时间常量必须与 TDD §6.1 一致（60s 心跳 / 3 次未收到 → offline）。
func TestProtocolTimingConstants(t *testing.T) {
	if DefaultHeartbeatInterval != 60*time.Second {
		t.Fatalf("心跳周期应为 60s（TDD §6.1），实际 %s", DefaultHeartbeatInterval)
	}
	if DefaultOfflineAfter != 180*time.Second {
		t.Fatalf("离线阈值应为 3 × 60s = 180s（TDD §6.1「3 次未收到」），实际 %s", DefaultOfflineAfter)
	}
}
