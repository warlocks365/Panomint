package compute

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// 节点能力探测（节点侧）。
//
// ⚠️ 双接口原则（长期红线）：GPU 与 CPU 两条路径必须同时存在于同一份代码里，
// "开发环境没有 CUDA" **不是**删减 GPU 接口的理由。所以本文件里没有任何
// build tag、没有任何「无 CUDA 就编译掉」的分支：能力差异只体现为**运行时字段**
// （HasNVENC / VRAMMB / Codecs），而不是编译期的有无。
//
// 具体表现为：`-device cuda` 在探测不到 NVENC 时**只降级、不退出**——
// 节点仍以 CPU 模式上线并声明 has_nvenc=false，控制端据此调度。
// 这样"同一台机器换块卡"不需要换二进制，"同一份配置在开发机与正式机上"也不需要分叉。

// DeviceCPU / DeviceCUDA / DeviceAuto 是 -device 的三个合法取值。
const (
	DeviceCPU  = "cpu"
	DeviceCUDA = "cuda"
	DeviceAuto = "auto"
)

// Capabilities 节点自报能力（对应 TDD §6.1 注册时上报的能力声明）。
//
// 目前 Codecs 是启发式的（有 NVENC → h264+hevc，否则只有 h264）。真实编码器清单
// 应以 `ffmpeg -encoders` 探测为准，但那属于接入 ffmpeg 时的工作，本交付项不引入
// ffmpeg 依赖，故先用这条保守规则（宁可少报能力，不可报出做不到的能力）。
type Capabilities struct {
	Codecs      []string
	HasNVENC    bool
	VRAMMB      int
	Concurrency int
}

// CodecsString 拼成库列 compute_nodes.codecs 的格式（varchar，逗号分隔）。
func (c Capabilities) CodecsString() string { return strings.Join(c.Codecs, ",") }

// ParseDevice 规范化 -device 取值。空串视为 auto（"没指定"与"自动挑"等价）。
func ParseDevice(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", DeviceAuto:
		return DeviceAuto, nil
	case DeviceCPU:
		return DeviceCPU, nil
	case DeviceCUDA:
		return DeviceCUDA, nil
	}
	return "", errInvalidDevice(s)
}

// NVProber 探测 NVIDIA 环境的抽象。
//
// 抽成接口只为让"有卡/无卡"两条分支都能被单测覆盖——否则 CUDA 降级这条
// 最关键的安全路径就只能靠手工在真机上验证，而它恰恰是最需要回归的部分。
type NVProber interface {
	Available() bool
	Info() (name string, vramMB int)
}

// DefaultNVProber 通过 nvidia-smi 探测（不链接 CUDA，也不需要 CGO）。
//
// 用 nvidia-smi 而不是 CUDA runtime API：节点 agent 要能在没有任何 CUDA 工具链、
// 甚至没有 NVIDIA 驱动的机器上编译运行（它就是给 CPU 节点用的），
// 为此引入 cgo + 驱动依赖会把"能跑"变成"不一定能编"。
type DefaultNVProber struct{}

// probeTimeout 外部命令超时：驱动异常时 nvidia-smi 可能挂住，不能拖死心跳。
const probeTimeout = 3 * time.Second

// nvidiaSMIPath nvidia-smi 可执行文件路径（可被测试覆盖）。
var nvidiaSMIPath = "nvidia-smi"

// Available nvidia-smi 是否可用。
func (DefaultNVProber) Available() bool {
	_, err := exec.LookPath(nvidiaSMIPath)
	return err == nil
}

// Info 取第一块卡的名称与显存容量；任何失败都返回空值而不是错误
// ——探测失败只意味着"能力未知"，不该让节点起不来。
func (DefaultNVProber) Info() (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, nvidiaSMIPath,
		"--query-gpu=name,memory.total", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return "", 0
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return "", 0
	}
	name := strings.TrimSpace(parts[0])
	vram, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || vram < 0 {
		vram = 0
	}
	return name, vram
}

// DetectCapabilities 按 -device 与本地探测结果得出能力声明。
//
// 规则：
//   - device=cpu  → 不看硬件，直接 CPU 能力（显式要求 CPU 时必须听调用方的）；
//   - device=auto → 探测到 NVENC 就报 GPU 能力，否则 CPU 能力（自动挑，永不失败）；
//   - device=cuda → 探测到 NVENC 报 GPU 能力；**探测不到则降级为 CPU 能力并返回 has_nvenc=false**，
//     **不返回错误、不退出**（双接口原则：要求 GPU 但机器没有，是"降级运行"而不是"起不来"）。
//
// 本函数**从不返回 error**——这是刻意的：能力探测属于"尽力而为"，
// 把它做成可失败会诱使调用方在无卡机器上直接退出，从而违背双接口原则。
func DetectCapabilities(device string, concurrency int, prober NVProber) Capabilities {
	if concurrency <= 0 {
		concurrency = 1
	}
	if prober == nil {
		prober = DefaultNVProber{}
	}

	cpuCaps := Capabilities{Codecs: []string{"h264"}, HasNVENC: false, VRAMMB: 0, Concurrency: concurrency}

	dev, err := ParseDevice(device)
	if err != nil || dev == DeviceCPU {
		// 非法取值按 CPU 处理：能力探测不阻断启动（调用方若要严格校验，
		// 应在 NewAgent 里用 ParseDevice 报错，而不是在这里挂掉节点）。
		return cpuCaps
	}
	if !prober.Available() {
		return cpuCaps
	}
	_, vram := prober.Info()
	return Capabilities{
		Codecs:      []string{"h264", "hevc"},
		HasNVENC:    true,
		VRAMMB:      vram,
		Concurrency: concurrency,
	}
}

// errInvalidDevice 非法 -device 的可读错误。
func errInvalidDevice(s string) error {
	return fmt.Errorf("device %q 非法，需为 cpu|cuda|auto", s)
}
