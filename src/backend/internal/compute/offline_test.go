package compute

import (
	"testing"
	"time"
)

// TestEffectiveStatus 离线判定的全边界。
//
// 规则（见 offline.go）：**加锁的**强制 offline > 从未心跳 > 超时 > 心跳新鲜；
// 超时取**严格大于**，恰好等于阈值仍算在线。
//
// ⚠️ 本测试在 T6.2 缺陷修复中改过：原来最后三条用的是「存储为 offline」，
// 想表达「管理员强制下线不可被心跳推翻」。但缺陷正是「初始 offline」与「强制 offline」
// 共用同一个值——新登记节点落库即 offline，于是被永久判为离线。
// 引入 status_locked 后，这三条改为 locked 版本，并新增「未加锁 offline + 新鲜心跳 → online」。
func TestEffectiveStatus(t *testing.T) {
	now := time.Now()
	within := now.Add(-time.Second)                     // 刚心跳过
	exact := now.Add(-DefaultOfflineAfter)              // 恰好落在阈值上
	over := now.Add(-DefaultOfflineAfter - time.Second) // 超出阈值 1s

	cases := []struct {
		name          string
		stored        Status
		locked        bool
		lastHeartbeat *time.Time
		want          Status
	}{
		{"刚心跳过 + online → online", StatusOnline, false, &within, StatusOnline},
		{"刚心跳过 + busy → busy", StatusBusy, false, &within, StatusBusy},
		{"从未心跳（nil）+ online → offline", StatusOnline, false, nil, StatusOffline},
		{"从未心跳（nil）+ busy → offline", StatusBusy, false, nil, StatusOffline},
		{"超时 + online → offline", StatusOnline, false, &over, StatusOffline},
		{"超时 + busy → offline", StatusBusy, false, &over, StatusOffline},
		{"恰好等于阈值 → 仍 online（取严格大于）", StatusOnline, false, &exact, StatusOnline},
		{"恰好等于阈值 → 仍 busy", StatusBusy, false, &exact, StatusBusy},

		// 状态锁：加锁 = 管理员显式置过状态，心跳不得推翻。
		{"加锁的 offline + 心跳新鲜 → 仍 offline（管理员强制下线不可被心跳推翻）", StatusOffline, true, &within, StatusOffline},
		{"加锁的 offline + 从未心跳 → offline", StatusOffline, true, nil, StatusOffline},
		{"加锁的 offline + 超时 → offline", StatusOffline, true, &over, StatusOffline},
		{"加锁的 busy + 心跳新鲜 → busy（锁只挡 offline 被推翻，不改写 busy）", StatusBusy, true, &within, StatusBusy},
		{"加锁的 online + 心跳新鲜 → online", StatusOnline, true, &within, StatusOnline},

		// 缺陷修复的核心回归点：注册默认值就是 offline（未加锁），心跳后必须上线。
		{"未加锁的 offline + 心跳新鲜 → online（新登记节点心跳后必须上线）", StatusOffline, false, &within, StatusOnline},
		{"未加锁的 offline + 恰好等于阈值 → 仍 online", StatusOffline, false, &exact, StatusOnline},
		{"未加锁的 offline + 从未心跳 → offline（登记后还没心跳过）", StatusOffline, false, nil, StatusOffline},
		{"未加锁的 offline + 超时 → offline", StatusOffline, false, &over, StatusOffline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EffectiveStatus(tc.stored, tc.locked, tc.lastHeartbeat, now, DefaultOfflineAfter); got != tc.want {
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
	// 用未加锁的 online 存储态：说明超时判定与状态锁无关，两者是独立的一层。
	if got := EffectiveStatus(StatusOnline, false, &hb, now, DefaultOfflineAfter); got != StatusOnline {
		t.Fatalf("默认阈值下应为 online，实际 %q", got)
	}
	if got := EffectiveStatus(StatusOnline, false, &hb, now, 60*time.Second); got != StatusOffline {
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
