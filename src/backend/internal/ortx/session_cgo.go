//go:build cgo

package ortx

// ONNX Runtime 会话装配（CPU / CUDA 双执行提供器）。
//
// ⚠️ onnxruntime_go 绑定的头文件版本与运行时库版本必须一致：本工程固定 **ORT 1.29.0**。
// 换成 1.30 的库会在 CreateOrtEnv 阶段段错误（已在 Linux 实测确认）。

import (
	"fmt"
	"strconv"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// 进程内只初始化一次 ONNX Runtime 环境（全局单例）。
var (
	envOnce sync.Once
	envErr  error
)

// EnsureEnv 进程内只初始化一次 ONNX Runtime 环境（全局单例）。
//
// 多次调用只有首次生效；失败结果会被记住并原样返回（避免反复重试掩盖首因）。
func EnsureEnv(lib string) error {
	envOnce.Do(func() {
		ort.SetSharedLibraryPath(lib)
		envErr = ort.InitializeEnvironment()
	})
	return envErr
}

// NewSessionOptions 按配置装配执行提供器，返回会话选项与实际生效设备。
func NewSessionOptions(cfg Config) (*ort.SessionOptions, DeviceKind, error) {
	so, err := ort.NewSessionOptions()
	if err != nil {
		return nil, "", fmt.Errorf("创建会话选项失败: %w", err)
	}
	if cfg.IntraThreads > 0 {
		if err := so.SetIntraOpNumThreads(cfg.IntraThreads); err != nil {
			so.Destroy()
			return nil, "", fmt.Errorf("设置线程数失败: %w", err)
		}
	}

	dev := cfg.Device
	if dev == "" {
		dev = DeviceAuto
	}
	switch dev {
	case DeviceCPU:
		return so, DeviceCPU, nil

	case DeviceCUDA, DeviceAuto:
		if err := appendCUDA(so, cfg); err == nil {
			return so, DeviceCUDA, nil
		} else if dev == DeviceCUDA {
			so.Destroy()
			return nil, "", fmt.Errorf("显式要求 CUDA 执行提供器但装配失败（请确认使用 CUDA 版 onnxruntime 库）: %w", err)
		}
		// auto：回落 CPU（保留 GPU 接口，仅在当前环境不可用时降级）
		return so, DeviceCPU, nil
	}
	return so, DeviceCPU, nil
}

// appendCUDA 装配 CUDA 执行提供器。
func appendCUDA(so *ort.SessionOptions, cfg Config) error {
	opts, err := ort.NewCUDAProviderOptions()
	if err != nil {
		return err
	}
	defer opts.Destroy()

	kv := map[string]string{}
	if cfg.DeviceID > 0 {
		kv["device_id"] = strconv.Itoa(cfg.DeviceID)
	}
	if cfg.GpuMemLimitMB > 0 {
		kv["gpu_mem_limit"] = strconv.Itoa(cfg.GpuMemLimitMB * 1024 * 1024)
	}
	if len(kv) > 0 {
		if err := opts.Update(kv); err != nil {
			return err
		}
	}
	return so.AppendExecutionProviderCUDA(opts)
}

// DestroyI64 / DestroyF32 尽力释放张量。
//
// ⚠️ 必须按**具体类型**分别写，不能用 `...interface{ Destroy() error }` 收集：
// 把 typed-nil 指针（如 CLIP 族的 attention_mask 张量本就为 nil）装箱进接口后，
// 接口本身**非 nil**，于是 nil 判断失效、Destroy() 解引用空指针 panic。
// （实测症状：embedgen 每次成功跑完都在退出时 panic，退出码恒为 2，
//
//	并会掩盖 NewEncoder 的真实错误——先 panic 后返回错误。）
//
// 新增推理能力（如人脸）一律复用这两个具型辅助函数，不要自造接口版判空。
func DestroyI64(ts ...*ort.Tensor[int64]) {
	for _, t := range ts {
		if t != nil {
			t.Destroy()
		}
	}
}

// DestroyF32 见 DestroyI64 的类型说明。
func DestroyF32(ts ...*ort.Tensor[float32]) {
	for _, t := range ts {
		if t != nil {
			t.Destroy()
		}
	}
}
