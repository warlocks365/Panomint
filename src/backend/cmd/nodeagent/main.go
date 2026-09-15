// nodeagent 算力节点 agent：把任意一台机器（本机 / 局域网第三方主机 / 云主机）
// 接入控制端作为算力节点，负责心跳、拉取任务、执行、回传结果（TDD v1.1 §6.1）。
//
// 用法：
//
//	nodeagent -server <控制端地址> -token <节点接入令牌> [选项]
//
// 选项：
//
//	-server      控制端地址，如 http://192.168.1.115:8080   （必填，无默认值）
//	-token       节点接入令牌，登记 lan_agent 节点时返回    （必填；亦可用 NODE_TOKEN 环境变量）
//	-name        节点名，仅用于日志                          默认取主机名
//	-device      cpu|cuda|auto                              默认 auto
//	-concurrency 并发任务数                                 默认 1
//	-executor    noop|local                                 默认 noop
//	-heartbeat   心跳周期，如 60s                            默认 60s（与 TDD §6.1 一致）
//
// ⚠️ 本程序**不含任何节点地址或凭据的默认值**：-server 与 -token 必须由使用者提供。
// 这是项目的长期红线——本地 GPU / 局域网第三方 GPU / 云 GPU 一律平权，
// 不允许把某台具体机器写进软件（开发期用于验证的那台机器只是"一个普通节点"）。
//
// ⚠️ -device cuda 在探测不到 NVENC 时**不会报错退出**，而是标记 has_nvenc=false
// 并以 CPU 模式继续工作（GPU/CPU 双接口原则，见 internal/compute/capabilities.go）。
//
// 示例（令牌来自登记响应，注意不要写进脚本历史）：
//
//	nodeagent -server http://<控制端>:8080 -token <令牌> -device auto -concurrency 2
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"panoalbum/internal/compute"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[nodeagent] ")

	fs := flag.NewFlagSet("nodeagent", flag.ExitOnError)
	server := fs.String("server", envOr("NODE_SERVER", ""), "控制端地址，如 http://host:8080（必填）")
	token := fs.String("token", envOr("NODE_TOKEN", ""), "节点接入令牌（必填）")
	name := fs.String("name", envOr("NODE_NAME", ""), "节点名（默认取主机名）")
	device := fs.String("device", envOr("NODE_DEVICE", compute.DeviceAuto), "算力设备 cpu|cuda|auto")
	concurrency := fs.Int("concurrency", 1, "并发任务数")
	executorName := fs.String("executor", "noop", "任务执行器 noop|local")
	heartbeat := fs.Duration("heartbeat", compute.DefaultHeartbeatInterval, "心跳周期（如 60s）")
	fs.Parse(os.Args[1:])

	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "错误：-server 与 -token 均为必填（本程序不含任何默认地址/凭据）")
		fs.Usage()
		os.Exit(2)
	}

	var executor compute.Executor
	switch *executorName {
	case "noop":
		executor = compute.NewNoopExecutor()
	case "local":
		executor = compute.NewLocalExecutor(*device)
	default:
		fmt.Fprintf(os.Stderr, "错误：-executor %q 非法，需为 noop|local\n", *executorName)
		os.Exit(2)
	}

	agent, err := compute.NewAgent(compute.AgentConfig{
		ServerURL:         *server,
		Token:             *token,
		NodeName:          *name,
		Device:            *device,
		Concurrency:       *concurrency,
		Executor:          executor,
		HeartbeatInterval: *heartbeat,
		OfflineAfter:      compute.OfflineAfterFromEnv(),
		Logger:            log.Default(),
	})
	if err != nil {
		log.Fatalf("启动参数错误: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := agent.Run(ctx); err != nil {
		log.Fatalf("agent 退出: %v", err)
	}
	log.Print("agent 已停止")
}

// envOr 取环境变量，为空时用默认值。
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
