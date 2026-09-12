//go:build !cgo

package embed

// 未启用 CGO 时的占位实现。
//
// 目的：让整个后端在无 C 工具链的环境（如 CGO_ENABLED=0 的默认构建）仍能编译通过，
// 语义召回能力则自动降级为不可用（NewEncoder 返回明确错误，上层据此退回结构化检索）。
//
// 注意：这只是**构建期**的能力占位，不是对 GPU/CPU 双接口或模型族的裁剪——
// 设备选择（cpu/cuda/auto）与模型族（clip/chinese-clip）在启用 CGO 的构建中完整保留，
// 见 clip_cgo.go。

import (
	"context"
	"errors"
	"image"
)

// ErrCGORequired 表示当前构建未启用 CGO，CLIP 推理不可用。
var ErrCGORequired = errors.New("本构建未启用 CGO，CLIP 向量能力不可用（需 CGO_ENABLED=1 重新构建）")

// Encoder 占位类型（方法签名与 cgo 版本保持一致）。
type Encoder struct {
	device DeviceKind
	family ModelFamily
	lib    string
	ctxLen int
}

// NewEncoder 始终失败。
func NewEncoder(cfg Config) (*Encoder, error) { return nil, ErrCGORequired }

// Device 返回请求的设备（占位实现下不代表实际可用）。
func (e *Encoder) Device() DeviceKind { return e.device }

// Family 返回请求的模型族。
func (e *Encoder) Family() ModelFamily { return e.family }

// Provider 占位实现下无可用执行提供器。
func (e *Encoder) Provider() string { return "unavailable(!cgo)" }

// LibPath 返回配置的库路径。
func (e *Encoder) LibPath() string { return e.lib }

// ContextLen 占位实现下无意义。
func (e *Encoder) ContextLen() int { return e.ctxLen }

// Close 空实现。
func (*Encoder) Close() {}

// EncodeText 不可用。
func (*Encoder) EncodeText(context.Context, string) ([]float32, error) { return nil, ErrCGORequired }

// EncodeImage 不可用。
func (*Encoder) EncodeImage(context.Context, string) ([]float32, error) { return nil, ErrCGORequired }

// EncodeImageData 不可用。
func (*Encoder) EncodeImageData(context.Context, image.Image) ([]float32, error) {
	return nil, ErrCGORequired
}

// EncodePixels 不可用。
func (*Encoder) EncodePixels(context.Context, []float32) ([]float32, error) {
	return nil, ErrCGORequired
}
