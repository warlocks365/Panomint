package faces

// YuNet 检测后处理：letterbox 预处理 + 12 头解码 + NMS，以及检测输入边长的推导。
//
// 本文件为**纯逻辑**（不依赖 CGO/ORT），便于在无 ORT 的环境下单测解码、坐标映射
// 与输入尺寸推导（resolveInputSize / roundUp32）。
//
// 模型输出（opencv_zoo face_detection_yunet_2023mar / 2026may）为 12 个张量，
// stride s ∈ {8, 16, 32}，每个 stride 各有 4 个头（锚点总数 A = (S/s)²）：
//
//	cls_s  [1, A, 1]   分类得分
//	obj_s  [1, A, 1]   目标置信（IoU 质量）
//	bbox_s [1, A, 4]   框回归 [dx, dy, dw, dh]（相对锚点原点，dw/dh 为 log 尺度）
//	kps_s  [1, A, 10]  5 点 landmark（相对锚点偏移）
//
// 解码（对齐 OpenCV FaceDetectorYNImpl::generateProposals）：
//
//	score = sqrt(cls * obj)
//	cx = (col + dx) * s ; cy = (row + dy) * s
//	w  = exp(dw) * s     ; h  = exp(dh) * s
//	landmark_j = ((kps[2j] + col) * s, (kps[2j+1] + row) * s)
//	col = i % (S/s) ; row = i / (S/s)

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
	// 媒体缩略图为 WebP（*_LG.webp）：标准库不解码，缺此注册会报 "image: unknown format"。
	_ "golang.org/x/image/webp"
)

// DecodeImage 解码图片文件（含 WebP）。
//
// 媒体缩略图由处理管线输出为 WebP，故此处统一注册 webp 解码器；
// 人脸检测/对齐直接从缩略图取像素，不依赖原始媒体文件。
func DecodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片失败: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	return img, nil
}

// letterboxPad 填充值（0=黑）。YuNet 训练时未使用 letterbox，
// 此处按主流实现（od_opencv 等）用等比缩放 + 0 填充，避免长宽比失真伤小脸。
//
// 明确性质：**是补边（pad），不是拉伸（stretch）**——LetterboxFor 的 Scale 取
// min(size/srcW, size/srcH)，两轴同一个系数，短轴两侧补 0；故非正方形源图
// （如 LG 的 1280×791）进网后人脸长宽比不变，只是外围填黑。
// 用例见 faces_test.go TestLetterboxPadsNonSquareSource。
const letterboxPad = 0

// Letterbox 等比缩放 + 居中填充的坐标变换参数。
type Letterbox struct {
	Size       int     // 目标画布边长
	SrcW, SrcH int     // 源图尺寸
	Scale      float64 // 源图 → 画布 的缩放比
	PadX, PadY float64 // 画布内的左侧/顶部填充量
}

// LetterboxFor 计算把 srcW×srcH 装进 size×size 画布的变换参数。
func LetterboxFor(srcW, srcH, size int) Letterbox {
	if srcW <= 0 || srcH <= 0 || size <= 0 {
		return Letterbox{Size: size, SrcW: srcW, SrcH: srcH, Scale: 1}
	}
	scale := math.Min(float64(size)/float64(srcW), float64(size)/float64(srcH))
	newW := int(math.Round(float64(srcW) * scale))
	newH := int(math.Round(float64(srcH) * scale))
	return Letterbox{
		Size:  size,
		SrcW:  srcW,
		SrcH:  srcH,
		Scale: scale,
		PadX:  float64(size-newW) / 2,
		PadY:  float64(size-newH) / 2,
	}
}

// ToCanvas 源图坐标 → 画布坐标。
func (l Letterbox) ToCanvas(x, y float64) (float64, float64) {
	return x*l.Scale + l.PadX, y*l.Scale + l.PadY
}

// ToSource 画布坐标 → 源图坐标。
func (l Letterbox) ToSource(x, y float64) (float64, float64) {
	if l.Scale == 0 {
		return x, y
	}
	return (x - l.PadX) / l.Scale, (y - l.PadY) / l.Scale
}

