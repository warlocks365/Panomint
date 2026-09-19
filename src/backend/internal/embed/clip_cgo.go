//go:build cgo

package embed

// CLIP / Chinese-CLIP 双塔编码器（ONNX Runtime，CGO 绑定 github.com/yalue/onnxruntime_go）。
//
// 支持的模型族见 ModelFamily：两族投影维度均为 512（对齐 media.embedding），
// 但 tokenizer、ONNX 文件、输入签名、预处理与文本长度不同，由 modelSpec 描述差异。
//
//	clip         : text_model_quantized.onnx [input_ids]           + vision_model_quantized.onnx [pixel_values]
//	chinese-clip : text_only.onnx           [input_ids, attention_mask] + vision_only.onnx       [pixel_values]
//
// 关于并发：onnxruntime 的 Session 非并发安全，这里用互斥锁串行化每个会话。
//
// 关于执行提供器（GPU / CPU 双接口）：见 internal/ortx.NewSessionOptions ——
// 同一二进制由配置切换，不因开发环境不具备 CUDA 而裁剪接口。
//
// ⚠️ 本文件仅在启用 CGO 时编译；未启用时由 clip_nocgo.go 提供占位实现。
// onnxruntime_go 要求运行时库版本与头文件版本一致（本工程配 1.29.0）。

import (
	"context"
	"fmt"
	"image"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"panoalbum/internal/ortx"
	"panoalbum/internal/vecutil"
)

// modelSpec 描述一个模型族的文件与接口差异。
type modelSpec struct {
	textModel    string
	textInputs   []string
	textOutput   string
	visionModel  string
	visionInputs []string
	visionOutput string
	contextLen   int  // 文本序列长度
	useMask      bool // 是否需要 attention_mask 输入
	resizeOnly   bool // 图像预处理：直接缩放 224（true）还是最短边缩放+中心裁剪（false）
}

// specFor 按模型族选择规格。
func specFor(family ModelFamily) modelSpec {
	if family == FamilyChineseCLIP {
		return modelSpec{
			textModel:    "text_only.onnx",
			textInputs:   []string{"input_ids", "attention_mask"},
			textOutput:   "text_embeds",
			visionModel:  "vision_only.onnx",
			visionInputs: []string{"pixel_values"},
			visionOutput: "image_embeds",
			contextLen:   BertContextLength,
			useMask:      true,
			resizeOnly:   true, // preprocessor_config: do_center_crop=false
		}
	}
	return modelSpec{
		textModel:    "text_model_quantized.onnx",
		textInputs:   []string{"input_ids"},
		textOutput:   "text_embeds",
		visionModel:  "vision_model_quantized.onnx",
		visionInputs: []string{"pixel_values"},
		visionOutput: "image_embeds",
		contextLen:   ContextLength,
		useMask:      false,
		resizeOnly:   false,
	}
}

// textTokenizer 两族分词器的共同接口。
type textTokenizer interface {
	Encode(text string) []int32
	PadID() int32
}

// Encoder 双塔编码器（按模型族装配）。
type Encoder struct {
	spec   modelSpec
	family ModelFamily
	tok    textTokenizer

	text   *ort.AdvancedSession
	vision *ort.AdvancedSession

	// 每会话复用的张量（会话锁定期间独占使用，避免反复分配）
	textIn  *ort.Tensor[int64]
	maskIn  *ort.Tensor[int64] // 仅 useMask 时非 nil
	textOut *ort.Tensor[float32]
	visIn   *ort.Tensor[float32]
	visOut  *ort.Tensor[float32]

	device DeviceKind
	lib    string

	muText   sync.Mutex
	muVision sync.Mutex
}

// Device 返回实际生效的推理设备（cpu / cuda）。
func (e *Encoder) Device() DeviceKind { return e.device }

// Family 返回实际使用的模型族。
func (e *Encoder) Family() ModelFamily { return e.family }

// Provider 返回人类可读的执行提供器描述（用于日志与自检输出）。
func (e *Encoder) Provider() string { return ortx.ProviderName(e.device) }

// LibPath 返回实际加载的原生库路径。
func (e *Encoder) LibPath() string { return e.lib }

// ContextLen 返回文本序列长度（便于诊断）。
func (e *Encoder) ContextLen() int { return e.spec.contextLen }

