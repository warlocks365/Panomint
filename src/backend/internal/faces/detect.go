//go:build cgo

package faces

// YuNet 人脸检测器（ONNX Runtime）。

import (
	"fmt"
	"image"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"panoalbum/internal/ortx"
)

// Detector YuNet 检测器（会话非并发安全，内部加锁串行化）。
type Detector struct {
	opts   Options
	size   int
	sess   *ort.AdvancedSession
	plans  []headPlan
	in     *ort.Tensor[float32]
	outs   []*ort.Tensor[float32]
	device ortx.DeviceKind
	mu     sync.Mutex
}

// NewDetector 加载 YuNet 并装配会话。
//
// 输入尺寸取 Options.InputSize，其次模型声明，最后 DefaultInputSize；
// 输出头按名字解析（cls_8/obj_8/bbox_8/kps_8/... ），名字不可用时按形状回退。
func NewDetector(o Options) (*Detector, error) {
	o = o.WithDefaults()
	modelPath, err := loadModelFile(o.ModelDir, yuNetCandidates)
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
	if len(ins) != 1 {
		return nil, fmt.Errorf("YuNet 期望 1 个输入，实得 %d", len(ins))
	}
	if ins[0].DataType != ort.TensorElementDataTypeFloat {
		return nil, fmt.Errorf("YuNet 输入应为 float32，实得 %s", ins[0].DataType)
	}
	size := o.InputSize
	if size <= 0 {
		size = declaredSquare(ins[0].Dimensions)
	}
	if size <= 0 {
		size = DefaultInputSize
	}

	names := make([]string, len(outs))
	dims := make([][]int64, len(outs))
	for i := range outs {
		names[i] = outs[i].Name
		dims[i] = []int64(outs[i].Dimensions)
	}
	plans, err := resolveHeads(names, dims, size)
	if err != nil {
		return nil, err
	}

	in, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 3, int64(size), int64(size)))
	if err != nil {
		return nil, fmt.Errorf("创建检测输入张量失败: %w", err)
	}
	outTensors := make([]*ort.Tensor[float32], 0, len(plans))
	for i, p := range plans {
		shape := declaredShape(p.Dims)
		if shape == nil {
			shape = ort.NewShape(1, int64(p.Anchors), int64(headChannel(p.Kind)))
		}
		t, err := ort.NewEmptyTensor[float32](shape)
		if err != nil {
			ortx.DestroyF32(append(outTensors, in)...)
			return nil, fmt.Errorf("创建检测输出张量失败（%s）: %w", names[i], err)
		}
		outTensors = append(outTensors, t)
	}

	so, dev, err := ortx.NewSessionOptions(ortx.Config{
		LibPath:       o.LibPath,
		Device:        o.Device,
		DeviceID:      o.DeviceID,
		IntraThreads:  o.IntraThreads,
		GpuMemLimitMB: o.GpuMemLimitMB,
	})
	if err != nil {
		ortx.DestroyF32(append(outTensors, in)...)
		return nil, err
	}
	values := make([]ort.Value, len(outTensors))
	for i, t := range outTensors {
		values[i] = t
	}
	sess, err := ort.NewAdvancedSession(modelPath, []string{ins[0].Name}, names,
		[]ort.Value{in}, values, so)
	so.Destroy()
	if err != nil {
		ortx.DestroyF32(append(outTensors, in)...)
		return nil, fmt.Errorf("加载 YuNet 失败（%s）: %w", modelPath, err)
	}

	return &Detector{
		opts:   o,
		size:   size,
		sess:   sess,
		plans:  plans,
		in:     in,
		outs:   outTensors,
		device: dev,
	}, nil
}

// Detect 在整图上检测人脸，返回**源图坐标**下、经 NMS 与最小尺寸过滤的结果。
func (d *Detector) Detect(img image.Image) ([]Detection, error) {
	px, lb := LetterboxPixels(img, d.size)
	dst := d.in.GetData()
	if len(dst) != len(px) {
		return nil, fmt.Errorf("检测输入张量长度不符: %d != %d", len(dst), len(px))
	}
	copy(dst, px)

	// 会话与输出张量被复用：持锁完成 Run 并拷贝输出，随后即可解锁解码。
	d.mu.Lock()
	if err := d.sess.Run(); err != nil {
		d.mu.Unlock()
		return nil, fmt.Errorf("人脸检测推理失败: %w", err)
	}
	heads := make([]YunetHeadData, len(d.plans))
	for i, p := range d.plans {
		src := d.outs[i].GetData()
		buf := make([]float32, len(src))
		copy(buf, src)
		heads[i] = YunetHeadData{Kind: p.Kind, Stride: p.Stride, Data: buf}
	}
	d.mu.Unlock()

	dets, err := DecodeYuNet(YunetOutputs{InputSize: d.size, Heads: heads},
		d.opts.ConfThreshold, d.opts.NMSThreshold)
	if err != nil {
		return nil, err
	}
	out := make([]Detection, 0, len(dets))
	minPx := float64(d.opts.MinFacePx)
	for _, det := range dets {
		m := lb.MapDetection(det)
		if m.W < minPx || m.H < minPx {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// Size 返回检测输入边长（便于诊断）。
func (d *Detector) Size() int { return d.size }

// Device 返回实际生效的推理设备。
func (d *Detector) Device() ortx.DeviceKind { return d.device }

// Provider 返回执行提供器名称。
func (d *Detector) Provider() string { return ortx.ProviderName(d.device) }

// Close 释放会话与张量。
func (d *Detector) Close() {
	if d.sess != nil {
		d.sess.Destroy()
	}
	ts := append([]*ort.Tensor[float32]{d.in}, d.outs...)
	ortx.DestroyF32(ts...)
}

// declaredSquare 从 [1,3,H,W] 取等边的 H==W；非正方或动态维返回 0。
func declaredSquare(dims []int64) int {
	if len(dims) != 4 {
		return 0
	}
	h, w := dims[2], dims[3]
	if h > 0 && h == w {
		return int(h)
	}
	return 0
}

// declaredShape 把模型声明的形状转成 ort.Shape；含动态维（<=0）时返回 nil。
func declaredShape(dims []int64) ort.Shape {
	if len(dims) == 0 {
		return nil
	}
	for _, d := range dims {
		if d <= 0 {
			return nil
		}
	}
	return ort.NewShape(dims...)
}