// MapDetection 把画布坐标下的检测框与 landmark 映射回源图坐标。
func (l Letterbox) MapDetection(d Detection) Detection {
	x1, y1 := l.ToSource(d.X, d.Y)
	x2, y2 := l.ToSource(d.X+d.W, d.Y+d.H)
	d.X, d.Y, d.W, d.H = x1, y1, x2-x1, y2-y1
	for i := range d.Landmarks {
		d.Landmarks[i][0], d.Landmarks[i][1] = l.ToSource(d.Landmarks[i][0], d.Landmarks[i][1])
	}
	return d
}

// LetterboxPixels 源图 → size×size 的 NCHW float32 张量。
//
// 通道顺序为 **BGR**、取值 **0..255 不做归一化**——与 OpenCV FaceDetectorYN
// 直接送 BGR 帧给 YuNet 的行为一致。
func LetterboxPixels(img image.Image, size int) ([]float32, Letterbox) {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	lb := LetterboxFor(srcW, srcH, size)

	out := make([]float32, 3*size*size)
	if lb.Scale == 0 || srcW == 0 || srcH == 0 {
		return out, lb
	}
	newW := int(math.Round(float64(srcW) * lb.Scale))
	newH := int(math.Round(float64(srcH) * lb.Scale))
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}

	// 画布已零初始化，填充区天然为 letterboxPad
	offX := int(math.Round(lb.PadX))
	offY := int(math.Round(lb.PadY))
	total := size * size

	// 1:1 快路径：缩放后与源图等尺寸时不重采样，直接逐像素取 BGR。
	// 自适应输入下这正是**常见**情形（LG 宽 1280、size 推导为 1280 → scale=1）：
	// 走 CatmullRom 除了白付一次重采样代价，还会在 1:1 上引入本不该有的插值。
	if newW == srcW && newH == srcH {
		copyToCanvas(out, img, b.Min.X, b.Min.Y, srcW, srcH, offX, offY, size, total)
		return out, lb
	}

	// 等比缩放（CatmullRom 近似双三次）
	resized := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, b, xdraw.Over, nil)
	copyToCanvas(out, resized, 0, 0, newW, newH, offX, offY, size, total)
	return out, lb
}

// copyToCanvas 把 src 上以 (x0,y0) 为左上、w×h 大小的区域按 BGR/0..255 写进
// size×size 画布的 (offX,offY) 处（越界像素丢弃，填充区保持零值）。
func copyToCanvas(out []float32, src image.Image, x0, y0, w, h, offX, offY, size, total int) {
	for y := 0; y < h; y++ {
		row := offY + y
		if row < 0 || row >= size {
			continue
		}
		for x := 0; x < w; x++ {
			col := offX + x
			if col < 0 || col >= size {
				continue
			}
			c := color.NRGBAModel.Convert(src.At(x0+x, y0+y)).(color.NRGBA)
			idx := row*size + col
			// BGR 顺序（OpenCV 约定）
			out[0*total+idx] = float32(c.B)
			out[1*total+idx] = float32(c.G)
			out[2*total+idx] = float32(c.R)
		}
	}
}

// inputAlign 检测输入边长的对齐粒度。
//
// YuNet 三个输出头的 stride 为 8/16/32，锚点数按 (S/stride)² 计算，
// 只有 S 是 32 的整数倍时三者才同时整除（resolveHeads 也要求 inputSize%stride==0）。
// 取 32 是最安全的对齐粒度。
const inputAlign = 32

// roundUp32 向上取到 32 的整数倍；n<=0 返回 0。
func roundUp32(n int) int {
	if n <= 0 {
		return 0
	}
	return (n + inputAlign - 1) / inputAlign * inputAlign
}

