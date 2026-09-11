package embed

// 与具体推理后端无关的共享定义：常量、配置、设备选择、相似度工具。
//
// 推理实现按构建标签分离：
//   - clip_cgo.go   （//go:build cgo）  : 真实 ONNX Runtime 实现（CPU / CUDA 双执行提供器）
//   - clip_nocgo.go （//go:build !cgo） : 占位实现（返回错误，语义召回自动降级）
//
// 这样后端在未启用 CGO 的环境仍可完整编译，AI 能力只是不可用而已。

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// EmbeddingDim CLIP ViT-B/32 投影维度（与 media.embedding VECTOR(512) 对齐）。
const EmbeddingDim = 512

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

// Config 编码器配置。
//
// 关于 GPU / CPU 双接口：Device 决定执行提供器，二者共用同一套模型与代码路径，
// 差异仅在 ONNX Runtime 的 Execution Provider 装配上。开发机不具备某项能力
// （例如无 CUDA 运行时）不影响部署环境启用的可能，故此处不因环境裁剪接口。
type Config struct {
	ModelDir string     // 含 *.onnx 与 tokenizer.json 的目录
	LibPath  string     // libonnxruntime 路径；空则按平台名走系统搜索
	Device   DeviceKind // cpu | cuda | auto（空 = auto）

	DeviceID     int // CUDA 设备序号（默认 0）
	IntraThreads int // CPU 算子内并行线程数（0 = 交给 ORT 默认）

	// GpuMemLimitMB CUDA 显存 arena 上限（MB，0 = 不限制）。
	// 多进程共用一张卡时用于避免显存争抢。
	GpuMemLimitMB int
}

// ConfigFromEnv 从环境变量构造配置（便于容器/服务端按部署环境选择设备）。
//
//	EMBED_MODEL_DIR 模型目录（默认 assets/models/clip）
//	EMBED_LIB       onnxruntime 原生库路径（默认按平台名搜索）
//	EMBED_DEVICE    cpu | cuda | auto（默认 auto）
//	EMBED_DEVICE_ID CUDA 设备序号（默认 0）
//	EMBED_THREADS   CPU 线程数（默认 0 = ORT 默认）
//	EMBED_GPU_MEM_MB CUDA 显存上限 MB（默认 0 = 不限）
func ConfigFromEnv() Config {
	c := Config{
		ModelDir: ModelDirFromEnv(),
		LibPath:  os.Getenv("EMBED_LIB"),
		Device:   ParseDevice(os.Getenv("EMBED_DEVICE")),
	}
	if v, err := strconv.Atoi(os.Getenv("EMBED_DEVICE_ID")); err == nil {
		c.DeviceID = v
	}
	if v, err := strconv.Atoi(os.Getenv("EMBED_THREADS")); err == nil {
		c.IntraThreads = v
	}
	if v, err := strconv.Atoi(os.Getenv("EMBED_GPU_MEM_MB")); err == nil {
		c.GpuMemLimitMB = v
	}
	return c
}

// defaultLibName 各平台的原生库文件名。
func defaultLibName() string {
	switch runtime.GOOS {
	case "windows":
		return "onnxruntime.dll"
	case "darwin":
		return "libonnxruntime.dylib"
	default:
		return "libonnxruntime.so"
	}
}

// LibPathAuto 返回当前平台的原生库文件名（供调用方拼路径）。
func LibPathAuto() string { return defaultLibName() }

// ModelDirFromEnv 解析模型目录：优先 EMBED_MODEL_DIR，其次仓库内默认位置。
func ModelDirFromEnv() string {
	if v := os.Getenv("EMBED_MODEL_DIR"); v != "" {
		return v
	}
	return filepath.Join("assets", "models", "clip")
}

// L2Normalize 原地 L2 归一化（零向量原样返回）。
func L2Normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := math.Sqrt(sum)
	if n == 0 {
		return v
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

// CosineSimilarity 余弦相似度（输入已归一化时等价于点积）。
func CosineSimilarity(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
