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
// ── 执行提供器（GPU / CPU 双接口）────────────────────────────────────────────
// Config.Device 决定装配哪个 ONNX Runtime Execution Provider：
//   cpu  : 仅 CPU EP（基座，任何环境可用）
//   cuda : 追加 CUDA EP；装配失败即报错（显式配置不静默降级）
//   auto : 先试 CUDA EP，失败则回落 CPU EP 并记录实际选择
// 二者共用同一模型、同一预处理与同一代码路径，差异仅在 EP 装配。
// 注意：CUDA EP 需要 CUDA 版 onnxruntime 库（onnxruntime-linux-x64-gpu_cudaXX）；
// 使用 CPU 版库时装配必然失败——因此 auto 模式在只有 CPU 库的机器上即自动落 CPU。
// 另：CUDA EP 已知会覆盖 Go 的信号处理器（yalue/onnxruntime_go#140），
// 若生产启用 CUDA 需在初始化后自行恢复信号处理。
//
// ⚠️ 本文件仅在启用 CGO 时编译；未启用时由 clip_nocgo.go 提供占位实现。
// onnxruntime_go 要求运行时库版本与头文件版本一致（本工程配 1.29.0）。

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
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

	device DeviceKind // 实际生效的设备
	lib    string     // 实际加载的原生库路径

	muText   sync.Mutex
	muVision sync.Mutex

	envOnce *sync.Once
	envErr  error
}

// Device 返回实际生效的推理设备（cpu / cuda）。
func (e *Encoder) Device() DeviceKind { return e.device }

// Provider 返回人类可读的执行提供器描述（用于日志与自检输出）。
func (e *Encoder) Provider() string {
	if e.device == DeviceCUDA {
		return "CUDAExecutionProvider"
	}
	return "CPUExecutionProvider"
}

// LibPath 返回实际加载的原生库路径。
func (e *Encoder) LibPath() string { return e.lib }

// newSessionOptions 按配置装配执行提供器，返回会话选项与实际生效设备。
func newSessionOptions(cfg Config) (*ort.SessionOptions, DeviceKind, error) {
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

	enc := &Encoder{tok: tok, envOnce: &sync.Once{}, lib: lib}
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
	textOpts, dev, err := newSessionOptions(cfg)
	if err != nil {
		textIn.Destroy()
		textOut.Destroy()
		return nil, err
	}
	textSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, "text_model_quantized.onnx"),
		[]string{"input_ids"}, []string{"text_embeds"},
		[]ort.Value{textIn}, []ort.Value{textOut}, textOpts)
	textOpts.Destroy()
	if err != nil {
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("加载文本编码器失败: %w", err)
	}
	enc.device = dev

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
	visOpts, dev2, err := newSessionOptions(cfg)
	if err != nil {
		visIn.Destroy()
		visOut.Destroy()
		textSess.Destroy()
		textIn.Destroy()
		textOut.Destroy()
		return nil, err
	}
	visSess, err := ort.NewAdvancedSession(
		filepath.Join(cfg.ModelDir, "vision_model_quantized.onnx"),
		[]string{"pixel_values"}, []string{"image_embeds"},
		[]ort.Value{visIn}, []ort.Value{visOut}, visOpts)
	visOpts.Destroy()
	if err != nil {
		visIn.Destroy()
		visOut.Destroy()
		textSess.Destroy()
		textIn.Destroy()
		textOut.Destroy()
		return nil, fmt.Errorf("加载图像编码器失败: %w", err)
	}
	// 两塔设备应一致（同一份配置）；不一致时取更保守的 CPU
	if dev2 != dev {
		enc.device = DeviceCPU
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
