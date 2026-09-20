//go:build cgo

package faces

// YuNet 人脸检测器（ONNX Runtime）。
//
// 输入边长**按源图长边自适应**（规则见 yunet.go resolveInputSize 与 face.go
// DefaultInputMax）：LG 缩略图宽 1280，而输入过去被钉死在 640×640，等于白丢
// 一半线性分辨率、大合影里每张脸再砍半 → 漏检。
//
// 自适应只在模型输入为**动态**时生效；静态图（如现役 2023mar 的 [1,3,640,640]）
// 仍只能用声明尺寸，因为 ONNX Runtime 会拒绝其它边长（见 NewDetector 的静态判定）。

import (
	"fmt"
	"image"
	"log"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"panoalbum/internal/ortx"
)

// inputCacheMax 按输入边长缓存的张量组数上限（FIFO 淘汰）。
//
// 为什么要缓存：每组张量含输入 1×3×S×S float32 —— S=1280 时约 **19.7MB**
// （3×1280×1280×4B），加 12 个输出头（1280 下合计约 1.9MB）。若每张图都重新
// 分配/释放，常驻的 watch 服务会持续制造 20MB 级垃圾 → GC 压力与停顿。
//
// 为什么设上界：来源尺寸理论上可五花八门（实际是 LG 缩略图，宽固定 1280，
// 常见尺寸只有 1~2 个），上界用于封住隐性内存增长；淘汰时立即 Destroy 张量。
const inputCacheMax = 4

// tensorSet 某一输入边长对应的一整套张量（输入 + 12 个输出头）与解码头规划。
type tensorSet struct {
	plans []headPlan
	in    *ort.Tensor[float32]
	outs  []*ort.Tensor[float32]
	// 传给 ORT 的 Value 切片：[0] 为输入，其后与 outputNames 顺序一一对应。
	// 预先构造并复用，避免每张图都重新拼一次切片。
	runInputs  []ort.Value
	runOutputs []ort.Value
}

// Destroy 释放该尺寸组的全部张量（session 不持有它们，须由本方法显式释放）。
func (t *tensorSet) Destroy() {
	ortx.DestroyF32(append([]*ort.Tensor[float32]{t.in}, t.outs...)...)
}

// Detector YuNet 检测器（会话非并发安全，内部加锁串行化）。
type Detector struct {
	opts Options
	// 模型接口信息：任意边长的张量组都要按它们重建解码头规划。
	inputName   string
	outputNames []string
	outputDims  [][]int64
	// staticSize >0 = 模型声明的静态输入边长（此时自适应失效，只能用该值）；0 = 动态。
	staticSize int
	// baseSize 拿不到源图尺寸时的默认边长（静态模型=声明值；动态模型=DefaultInputSize）。
	baseSize int
	sess     *ort.DynamicAdvancedSession
	// sets/order 按输入边长缓存张量组，order 为 FIFO 淘汰顺序；均由 mu 保护。
	sets   map[int]*tensorSet
	order  []int
	device ortx.DeviceKind
	mu     sync.Mutex
}

