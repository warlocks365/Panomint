package embed

// 与具体推理后端无关的共享定义：常量、配置、相似度工具。
//
// 推理实现按构建标签分离：
//   - clip_cgo.go   （//go:build cgo）  : 真实 ONNX Runtime 实现
//   - clip_nocgo.go （//go:build !cgo） : 占位实现（返回错误，语义召回自动降级）
//
// 这样后端在未启用 CGO 的环境仍可完整编译，AI 能力只是不可用而已。

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
)

// EmbeddingDim CLIP ViT-B/32 投影维度（与 media.embedding VECTOR(512) 对齐）。
const EmbeddingDim = 512

// Config 编码器配置。
type Config struct {
	ModelDir string // 含 *.onnx 与 tokenizer.json 的目录
	LibPath  string // libonnxruntime 路径；空则按平台名走系统搜索
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