// resolveInputSize 计算一次检测实际使用的输入边长（**纯函数**，便于单测）。
//
// 优先级（前两条是既有语义，第三条是本次新增的自适应）：
//
//  1. explicit > 0    → 显式值（FACE_INPUT_SIZE / -inputsize）绝对优先，不再推导；
//  2. staticSize > 0  → **模型声明的静态边长**。必须用它：静态图喂别的尺寸，
//     ONNX Runtime 直接报错（比漏检更糟）。自适应在此让位；
//  3. 其它（动态模型） → clamp(roundUp32(srcLong), base, max)。
//
// base 是下限（现默认 640，小图不得退化），max 是上限（DefaultInputMax）。
// srcLong<=0（源尺寸未知，如构造期）时推导结果为 base。
// max<base 时按 base 处理，保证 FACE_INPUT_MAX 不会被误设成比下限还小。
func resolveInputSize(explicit, staticSize, srcLong, base, max int) int {
	if base <= 0 {
		base = DefaultInputSize
	}
	if max < base {
		max = base
	}
	if explicit > 0 {
		return explicit
	}
	if staticSize > 0 {
		return staticSize
	}
	d := roundUp32(srcLong)
	if d < base {
		return base
	}
	if d > max {
		return max
	}
	return d
}

// headKind 解码头类别。
type headKind int

const (
	kindCls headKind = iota
	kindObj
	kindBBox
	kindKps
)

func (k headKind) String() string {
	switch k {
	case kindCls:
		return "cls"
	case kindObj:
		return "obj"
	case kindBBox:
		return "bbox"
	default:
		return "kps"
	}
}

// headSpec 一个已解析的解码头：类别 + 元素数（锚点数 × 通道数）。
type headSpec struct {
	Kind    headKind
	Stride  int
	Anchors int
}

// YunetOutputs 12 个检测头的原始输出（与具体 ORT 张量解耦，便于单测）。
type YunetOutputs struct {
	InputSize int
	Heads     []YunetHeadData
}

// YunetHeadData 单个头的原始数据。
type YunetHeadData struct {
	Kind   headKind
	Stride int
	Data   []float32
}

// headChannel 每个头的通道数。
func headChannel(k headKind) int {
	switch k {
	case kindCls, kindObj:
		return 1
	case kindBBox:
		return 4
	default:
		return 10
	}
}

// headPlan 一个输出张量对应的解码头规划（与模型输出顺序一一对应）。
type headPlan struct {
	Kind    headKind
	Stride  int
	Anchors int
	Dims    []int64 // 模型声明的形状（可含 -1；不可用时由 Anchors/通道推导）
}

// headNameRe 从输出名提取类别与 stride，例如 "cls_8" / "bbox_16" / "kps32"。
var headNameRe = regexp.MustCompile(`(?i)^.*?(cls|obj|bbox|kps)[^0-9]*([0-9]+).*$`)

// resolveHeads 依据输出名 / 形状判定每个输出张量属于哪个解码头。
//
// 优先按名字（opencv_zoo 标准导出为 cls_8/obj_8/bbox_8/kps_8/cls_16/... ）；
// 名字不可用时回退到「形状 + 锚点数」推断。两者都无法得到完整的
// 4 类 × 3 stride 时返回错误（附观测到的名字与形状，便于排查）。
func resolveHeads(names []string, dims [][]int64, inputSize int) ([]headPlan, error) {
	plans := make([]headPlan, len(names))

	// ---- 1) 名字优先 ----
	named := 0
	for i, n := range names {
		m := headNameRe.FindStringSubmatch(n)
		if m == nil {
			continue
		}
		kind, err := parseHeadKind(m[1])
		if err != nil {
			continue
		}
		stride, err := strconv.Atoi(m[2])
		if err != nil || stride <= 0 || inputSize%stride != 0 {
			continue
		}
		plans[i] = headPlan{Kind: kind, Stride: stride, Anchors: (inputSize / stride) * (inputSize / stride), Dims: dims[i]}
		named++
	}
	if named > 0 {
		if err := validateHeads(plans); err == nil {
			return plans, nil
		}
	}

	// ---- 2) 形状回退 ----
	fallback := make([]headPlan, len(names))
	clsSeen := map[int]bool{}
	for i := range names {
		if len(dims[i]) == 0 {
			return nil, headsError(names, dims, "输出形状未知，无法回退推断")
		}
		last := dims[i][len(dims[i])-1]
		if last <= 0 {
			return nil, headsError(names, dims, "输出末维为动态（-1），无法回退推断")
		}
		count := 1
		for _, d := range dims[i] {
			if d <= 0 {
				return nil, headsError(names, dims, "输出形状含动态维，无法回退推断")
			}
			count *= int(d)
		}
		anchors := count / int(last)
		side := int(math.Round(math.Sqrt(float64(anchors))))
		if side <= 0 || side*side != anchors || inputSize%side != 0 {
			return nil, headsError(names, dims, fmt.Sprintf("锚点数 %d 无法解析为方形网格", anchors))
		}
		stride := inputSize / side

		var kind headKind
		switch last {
		case 4:
			kind = kindBBox
		case 10:
			kind = kindKps
		case 1:
			// 两个 1 通道头在标准导出中先后为 cls、obj
			if !clsSeen[stride] {
				kind, clsSeen[stride] = kindCls, true
			} else {
				kind = kindObj
			}
		default:
			return nil, headsError(names, dims, fmt.Sprintf("末维 %d 不是 1/4/10", last))
		}
		fallback[i] = headPlan{Kind: kind, Stride: stride, Anchors: anchors, Dims: dims[i]}
	}
	if err := validateHeads(fallback); err != nil {
		return nil, headsError(names, dims, err.Error())
	}
	return fallback, nil
}

