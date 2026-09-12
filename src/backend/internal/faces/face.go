// Package faces 人脸检测 / 对齐 / 特征 / 聚类（ONNX Runtime，CGO 绑定）。
//
// 模型选型（**均为 Apache-2.0，OpenCV Zoo**，规避 insightface 预训练模型的商用许可风险）：
//
//	检测 YuNet  face_detection_yunet_2023mar.onnx   输入 1×3×H×W float32 BGR，0..255 不归一化
//	特征 SFace  face_recognition_sface_2021dec.onnx 输入 1×3×112×112 float32 RGB，0..255，输出 128 维
//
// 两处预处理参数**直接对齐 OpenCV 参考实现**（不是想当然）：
//   - YuNet ：OpenCV FaceDetectorYN 对 BGR 帧直接送网，不做归一化 → 本包 letterbox 后保持 BGR / 0..255。
//   - SFace ：OpenCV FaceRecognizerSFImpl::feature 为
//     blobFromImage(aligned, 1, Size(112,112), Scalar(0,0,0), swapRB=true, crop=false)
//     → **scale=1、mean=0、通道由 BGR 交换为 RGB**；本包源图本就是 RGB，故等价于直接送原始 0..255 RGB。
//
// 推理设备（GPU / CPU 双接口）由 ortx 提供，配置选择而非编译期裁剪；
// 未启用 CGO 的构建由 faces_nocgo.go 提供占位实现（能力降级，不裁剪接口）。
package faces

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"panoalbum/internal/ortx"
)

// EmbeddingDim SFace 输出维度（与 faces.embedding VECTOR(128) 对齐）。
const EmbeddingDim = 128

// FaceSize SFace 输入边长（112×112，ArcFace 标准对齐尺寸）。
const FaceSize = 112

// 默认参数。阈值需在自有语料上标定（见 Options 注释）。
const (
	// DefaultInputSize 检测输入边长兜底值（模型未声明静态形状时使用）。
	DefaultInputSize = 640
	// DefaultConfThreshold 检测置信度阈值（对齐 OpenCV 演示默认）。
	DefaultConfThreshold = 0.9
	// DefaultNMSThreshold 检测 NMS IoU 阈值（对齐 OpenCV 演示默认）。
	DefaultNMSThreshold = 0.3
	// DefaultMinFacePx 最小人脸边长（像素）：过小的人脸特征不可靠，直接丢弃。
	DefaultMinFacePx = 24
	// DefaultMergeSimilarity 聚类合并阈值（余弦相似度）。
	//
	// SFace 官方**同人判定**阈值为 0.363；聚类合并故意取略低值以提升召回，
	// 需按自有语料标定（亚洲人脸无公开基准）。可用 FACE_MERGE_SIM 覆盖。
	DefaultMergeSimilarity = 0.33
)

// 模型文件名候选（按顺序取第一个存在的文件；兼容 2023mar / 2026may 动态版 / 通用名）。
var (
	yuNetCandidates = []string{
		"face_detection_yunet_2023mar.onnx",
		"face_detection_yunet_2026may.onnx",
		"face_detection_yunet.onnx",
	}
	sFaceCandidates = []string{
		"face_recognition_sface_2021dec.onnx",
		"face_recognition_sface.onnx",
	}
)

// ErrCGORequired 表示当前构建未启用 CGO，人脸推理不可用。
var ErrCGORequired = errors.New("本构建未启用 CGO，人脸识别能力不可用（需 CGO_ENABLED=1 重新构建）")

// Detection 一张人脸（坐标均为**原图**像素坐标）。
type Detection struct {
	X, Y, W, H float64
	// Landmarks 五点：右眼、左眼、鼻尖、右嘴角、左嘴角（YuNet / OpenCV 约定）。
	Landmarks [5][2]float64
	Score     float64
}

