// Package ortx 封装 ONNX Runtime 的运行时装配：进程级环境初始化 + 执行提供器
// （CPU / CUDA 双接口）装配 + 设备/线程/显存配置。
//
// 抽出自 internal/embed，供所有 ONNX 推理能力（CLIP 编码器、人脸检测/识别）复用；
// 差异仅在执行提供器装配与模型文件上，故此处只保留与具体模型无关的部分。
//
// 设计约定：**GPU / CPU 双接口由配置选择而非编译期裁剪**——开发环境不具备 CUDA
// 不影响部署环境启用，禁止以「本地跑不了」为由删减接口。
//
// 本文件不依赖 CGO（设备解析与配置在任何构建下都可用）；
// 需要 ORT 绑定的部分见 session_cgo.go。
package ortx

import (
	"runtime"
	"strings"
)

// DeviceKind 推理设备类型（保留 CPU / GPU 双接口，由配置选择而非编译期裁剪）。
type DeviceKind string

const (
	// DeviceCPU 强制使用 CPU 执行提供器（默认基座，任何环境可用）。
	DeviceCPU DeviceKind = "cpu"
	// DeviceCUDA 强制使用 CUDA 执行提供器；不可用时**报错**（显式配置不应静默降级）。
	DeviceCUDA DeviceKind = "cuda"
	// DeviceAuto 优先 CUDA，不可用则回落 CPU 并记录实际选择（一套配置适配异构部署）。
	DeviceAuto DeviceKind = "auto"
)

// ParseDevice 解析设备配置字符串（大小写不敏感），未知值回落到 auto。
func ParseDevice(s string) DeviceKind {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "cpu":
		return DeviceCPU
	case "cuda", "gpu":
		return DeviceCUDA
	case "auto", "":
		return DeviceAuto
	default:
		return DeviceAuto
	}
}

// Config ONNX Runtime 运行时配置（与具体模型无关）。
//
// 关于 GPU / CPU 双接口：Device 决定执行提供器，各推理能力共用同一套代码路径，
// 差异仅在 EP 装配上。
type Config struct {
	LibPath       string     // libonnxruntime 路径；空则按平台名走系统搜索
	Device        DeviceKind // cpu | cuda | auto（空 = auto）
	DeviceID      int        // CUDA 设备序号（默认 0）
	IntraThreads  int        // CPU 算子内并行线程数（0 = 交给 ORT 默认）
	GpuMemLimitMB int        // CUDA 显存 arena 上限（MB，0 = 不限制）
}

// DefaultLibName 各平台的原生库文件名。
func DefaultLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// ProviderName 返回设备对应的执行提供器名称（用于日志与自检输出）。
func ProviderName(dev DeviceKind) string {
	if dev == DeviceCUDA {
		return "CUDAExecutionProvider"
	}
	return "CPUExecutionProvider"
}