// NewDetector 加载 YuNet 并装配会话。
//
// 输入边长的确定规则（详见 yunet.go resolveInputSize）：
// Options.InputSize 显式指定 > 模型静态声明尺寸 > 按源图长边自适应（仅动态模型）。
//
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

	// 模型声明的静态边长。含符号维/动态维时返回 0 —— 此时才能按源自适应。
	staticSize := declaredSquare(ins[0].Dimensions)
	if staticSize > 0 && o.InputSize > 0 && o.InputSize != staticSize {
		return nil, fmt.Errorf(
			"检测输入边长被显式设为 %d，但模型 %s 声明的是静态输入 %d：静态图喂入其它边长会被 ONNX Runtime 直接拒绝。"+
				"请去掉 FACE_INPUT_SIZE/-inputsize，或改用动态输入导出（graph input 为 [1,3,height,width]，"+
				"如 face_detection_yunet_2026may.onnx）",
			o.InputSize, modelPath, staticSize)
	}
	baseSize := resolveInputSize(o.InputSize, staticSize, 0, DefaultInputSize, o.InputMax)

	names := make([]string, len(outs))
	dims := make([][]int64, len(outs))
	for i := range outs {
		names[i] = outs[i].Name
		dims[i] = []int64(outs[i].Dimensions)
	}

	// 先按默认边长装配一组张量：既趁早校验解码头（而不是等第一张图），
	// 也保证静态模型（自适应失效）下与改造前的内存行为一致。
	defSet, err := newTensorSet(names, dims, baseSize)
	if err != nil {
		return nil, err
	}

	so, dev, err := ortx.NewSessionOptions(ortx.Config{
		LibPath:       o.LibPath,
		Device:        o.Device,
		DeviceID:      o.DeviceID,
		IntraThreads:  o.IntraThreads,
		GpuMemLimitMB: o.GpuMemLimitMB,
	})
	if err != nil {
		defSet.Destroy()
		return nil, err
	}
	// 动态会话：张量在每次 Run 时传入，因而允许同一会话服务多种输入边长
	// （这是"按源自适应"的前提；普通 AdvancedSession 在创建时就绑死了张量形状）。
	sess, err := ort.NewDynamicAdvancedSession(modelPath, []string{ins[0].Name}, names, so)
	so.Destroy()
	if err != nil {
		defSet.Destroy()
		return nil, fmt.Errorf("加载 YuNet 失败（%s）: %w", modelPath, err)
	}

	adaptive := "自适应失效（模型静态）"
	if staticSize <= 0 {
		adaptive = fmt.Sprintf("自适应生效（动态输入，按源长边取 %d..%d）", DefaultInputSize, o.InputMax)
	}
	log.Printf("YuNet 输入边长：模型 %s 声明 %s，默认 %d，上限 %d → %s",
		modelPath, describeInputShape(staticSize), baseSize, o.InputMax, adaptive)

	return &Detector{
		opts:        o,
		inputName:   ins[0].Name,
		outputNames: names,
		outputDims:  dims,
		staticSize:  staticSize,
		baseSize:    baseSize,
		sess:        sess,
		sets:        map[int]*tensorSet{baseSize: defSet},
		order:       []int{baseSize},
		device:      dev,
	}, nil
}

// Detect 在整图上检测人脸，返回**源图坐标**下、经 NMS 与最小尺寸过滤的结果。
//
// 输入边长按源图长边自适应（resolveInputSize）：LG 缩略图（宽 1280）会以
// 1280 画布 1:1 送入，不再降采样；长边 ≤640 的小图仍是 640，行为与改造前一致。
//
// ⚠️ minpx 的坐标系不因输入变大而漂移：lb 由**本次实际使用的 size** 推导，
// MapDetection 又用同一个 lb 反向映射，所以"画布坐标 → 源图坐标"的缩放系数恒为
// letterbox 实际缩放系数的倒数，与 size 取值无关。
//
// ⚠️ minpx 标定在 **LG/1280 坐标系**（见 Options.MinFacePx / faces.LGWidth），
// 而本函数返回的是当次图源坐标——喂原图（宽 ≠1280）时必须按 `MinFacePx × 图源宽 / LGWidth`
// 换算，否则 24px@LG 在原图上会被当成 24px@原图（≈8px@LG），过滤阈值语义漂移（P0-02）。
func (d *Detector) Detect(img image.Image) ([]Detection, error) {
	b := img.Bounds()
	srcLong := b.Dx()
	if b.Dy() > srcLong {
		srcLong = b.Dy()
	}
	size := resolveInputSize(d.opts.InputSize, d.staticSize, srcLong,
		DefaultInputSize, d.opts.InputMax)
	return d.detectAt(size, img)
}

// DetectMulti 多尺度检测（Job000041）：按 multiSizes 规划跑一个或多个输入边长，
// 各尺度结果都已映射回**图源坐标**，跨尺度 IoU 去重后返回并集。
//
// 单尺度场景（未开启多尺度 / 静态模型 / 小图）与 Detect 完全同路径——
// multiSizes 返回单元素列表时二者行为逐字节一致（mergeScales 单尺度过车=恒等，
// 仍保留检测器内部 NMS 的结果）。
func (d *Detector) DetectMulti(img image.Image) ([]Detection, error) {
	b := img.Bounds()
	srcLong := b.Dx()
	if b.Dy() > srcLong {
		srcLong = b.Dy()
	}
	adaptive := resolveInputSize(d.opts.InputSize, d.staticSize, srcLong,
		DefaultInputSize, d.opts.InputMax)
	sizes := multiSizes(adaptive, d.staticSize, d.opts.MultiScale)
	if len(sizes) == 1 {
		return d.detectAt(sizes[0], img)
	}
	byScale := make([][]Detection, 0, len(sizes))
	for _, s := range sizes {
		dets, err := d.detectAt(s, img)
		if err != nil {
			return nil, err
		}
		byScale = append(byScale, dets)
	}
	return mergeScales(byScale), nil
}