// ortConfig 把 embed 配置投影为 ortx 运行时配置（含设备/线程/显存）。
func (c Config) ortConfig() ortx.Config {
	return ortx.Config{
		LibPath:       c.LibPath,
		Device:        c.Device,
		DeviceID:      c.DeviceID,
		IntraThreads:  c.IntraThreads,
		GpuMemLimitMB: c.GpuMemLimitMB,
	}
}

// NewEncoder 初始化 ONNX Runtime 环境并加载分词器与两个推理会话。
func NewEncoder(cfg Config) (*Encoder, error) {
	if cfg.ModelDir == "" {
		return nil, fmt.Errorf("ModelDir 不能为空")
	}
	family := cfg.Family
	if family == "" {
		family = FamilyCLIP
	}
	spec := specFor(family)

	// 分词器按族选择（CLIP:BPE / Chinese-CLIP:BERT WordPiece）
	var tok textTokenizer
	switch family {
	case FamilyChineseCLIP:
		bt, err := LoadBertTokenizer(filepath.Join(cfg.ModelDir, "tokenizer.json"))
		if err != nil {
			return nil, err
		}
		tok = bt
	default:
		ct, err := LoadTokenizer(filepath.Join(cfg.ModelDir, "tokenizer.json"))
		if err != nil {
			return nil, err
		}
		tok = ct
	}

	lib := cfg.LibPath
	if lib == "" {
		lib = defaultLibName()
	}
	enc := &Encoder{spec: spec, family: family, tok: tok, lib: lib}
	if err := ortx.EnsureEnv(lib); err != nil {
		return nil, fmt.Errorf("初始化 ONNX Runtime 失败（库 %s）: %w", lib, err)
	}

	// ---- 文本塔 ----
	textIn, err := ort.NewEmptyTensor[int64](ort.NewShape(1, int64(spec.contextLen)))
	if err != nil {
		return nil, fmt.Errorf("创建文本输入张量失败: %w", err)
	}
	textInputs := []ort.Value{textIn}
	var maskIn *ort.Tensor[int64]
	if spec.useMask {
		maskIn, err = ort.NewEmptyTensor[int64](ort.NewShape(1, int64(spec.contextLen)))
		if err != nil {
			textIn.Destroy()
			return nil, fmt.Errorf("创建 attention_mask 张量失败: %w", err)
		}
		textInputs = append(textInputs, maskIn)
	}
	textOut, err := ort.NewEmptyTensor[float32](ort.NewShape(1, EmbeddingDim))
	if err != nil {
		ortx.DestroyI64(textIn, maskIn)
		return nil, fmt.Errorf("创建文本输出张量失败: %w", err)
	}
	textOpts, dev, err := ortx.NewSessionOptions(cfg.ortConfig())
	if err != nil {
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, err
	}
	textSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, spec.textModel),
		spec.textInputs, []string{spec.textOutput},
		textInputs, []ort.Value{textOut}, textOpts)
	textOpts.Destroy()
	if err != nil {
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, fmt.Errorf("加载文本编码器失败（%s）: %w", spec.textModel, err)
	}
	enc.device = dev

	// ---- 视觉塔 ----
	visIn, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, ImageSize, ImageSize))
	if err != nil {
		textSess.Destroy()
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, fmt.Errorf("创建图像输入张量失败: %w", err)
	}
	visOut, err := ort.NewEmptyTensor[float32](ort.NewShape(1, EmbeddingDim))
	if err != nil {
		visIn.Destroy()
		textSess.Destroy()
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, fmt.Errorf("创建图像输出张量失败: %w", err)
	}
	visOpts, dev2, err := ortx.NewSessionOptions(cfg.ortConfig())
	if err != nil {
		ortx.DestroyF32(visIn, visOut)
		textSess.Destroy()
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, err
	}
	visSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, spec.visionModel),
		spec.visionInputs, []string{spec.visionOutput},
		[]ort.Value{visIn}, []ort.Value{visOut}, visOpts)
	visOpts.Destroy()
	if err != nil {
		ortx.DestroyF32(visIn, visOut)
		textSess.Destroy()
		ortx.DestroyI64(textIn, maskIn)
		ortx.DestroyF32(textOut)
		return nil, fmt.Errorf("加载图像编码器失败（%s）: %w", spec.visionModel, err)
	}
	// 两塔设备应一致（同一份配置）；不一致时取更保守的 CPU
	if dev2 != dev {
		enc.device = DeviceCPU
	}

	enc.text, enc.vision = textSess, visSess
	enc.textIn, enc.maskIn, enc.textOut = textIn, maskIn, textOut
	enc.visIn, enc.visOut = visIn, visOut
	return enc, nil
}