// validateHeads 校验规划覆盖 4 类 × 3 stride 且无空缺。
func validateHeads(plans []headPlan) error {
	seen := map[headKind]map[int]bool{}
	for _, p := range plans {
		if p.Stride == 0 {
			return fmt.Errorf("存在未识别的输出张量")
		}
		if seen[p.Kind] == nil {
			seen[p.Kind] = map[int]bool{}
		}
		if seen[p.Kind][p.Stride] {
			return fmt.Errorf("%s@%d 重复", p.Kind, p.Stride)
		}
		seen[p.Kind][p.Stride] = true
	}
	for _, k := range []headKind{kindCls, kindObj, kindBBox, kindKps} {
		if len(seen[k]) != 3 {
			return fmt.Errorf("%s 头数量 %d，期望 3", k, len(seen[k]))
		}
	}
	return nil
}

func parseHeadKind(s string) (headKind, error) {
	switch s {
	case "cls":
		return kindCls, nil
	case "obj":
		return kindObj, nil
	case "bbox":
		return kindBBox, nil
	case "kps":
		return kindKps, nil
	}
	return kindCls, fmt.Errorf("未知头类别 %q", s)
}

func headsError(names []string, dims [][]int64, why string) error {
	var sb strings.Builder
	for i, n := range names {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "%s=%v", n, dims[i])
	}
	return fmt.Errorf("无法解析 YuNet 输出头（%s）；请使用 opencv_zoo 标准导出。观测到：[%s]", why, sb.String())
}

