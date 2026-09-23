//go:build !cgo

package faces

// 未启用 CGO 时的占位实现。
//
// 目的：让整个后端在无 C 工具链的环境（CGO_ENABLED=0）仍能编译通过，
// 人脸能力自动降级为不可用（构造函数返回明确错误，上层据此跳过人脸流水线）。
//
// 注意：这只是**构建期**的能力占位，不是对 GPU/CPU 双接口的裁剪——
// 设备选择（cpu/cuda/auto）与全部推理路径在 CGO 构建中完整保留。

import (
	"image"

	"panoalbum/internal/ortx"
)

// Detector 占位类型（方法集与 cgo 版本保持一致）。
type Detector struct{}

// NewDetector 始终失败。
func NewDetector(Options) (*Detector, error) { return nil, ErrCGORequired }

// Detect 不可用。
func (*Detector) Detect(image.Image) ([]Detection, error) { return nil, ErrCGORequired }

// DetectMulti 不可用（Job000041 多尺度加入 cgo 版时漏配占位 —— 曾致 CGO_ENABLED=0
// 下 cmd/facesgen 编译失败、CI 全量 test 门静默断链；方法集必须与 cgo 版保持一致）。
func (*Detector) DetectMulti(image.Image) ([]Detection, error) { return nil, ErrCGORequired }

// Size 占位实现下无意义。
func (*Detector) Size() int { return 0 }

// Device 占位实现下无实际设备。
func (*Detector) Device() ortx.DeviceKind { return "" }

// Provider 占位实现下无可用执行提供器。
func (*Detector) Provider() string { return "unavailable(!cgo)" }

// Close 空实现。
func (*Detector) Close() {}

// Recognizer 占位类型（方法集与 cgo 版本保持一致）。
type Recognizer struct{}

// NewRecognizer 始终失败。
func NewRecognizer(Options) (*Recognizer, error) { return nil, ErrCGORequired }

// EmbedFace 不可用。
func (*Recognizer) EmbedFace(image.Image, [5][2]float64) ([]float32, error) {
	return nil, ErrCGORequired
}

// EmbedAligned 不可用。
func (*Recognizer) EmbedAligned([]float32) ([]float32, error) { return nil, ErrCGORequired }

// Device 占位实现下无实际设备。
func (*Recognizer) Device() ortx.DeviceKind { return "" }

// Provider 占位实现下无可用执行提供器。
func (*Recognizer) Provider() string { return "unavailable(!cgo)" }

// Close 空实现。
func (*Recognizer) Close() {}
