//go:build cgo

package embed

// CLIP 双塔编码器（ONNX Runtime，CGO 绑定 github.com/yalue/onnxruntime_go）。
//
// 模型：Xenova/clip-vit-base-patch32 导出的 ONNX（基于 openai/clip-vit-base-patch32，MIT）。
//   - text_model_quantized.onnx   : input_ids[1,77] int64      → text_embeds[1,512]  float32
//   - vision_model_quantized.onnx : pixel_values[1,3,224,224]  → image_embeds[1,512] float32
//
// 两塔输出已投影到同一 512 维空间，可直接用余弦相似度做跨模态检索。
//
// 关于并发：onnxruntime 的 Session 非并发安全，这里用互斥锁串行化每个会话。
//
// 为什么用 CGO：另一类纯 Go 绑定（purego 系）在 Windows 上缺少 dlopen 支持，
// 会导致本机完全无法构建/迭代；CGO 在本机（msys2 gcc）与 Linux/Docker 均可用。
//
// ⚠️ 本文件仅在启用 CGO 时编译；未启用时由 clip_nocgo.go 提供占位实现。
// 注意：onnxruntime_go 要求运行时库版本与头文件版本一致（本工程配 1.29.0）。

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Encoder CLIP 编码器（文本 + 图像）。
type Encoder struct {
	tok    *Tokenizer
	text   *ort.AdvancedSession
	vision *ort.AdvancedSession

	// 每会话复用的输入/输出张量（会话锁定期间独占使用，避免反复分配）
	textIn  *ort.Tensor[int64]
	textOut *ort.Tensor[float32]
	visIn   *ort.Tensor[float32]
	visOut  *ort.Tensor[float32]

	muText   sync.Mutex
	muVision sync.Mutex

	envOnce *sync.Once
	envErr  error
}

// NewEncoder 初始化 ONNX Runtime 环境并加载分词器与两个推理会话。
func NewEncoder(cfg Config) (*Encoder, error) {
	if cfg.ModelDir == "" {
		return nil, fmt.Errorf("ModelDir 不能为空")
	}
	tok, err := LoadTokenizer(filepath.Join(cfg.ModelDir, "tokenizer.json"))
	if err != nil {
		return nil, err
	}
	lib := cfg.LibPath
	if lib == "" {
		lib = defaultLibName()
	}

	enc := &Encoder{tok: tok, envOnce: &sync.Once{}}
	if err := enc.initEnv(lib); err != nil {
		return nil, fmt.Errorf("初始化 ONNX Runtime 失败（库 %s）: %w", lib, err)
	}

	// 文本塔：输入 [1,77] int64
	textIn, err := ort.NewEmptyTensor[int64](ort.NewShape(1, ContextLength))
	if err != nil {
		return nil, fmt.Errorf("创建文本输入张量失败: %w", err)
	}
	textOut, err := ort.NewEmptyTensor[float32](ort.NewShape(1, EmbeddingDim))
	if err != nil {
		textIn.Destroy()
		return nil, fmt.Errorf("创建文本输出张量失败: %w", err)
	}
	textSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, "text_model_quantized.onnx"),
		[]string{"input_ids"}, []string{"text_embeds"},
		[]ort.Value{textIn}, []ort.Value{textOut}, nil)
	if err != nil {
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("加载文本编码器失败: %w", err)
	}

	// 视觉塔：输入 [1,3,224,224] float32
	visIn, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, ImageSize, ImageSize))
	if err != nil {
		textSess.Destroy()
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("创建图像输入张量失败: %w", err)
	}
	visOut, err := ort.NewEmptyTensor[float32](ort.NewShape(1, EmbeddingDim))
	if err != nil {
		visIn.Destroy()
		textSess.Destroy()
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("创建图像输出张量失败: %w", err)
	}
	visSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, "vision_model_quantized.onnx"),
		[]string{"pixel_values"}, []string{"image_embeds"},
		[]ort.Value{visIn}, []ort.Value{visOut}, nil)
	if err != nil {
		visIn.Destroy()
		visOut.Destroy()
		textSess.Destroy()
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("加载图像编码器失败: %w", err)
	}

	enc.text, enc.vision = textSess, visSess
	enc.textIn, enc.textOut = textIn, textOut
	enc.visIn, enc.visOut = visIn, visOut
	return enc, nil
}

// initEnv 进程内只初始化一次 ONNX Runtime 环境（全局单例）。
func (e *Encoder) initEnv(lib string) error {
	e.envOnce.Do(func() {
		ort.SetSharedLibraryPath(lib)
		e.envErr = ort.InitializeEnvironment()
	})
	return e.envErr
}

// Close 释放会话与张量（不销毁全局环境，进程退出时由 ORT 自行回收）。
func (e *Encoder) Close() {
	if e.text != nil {
		e.text.Destroy()
	}
	if e.vision != nil {
		e.vision.Destroy()
	}
	if e.textIn != nil {
		e.textIn.Destroy()
	}
	if e.textOut != nil {
		e.textOut.Destroy()
	}
	if e.visIn != nil {
		e.visIn.Destroy()
	}
	if e.visOut != nil {
		e.visOut.Destroy()
	}
}

// EncodeText 文本 → 512 维 L2 归一化向量。
func (e *Encoder) EncodeText(ctx context.Context, text string) ([]float32, error) {
	ids := e.tok.Encode(text)
	dst := e.textIn.GetData()
	if len(dst) < len(ids) {
		return nil, fmt.Errorf("文本输入张量过小: %d < %d", len(dst), len(ids))
	}
	for i, id := range ids {
		dst[i] = int64(id)
	}

	e.muText.Lock()
	defer e.muText.Unlock()

	if err := e.text.Run(); err != nil {
		return nil, fmt.Errorf("文本推理失败: %w", err)
	}
	out := e.textOut.GetData()
	if len(out) != EmbeddingDim {
		return nil, fmt.Errorf("文本输出维度应为 %d，实得 %d", EmbeddingDim, len(out))
	}
	res := make([]float32, EmbeddingDim)
	copy(res, out)
	return L2Normalize(res), nil
}

// EncodeImage 图像文件 → 512 维 L2 归一化向量。
func (e *Encoder) EncodeImage(ctx context.Context, path string) ([]float32, error) {
	px, err := PreprocessImage(path)
	if err != nil {
		return nil, err
	}
	return e.EncodePixels(ctx, px)
}

// EncodePixels 预处理后的像素（NCHW 3×224×224）→ 512 维 L2 归一化向量。
func (e *Encoder) EncodePixels(ctx context.Context, px []float32) ([]float32, error) {
	if len(px) != 3*ImageSize*ImageSize {
		return nil, fmt.Errorf("像素长度应为 %d，实得 %d", 3*ImageSize*ImageSize, len(px))
	}
	dst := e.visIn.GetData()
	if len(dst) != len(px) {
		return nil, fmt.Errorf("图像输入张量长度不符: %d != %d", len(dst), len(px))
	}
	copy(dst, px)

	e.muVision.Lock()
	defer e.muVision.Unlock()

	if err := e.vision.Run(); err != nil {
		return nil, fmt.Errorf("图像推理失败: %w", err)
	}
	out := e.visOut.GetData()
	if len(out) != EmbeddingDim {
		return nil, fmt.Errorf("图像输出维度应为 %d，实得 %d", EmbeddingDim, len(out))
	}
	res := make([]float32, EmbeddingDim)
	copy(res, out)
	return L2Normalize(res), nil
}
