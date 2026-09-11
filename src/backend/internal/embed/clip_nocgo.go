//go:build !cgo

package embed

// 未启用 CGO 时的占位实现。
//
// 目的：让整个后端在无 C 工具链的环境（如 CGO_ENABLED=0 的默认构建）仍能编译通过，
// 语义召回能力则自动降级为不可用（NewEncoder 返回明确错误，上层据此退回结构化检索）。

import (
	"context"
	"errors"
)

// ErrCGORequired 表示当前构建未启用 CGO，CLIP 推理不可用。
var ErrCGORequired = errors.New("本构建未启用 CGO，CLIP 向量能力不可用（需 CGO_ENABLED=1 重新构建）")

// Encoder 占位类型（方法签名与 cgo 版本保持一致）。
type Encoder struct{}

// NewEncoder 始终失败。
func NewEncoder(Config) (*Encoder, error) { return nil, ErrCGORequired }

// Close 空实现。
func (*Encoder) Close() {}

// EncodeText 不可用。
func (*Encoder) EncodeText(context.Context, string) ([]float32, error) { return nil, ErrCGORequired }

// EncodeImage 不可用。
func (*Encoder) EncodeImage(context.Context, string) ([]float32, error) { return nil, ErrCGORequired }

// EncodePixels 不可用。
func (*Encoder) EncodePixels(context.Context, []float32) ([]float32, error) { return nil, ErrCGORequired }
