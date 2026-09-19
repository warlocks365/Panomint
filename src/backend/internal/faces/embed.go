//go:build cgo

package faces

// SFace 特征提取器（ONNX Runtime）：112×112 对齐人脸 → 128 维 L2 归一化向量。

import (
	"fmt"
	"image"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"panoalbum/internal/ortx"
	"panoalbum/internal/vecutil"
)

// Recognizer SFace 识别器（会话非并发安全，内部加锁串行化）。
type Recognizer struct {
	opts   Options
	sess   *ort.AdvancedSession
	in     *ort.Tensor[float32]
	out    *ort.Tensor[float32]
	device ortx.DeviceKind
	mu     sync.Mutex
}

// NewRecognizer 加载 SFace 并装配会话。
func NewRecognizer(o Options) (*Recognizer, error) {
	o = o.WithDefaults()
	modelPath, err := loadModelFile(o.ModelDir, sFaceCandidates)
	if err != nil {
		return nil, err
	}
	lib := o.LibPath
	if lib == "" {
		lib = ortx.DefaultLibName()
	}
	if err := ortx.EnsureEnv(lib); err != nil {
		return nil, fmt.Errorf("初始化 ONNX Runtime 失败（库 %s）: %w", lib, err)
	}

	ins, outs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("读取模型接口失败（%s）: %w", modelPath, err)
	}
	if len(ins) != 1 || len(outs) != 1 {
		return nil, fmt.Errorf("SFace 期望 1 输入 1 输出，实得 %d 输入 %d 输出", len(ins), len(outs))
	}
	if ins[0].DataType != ort.TensorElementDataTypeFloat {
		return nil, fmt.Errorf("SFace 输入应为 float32，实得 %s", ins[0].DataType)
	}
	outDim := int(lastPositiveDim(outs[0].Dimensions))
	if outDim == 0 {
		outDim = EmbeddingDim
	}
	if outDim != EmbeddingDim {
		return nil, fmt.Errorf("SFace 输出维度应为 %d，模型声明为 %d（请确认模型版本）", EmbeddingDim, outDim)
	}

	in, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, FaceSize, FaceSize))
	if err != nil {
		return nil, fmt.Errorf("创建人脸输入张量失败: %w", err)
	}
	out, err := ort.NewEmptyTensor[float32](ort.NewShape(1, int64(outDim)))
	if err != nil {
		ortx.DestroyF32(in)
		return nil, fmt.Errorf("创建人脸输出张量失败: %w", err)
	}

	so, dev, err := ortx.NewSessionOptions(ortx.Config{
		LibPath:       o.LibPath,
		Device:        o.Device,
		DeviceID:      o.DeviceID,
		IntraThreads:  o.IntraThreads,
		GpuMemLimitMB: o.GpuMemLimitMB,
	})
	if err != nil {
		ortx.DestroyF32(in, out)
		return nil, err
	}
	sess, err := ort.NewAdvancedSession(modelPath, []string{ins[0].Name}, []string{outs[0].Name},
		[]ort.Value{in}, []ort.Value{out}, so)
	so.Destroy()
	if err != nil {
		ortx.DestroyF32(in, out)
		return nil, fmt.Errorf("加载 SFace 失败（%s）: %w", modelPath, err)
	}

	return &Recognizer{opts: o, sess: sess, in: in, out: out, device: dev}, nil
}

// EmbedFace 直接对「原图 + 五点 landmark」完成对齐与特征提取（128 维 L2 归一化）。
func (r *Recognizer) EmbedFace(img image.Image, landmarks [5][2]float64) ([]float32, error) {
	return r.EmbedAligned(AlignFace(img, landmarks))
}

// EmbedAligned 对 AlignFace 的输出（NCHW 3×112×112，RGB，0..255）提取特征。
func (r *Recognizer) EmbedAligned(px []float32) ([]float32, error) {
	if len(px) != 3*FaceSize*FaceSize {
		return nil, fmt.Errorf("对齐像素长度应为 %d，实得 %d", 3*FaceSize*FaceSize, len(px))
	}

	// 会话与复用张量非并发安全：先加锁，再触碰共享输入/输出张量（写输入→Run→拷输出 全段串行化）。
	r.mu.Lock()
	defer r.mu.Unlock()

	dst := r.in.GetData()
	if len(dst) != len(px) {
		return nil, fmt.Errorf("人脸输入张量长度不符: %d != %d", len(dst), len(px))
	}
	copy(dst, px)

	if err := r.sess.Run(); err != nil {
		return nil, fmt.Errorf("人脸特征推理失败: %w", err)
	}
	src := r.out.GetData()
	if len(src) != EmbeddingDim {
		return nil, fmt.Errorf("人脸输出维度应为 %d，实得 %d", EmbeddingDim, len(src))
	}
	res := make([]float32, EmbeddingDim)
	copy(res, src)
	return vecutil.L2Normalize(res), nil
}

// Device 返回实际生效的推理设备。
func (r *Recognizer) Device() ortx.DeviceKind { return r.device }

// Provider 返回执行提供器名称。
func (r *Recognizer) Provider() string { return ortx.ProviderName(r.device) }

// Close 释放会话与张量。
func (r *Recognizer) Close() {
	if r.sess != nil {
		r.sess.Destroy()
	}
	ortx.DestroyF32(r.in, r.out)
}

// lastPositiveDim 取形状末维（>0 时）；动态或空返回 0。
func lastPositiveDim(dims []int64) int64 {
	if len(dims) == 0 {
		return 0
	}
	last := dims[len(dims)-1]
	if last <= 0 {
		return 0
	}
	return last
}