// Close 释放会话与张量（不销毁全局环境，进程退出时由 ORT 自行回收）。
//
// 张量释放一律走 ortx 的**具型**辅助函数（DestroyI64 / DestroyF32）——
// 不能用接口版判空：typed-nil 装箱后接口非 nil，会解引用空指针 panic。
func (e *Encoder) Close() {
	if e.text != nil {
		e.text.Destroy()
	}
	if e.vision != nil {
		e.vision.Destroy()
	}
	ortx.DestroyI64(e.textIn, e.maskIn)
	ortx.DestroyF32(e.textOut, e.visIn, e.visOut)
}

// EncodeText 文本 → 512 维 L2 归一化向量。
func (e *Encoder) EncodeText(ctx context.Context, text string) ([]float32, error) {
	ids := e.tok.Encode(text)

	// 会话与复用张量非并发安全：先加锁，再触碰共享输入/输出张量（写输入→Run→拷输出 全段串行化）。
	e.muText.Lock()
	defer e.muText.Unlock()

	dst := e.textIn.GetData()
	if len(dst) < len(ids) {
		return nil, fmt.Errorf("文本输入张量过小: %d < %d", len(dst), len(ids))
	}
	for i, id := range ids {
		dst[i] = int64(id)
	}
	if e.maskIn != nil {
		md := e.maskIn.GetData()
		pad := e.tok.PadID()
		for i, id := range ids {
			if id == pad {
				md[i] = 0
			} else {
				md[i] = 1
			}
		}
	}

	if err := e.text.Run(); err != nil {
		return nil, fmt.Errorf("文本推理失败: %w", err)
	}
	out := e.textOut.GetData()
	if len(out) != EmbeddingDim {
		return nil, fmt.Errorf("文本输出维度应为 %d，实得 %d", EmbeddingDim, len(out))
	}
	res := make([]float32, EmbeddingDim)
	copy(res, out)
	return vecutil.L2Normalize(res), nil
}

// EncodeImage 图像文件 → 512 维 L2 归一化向量。
func (e *Encoder) EncodeImage(ctx context.Context, path string) ([]float32, error) {
	img, err := DecodeImageFile(path)
	if err != nil {
		return nil, err
	}
	return e.EncodeImageData(ctx, img)
}

// EncodeImageData 已解码图像 → 512 维 L2 归一化向量（按族选择预处理）。
func (e *Encoder) EncodeImageData(ctx context.Context, img image.Image) ([]float32, error) {
	var px []float32
	if e.spec.resizeOnly {
		px = PreprocessImageDataResize(img)
	} else {
		px = PreprocessImageData(img)
	}
	return e.EncodePixels(ctx, px)
}

// EncodePixels 预处理后的像素（NCHW 3×224×224）→ 512 维 L2 归一化向量。
func (e *Encoder) EncodePixels(ctx context.Context, px []float32) ([]float32, error) {
	if len(px) != 3*ImageSize*ImageSize {
		return nil, fmt.Errorf("像素长度应为 %d，实得 %d", 3*ImageSize*ImageSize, len(px))
	}

	// 会话与复用张量非并发安全：先加锁，再触碰共享输入/输出张量（写输入→Run→拷输出 全段串行化）。
	e.muVision.Lock()
	defer e.muVision.Unlock()

	dst := e.visIn.GetData()
	if len(dst) != len(px) {
		return nil, fmt.Errorf("图像输入张量长度不符: %d != %d", len(dst), len(px))
	}
	copy(dst, px)

	if err := e.vision.Run(); err != nil {
		return nil, fmt.Errorf("图像推理失败: %w", err)
	}
	out := e.visOut.GetData()
	if len(out) != EmbeddingDim {
		return nil, fmt.Errorf("图像输出维度应为 %d，实得 %d", EmbeddingDim, len(out))
	}
	res := make([]float32, EmbeddingDim)
	copy(res, out)
	return vecutil.L2Normalize(res), nil
}