// DecodeYuNet 解码 12 头输出为检测框（坐标仍为**画布**坐标系）。
//
// 兼容两种排布：[1, A, C] 与 [A, C]（按元素个数推导，元素数必须能被通道数整除）。
// 若模型导出为「单张量拼接 14 列」的变体，此处会因元素数不匹配而报错，
// 提示改用 opencv_zoo 的标准导出。
func DecodeYuNet(o YunetOutputs, conf, nms float64) ([]Detection, error) {
	if o.InputSize <= 0 {
		return nil, fmt.Errorf("输入尺寸无效: %d", o.InputSize)
	}
	byStride := map[int]map[headKind]*YunetHeadData{}
	for i := range o.Heads {
		h := &o.Heads[i]
		if h.Stride <= 0 {
			return nil, fmt.Errorf("非法 stride: %d", h.Stride)
		}
		ch := headChannel(h.Kind)
		if len(h.Data) == 0 || len(h.Data)%ch != 0 {
			return nil, fmt.Errorf("头 %s@%d 元素数 %d 与通道数 %d 不匹配（可能不是 opencv_zoo 标准导出）",
				h.Kind, h.Stride, len(h.Data), ch)
		}
		if byStride[h.Stride] == nil {
			byStride[h.Stride] = map[headKind]*YunetHeadData{}
		}
		byStride[h.Stride][h.Kind] = h
	}

	var dets []Detection
	for _, stride := range sortedStrides(byStride) {
		heads := byStride[stride]
		cls, ok1 := heads[kindCls]
		obj, ok2 := heads[kindObj]
		bbox, ok3 := heads[kindBBox]
		kps, ok4 := heads[kindKps]
		if !(ok1 && ok2 && ok3 && ok4) {
			return nil, fmt.Errorf("stride %d 缺少解码头（cls/obj/bbox/kps）", stride)
		}
		side := o.InputSize / stride
		if side <= 0 {
			return nil, fmt.Errorf("stride %d 与输入尺寸 %d 不匹配", stride, o.InputSize)
		}
		cols, rows := side, side
		anchors := cols * rows
		if len(cls.Data) != anchors || len(obj.Data) != anchors ||
			len(bbox.Data) != anchors*4 || len(kps.Data) != anchors*10 {
			return nil, fmt.Errorf("stride %d 锚点数不符：期望 %d（cls=%d obj=%d bbox=%d kps=%d）",
				stride, anchors, len(cls.Data), len(obj.Data), len(bbox.Data), len(kps.Data))
		}

		for i := 0; i < anchors; i++ {
			score := math.Sqrt(math.Max(0, math.Min(1, float64(cls.Data[i]))) *
				math.Max(0, math.Min(1, float64(obj.Data[i]))))
			if float64(score) < conf {
				continue
			}
			col := i % cols
			row := i / cols
			cx := (float64(col) + float64(bbox.Data[i*4+0])) * float64(stride)
			cy := (float64(row) + float64(bbox.Data[i*4+1])) * float64(stride)
			w := math.Exp(float64(bbox.Data[i*4+2])) * float64(stride)
			h := math.Exp(float64(bbox.Data[i*4+3])) * float64(stride)

			d := Detection{
				X:     cx - w/2,
				Y:     cy - h/2,
				W:     w,
				H:     h,
				Score: score,
			}
			for j := 0; j < 5; j++ {
				d.Landmarks[j][0] = (float64(kps.Data[i*10+2*j]) + float64(col)) * float64(stride)
				d.Landmarks[j][1] = (float64(kps.Data[i*10+2*j+1]) + float64(row)) * float64(stride)
			}
			dets = append(dets, d)
		}
	}
	return nmsDetections(dets, nms), nil
}

func sortedStrides(m map[int]map[headKind]*YunetHeadData) []int {
	out := make([]int, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Ints(out)
	return out
}

// nmsDetections 贪心 NMS（按 score 降序，IoU ≥ iouThresh 的丢弃）。
func nmsDetections(dets []Detection, iouThresh float64) []Detection {
	if len(dets) == 0 {
		return nil
	}
	sort.SliceStable(dets, func(i, j int) bool { return dets[i].Score > dets[j].Score })
	kept := make([]Detection, 0, len(dets))
	for _, d := range dets {
		ok := true
		for _, k := range kept {
			if iou(d, k) >= iouThresh {
				ok = false
				break
			}
		}
		if ok {
			kept = append(kept, d)
		}
	}
	return kept
}

// iou 两框交并比。
func iou(a, b Detection) float64 {
	ax1, ay1, ax2, ay2 := a.X, a.Y, a.X+a.W, a.Y+a.H
	bx1, by1, bx2, by2 := b.X, b.Y, b.X+b.W, b.Y+b.H
	ix1, iy1 := math.Max(ax1, bx1), math.Max(ay1, by1)
	ix2, iy2 := math.Min(ax2, bx2), math.Min(ay2, by2)
	iw, ih := ix2-ix1, iy2-iy1
	if iw <= 0 || ih <= 0 {
		return 0
	}
	inter := iw * ih
	union := a.W*a.H + b.W*b.H - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}
