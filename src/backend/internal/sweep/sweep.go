// Package sweep 清扫式 watch 主循环骨架（审查 P2-06 收敛）。
//
// 「立即先跑一轮 → ticker 周期跑 → ctx/信号退出」曾是
// cmd/{embedgen,facesgen,phashgen,taggen} 四份近同构代码，现收敛为 Loop。
package sweep

import (
	"context"
	"log"
	"time"
)

// DefaultInterval 未显式指定间隔时的默认值（与四个 cmd 的既有行为一致）。
const DefaultInterval = 30 * time.Second

// Loop 周期执行 fn 直到 ctx 取消：先立即执行一轮，之后每 interval 执行一轮。
// interval<=0 时取 DefaultInterval；name 仅用于退出日志（"收到退出信号，<name>停止"）。
// 各 cmd 的启动日志（含自身配置信息）仍由调用方在打点好后打印。
func Loop(ctx context.Context, interval time.Duration, name string, fn func(ctx context.Context)) {
	if interval <= 0 {
		interval = DefaultInterval
	}
	fn(ctx)

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("收到退出信号，%s停止", name)
			return
		case <-t.C:
			fn(ctx)
		}
	}
}
