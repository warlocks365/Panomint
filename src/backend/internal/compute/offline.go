package compute

import (
	"os"
	"strconv"
	"time"
)

// 离线判定：**放在查询侧算，不写定时任务**。
//
// 设计依据：TDD v1.1 §6.1「每 60s 发送 {type:"heartbeat", ...}；3 次未收到 → 标记 offline」。
// 阈值因此是 3 × 60s = 180s（DefaultOfflineAfter），而不是单次心跳周期。
//
// 为什么不做定时任务：为 C 阶段再引入一个常驻进程/goroutine 去周期性写库，会带来
// 「谁来保证它活着」「多副本同时扫同一批行」「进程重启后状态如何收敛」三个新问题，
// 而收益仅仅是让库里的 status 更"实时"。改为每次读节点时按 last_heartbeat 现算，
// 一个纯函数即可，状态天然收敛、无并发写、无重启迁移问题。

// EffectiveStatus 按心跳超时把「存储状态」修正为「生效状态」。
//
// 规则（顺序即优先级）：
//  1. stored == offline → offline。管理端显式下线是最高优先级：即便节点还在心跳，
//     也不该因为它的心跳就"自己复活"——下线是人的决定，必须由人（或再次 PATCH）解除。
//  2. lastHeartbeat == nil → offline。登记过但从未心跳，等价于没上线。
//  3. now - lastHeartbeat > offlineAfter → offline。取严格大于：恰好落在阈值上视为仍在线，
//     避免边界抖动（时钟精度 + 网络抖动下，">=" 会让一个刚好准时的节点随机闪断）。
//  4. 否则保持 stored（online 或 busy）。
func EffectiveStatus(stored Status, lastHeartbeat *time.Time, now time.Time, offlineAfter time.Duration) Status {
	if stored == StatusOffline {
		return StatusOffline
	}
	if lastHeartbeat == nil {
		return StatusOffline
	}
	if now.Sub(*lastHeartbeat) > offlineAfter {
		return StatusOffline
	}
	return stored
}

// OfflineAfterFromEnv 读取 COMPUTE_OFFLINE_AFTER_SECONDS 覆盖离线阈值。
//
// 提供这个开关是因为阈值与部署网络质量强相关（跨网段/代理环境下 60s 心跳可能抖动），
// 但它不改变 TDD 的默认语义：缺省、非法、非正、或小于一个心跳周期的值
// （小于心跳周期会让正常节点在两拍之间被误判离线）一律回落到 DefaultOfflineAfter。
func OfflineAfterFromEnv() time.Duration {
	s := os.Getenv("COMPUTE_OFFLINE_AFTER_SECONDS")
	if s == "" {
		return DefaultOfflineAfter
	}
	sec, err := strconv.Atoi(s)
	if err != nil || sec <= 0 {
		return DefaultOfflineAfter
	}
	d := time.Duration(sec) * time.Second
	if d < DefaultHeartbeatInterval {
		return DefaultOfflineAfter
	}
	return d
}