// detectAt 在给定输入边长下跑一遍检测（letterbox → 推理 → 解码 → minpx 过滤），
// 返回**源图坐标**结果。Detect/DetectMulti 的唯一执行体。
func (d *Detector) detectAt(size int, img image.Image) ([]Detection, error) {
	b := img.Bounds()
	px, lb := LetterboxPixels(img, size)

	// 会话与输出张量被复用：持锁取张量组、完成 Run 并拷贝输出，随后即可解锁解码。
	d.mu.Lock()
	set, err := d.setFor(size)
	if err != nil {
		d.mu.Unlock()
		return nil, err
	}
	dst := set.in.GetData()
	if len(dst) != len(px) {
		d.mu.Unlock()
		return nil, fmt.Errorf("检测输入张量长度不符: %d != %d（输入边长 %d）", len(dst), len(px), size)
	}
	copy(dst, px)

	if err := d.sess.Run(set.runInputs, set.runOutputs); err != nil {
		d.mu.Unlock()
		return nil, fmt.Errorf("人脸检测推理失败（输入边长 %d）: %w", size, err)
	}
	heads := make([]YunetHeadData, len(set.plans))
	for i, p := range set.plans {
		src := set.outs[i].GetData()
		buf := make([]float32, len(src))
		copy(buf, src)
		heads[i] = YunetHeadData{Kind: p.Kind, Stride: p.Stride, Data: buf}
	}
	d.mu.Unlock()

	dets, err := DecodeYuNet(YunetOutputs{InputSize: size, Heads: heads},
		d.opts.ConfThreshold, d.opts.NMSThreshold)
	if err != nil {
		return nil, err
	}
	out := make([]Detection, 0, len(dets))
	// MinFacePx 标定在 LG/1280 坐标系：换算到当次图源坐标再判定（源宽 1280 时与旧行为一致）。
	minPx := float64(d.opts.MinFacePx) * float64(b.Dx()) / float64(LGWidth)
	for _, det := range dets {
		m := lb.MapDetection(det)
		if m.W < minPx || m.H < minPx {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// setFor 取（必要时创建）指定输入边长的张量组；**调用方必须已持有 d.mu**。
//
// 分辨率论证：现有解码头按名字带 stride，锚点数 = (size/stride)²，
// 故不同 size 必须各有一套形状不同的输出张量，不能复用。
func (d *Detector) setFor(size int) (*tensorSet, error) {
	if s, ok := d.sets[size]; ok {
		return s, nil
	}
	s, err := newTensorSet(d.outputNames, d.outputDims, size)
	if err != nil {
		return nil, err
	}
	for len(d.order) >= inputCacheMax {
		old := d.order[0]
		d.order = d.order[1:]
		if o, ok := d.sets[old]; ok {
			o.Destroy()
			delete(d.sets, old)
		}
	}
	d.sets[size] = s
	d.order = append(d.order, size)
	return s, nil
}

// newTensorSet 按给定输入边长装配一组张量（1 输入 + 12 输出头）。
func newTensorSet(names []string, dims [][]int64, size int) (*tensorSet, error) {
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
			return nil, fmt.Errorf("创建检测输出张量失败（%s，输入边长 %d）: %w", names[i], size, err)
		}
		outTensors = append(outTensors, t)
	}
	set := &tensorSet{
		plans:      plans,
		in:         in,
		outs:       outTensors,
		runInputs:  []ort.Value{in},
		runOutputs: make([]ort.Value, len(outTensors)),
	}
	for i, t := range outTensors {
		set.runOutputs[i] = t
	}
	return set, nil
}

// Size 返回默认检测输入边长（便于诊断）。
//
// 动态模型下实际使用的边长按源图自适应（见 Detect / resolveInputSize），
// 可能大于本值；静态模型下本值即唯一可能值。
func (d *Detector) Size() int { return d.baseSize }

// Device 返回实际生效的推理设备。
func (d *Detector) Device() ortx.DeviceKind { return d.device }

// Provider 返回执行提供器名称。
func (d *Detector) Provider() string { return ortx.ProviderName(d.device) }

// Close 释放会话与所有缓存张量。
func (d *Detector) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.sess != nil {
		d.sess.Destroy()
		d.sess = nil
	}
	for _, s := range d.sets {
		s.Destroy()
	}
	d.sets = nil
	d.order = nil
}

// describeInputShape 生成给日志看的输入形状描述（静态给具体边长，动态给"动态"）。
func describeInputShape(staticSize int) string {
	if staticSize > 0 {
		return fmt.Sprintf("静态输入 [1,3,%d,%d]", staticSize, staticSize)
	}
	return "动态输入 [1,3,height,width]"
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