// Options 人脸流水线配置。
type Options struct {
	ModelDir string // 含 YuNet / SFace 两个 onnx 的目录
	LibPath  string // libonnxruntime 路径；空则按平台名走系统搜索

	// 推理设备（cpu|cuda|auto）；GPU / CPU 双接口保留，不因开发机无 CUDA 而裁剪。
	Device        ortx.DeviceKind
	DeviceID      int
	IntraThreads  int
	GpuMemLimitMB int

	InputSize     int     // 检测输入边长（0=用模型声明值，其次 DefaultInputSize）
	ConfThreshold float64 // 检测置信度阈值（0=默认 0.9）
	NMSThreshold  float64 // 检测 NMS IoU 阈值（0=默认 0.3）
	MinFacePx     int     // 最小人脸边长（0=默认 24）
	MergeSim      float64 // 聚类合并余弦阈值（0=默认 0.33）

	// Store 聚类所需的 DB 存取（nil=只检测不聚类）。
	Store *Store
}

// OptionsFromEnv 从环境变量构造配置（flag 可再覆盖）。
//
//	FACE_MODEL_DIR   模型目录（默认 assets/models/faces）
//	FACE_LIB / EMBED_LIB  onnxruntime 原生库路径
//	FACE_DEVICE      cpu|cuda|auto（默认 auto）
//	FACE_DEVICE_ID / FACE_THREADS / FACE_GPU_MEM_MB
//	FACE_INPUT_SIZE / FACE_CONF / FACE_NMS / FACE_MIN_PX / FACE_MERGE_SIM
func OptionsFromEnv() Options {
	lib := os.Getenv("FACE_LIB")
	if lib == "" {
		lib = os.Getenv("EMBED_LIB") // 与语义向量共用同一份 ORT 运行时
	}
	o := Options{
		ModelDir: ModelDirFromEnv(),
		LibPath:  lib,
		Device:   ortx.ParseDevice(firstNonEmpty(os.Getenv("FACE_DEVICE"), os.Getenv("EMBED_DEVICE"))),
	}
	o.DeviceID = atoi(os.Getenv("FACE_DEVICE_ID"))
	o.IntraThreads = atoi(firstNonEmpty(os.Getenv("FACE_THREADS"), os.Getenv("EMBED_THREADS")))
	o.GpuMemLimitMB = atoi(firstNonEmpty(os.Getenv("FACE_GPU_MEM_MB"), os.Getenv("EMBED_GPU_MEM_MB")))
	o.InputSize = atoi(os.Getenv("FACE_INPUT_SIZE"))
	o.ConfThreshold = atof(os.Getenv("FACE_CONF"))
	o.NMSThreshold = atof(os.Getenv("FACE_NMS"))
	o.MinFacePx = atoi(os.Getenv("FACE_MIN_PX"))
	o.MergeSim = atof(os.Getenv("FACE_MERGE_SIM"))
	return o
}

// ModelDirFromEnv 解析模型目录：优先 FACE_MODEL_DIR，其次仓库内默认位置。
func ModelDirFromEnv() string {
	if v := os.Getenv("FACE_MODEL_DIR"); v != "" {
		return v
	}
	return filepath.Join("assets", "models", "faces")
}

// WithDefaults 补齐零值（便于 flag 只覆盖部分字段）。
func (o Options) WithDefaults() Options {
	if o.Device == "" {
		o.Device = ortx.DeviceAuto
	}
	if o.ConfThreshold <= 0 {
		o.ConfThreshold = DefaultConfThreshold
	}
	if o.NMSThreshold <= 0 {
		o.NMSThreshold = DefaultNMSThreshold
	}
	if o.MinFacePx <= 0 {
		o.MinFacePx = DefaultMinFacePx
	}
	if o.MergeSim <= 0 {
		o.MergeSim = DefaultMergeSimilarity
	}
	return o
}

// loadModelFile 在模型目录内按候选名找到实际存在的模型文件。
func loadModelFile(dir string, candidates []string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("模型目录为空")
	}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("在 %s 未找到模型文件（候选：%s）", dir, strings.Join(candidates, ", "))
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

// CosineSimilarity 余弦相似度（与模长无关）。
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

// vectorLiteral float32 切片 → pgvector 字面量 "[v1,v2,...]"。
func vectorLiteral(v []float32) string {
	var sb strings.Builder
	sb.Grow(len(v) * 9)
	sb.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(x), 'f', 6, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// parseVector 解析 pgvector 文本表示 "[v1,v2,...]"。
func parseVector(s string) []float32 {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil
		}
		out = append(out, float32(f))
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
