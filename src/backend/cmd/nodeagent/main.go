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
//	-media-root  media.path 相对路径的解析根，可重复（顺序即优先级）
//	-hls-dir     HLS 输出根目录（-executor local 时必填；亦可用 HLS_DIR）
//	-heartbeat   心跳周期，如 60s                            默认 60s（与 TDD §6.1 一致）
//
// ⚠️ 本程序**不含任何节点地址、凭据或路径的默认值**：-server 与 -token 必须由使用者提供，
// -media-root / -hls-dir 也是（除非用同名环境变量）。这是项目的长期红线——本地 GPU /
// 局域网第三方 GPU / 云 GPU 一律平权，不允许把某台具体机器写进软件
// （开发期用于验证的那台机器只是"一个普通节点"）。路径同理：它是节点相关的部署事实。
//
// ⚠️ -device cuda 在探测不到 NVENC 时**不会报错退出**，而是标记 has_nvenc=false
// 并以 CPU 模式继续工作（GPU/CPU 双接口原则，见 internal/compute/capabilities.go）。
//
// ⚠️ 领到什么任务类型由**控制端**决定（`COMPUTE_CLAIMABLE_KINDS`，默认仅 noop），
// 节点侧只负责"领到的任务能不能干"：-executor local 支持 noop 与 hls，
// 遇到不支持的 kind 会**显式回传 failed**，不会假装成功。
//
// 示例（令牌来自登记响应，注意不要写进脚本历史）：
//
//	nodeagent -server http://<控制端>:8080 -token <令牌> -device auto -concurrency 2 \
//	          -executor local -media-root /srv/media -hls-dir /srv/hls
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"panoalbum/internal/compute"
)

// stringList 可重复的字符串标志（用于 -media-root；顺序即探测优先级）。
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	if v = strings.TrimSpace(v); v != "" {
		*s = append(*s, v)
	}
	return nil
}

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
	hlsDir := fs.String("hls-dir", envOr("HLS_DIR", ""), "HLS 输出根目录（-executor local 时必填）")
	heartbeat := fs.Duration("heartbeat", compute.DefaultHeartbeatInterval, "心跳周期（如 60s）")
	var mediaRoots stringList
	fs.Var(&mediaRoots, "media-root", "media.path 相对路径的解析根，可重复（顺序即优先级）")
	fs.Parse(os.Args[1:])

	if *server == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "错误：-server 与 -token 均为必填（本程序不含任何默认地址/凭据）")
		fs.Usage()
		os.Exit(2)
	}

	// 未显式给 -media-root 时回落到环境变量，**顺序与 cmd/transcodectl 一致**：
	// 上传目录优先，其次既有索引根。顺序不一致会让同一个 media.path 在两边解析到不同文件
	// —— 那是最难查的一类"偶发转错素材"。
	if len(mediaRoots) == 0 {
		for _, v := range []string{os.Getenv("UPLOAD_DIR"), os.Getenv("MEDIA_ROOT")} {
			if strings.TrimSpace(v) != "" {
				mediaRoots = append(mediaRoots, v)
			}
		}
	}

	var executor compute.Executor
	switch *executorName {
	case "noop":
		executor = compute.NewNoopExecutor()
	case "local":
		// 本地执行器要读源文件、写 HLS 产出，两个路径缺一不可。宁可在启动时报错退出，
		// 也不要让它跑起来、领到任务、每个都失败 —— 后者会安静地浪费整条队列。
		if *hlsDir == "" {
			fmt.Fprintln(os.Stderr, "错误：-executor local 需要 -hls-dir（或 HLS_DIR）")
			os.Exit(2)
		}
		if len(mediaRoots) == 0 {
			fmt.Fprintln(os.Stderr, "错误：-executor local 需要至少一个 -media-root（或 UPLOAD_DIR / MEDIA_ROOT）；"+
				"节点必须能看到源文件（同机或挂载共享存储）")
			os.Exit(2)
		}
		le := compute.NewLocalExecutor(*device)
		le.MediaRoots = mediaRoots
		le.HLSDir = *hlsDir
		executor = le
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
