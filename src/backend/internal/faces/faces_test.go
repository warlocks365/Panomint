package faces

// 纯逻辑单测（无 CGO 依赖，CGO_ENABLED=0 下亦可运行）：
// letterbox 坐标映射与补边性质、检测输入边长推导、YuNet 12 头解码与 NMS、
// 输出头解析、五点相似变换与对齐、聚类判定。
//
// ⚠️ 这些用例只能覆盖**数学/几何**正确性；与 OpenCV 参考实现的逐位一致性
// （输出头命名与解码、对齐模板等价性、SFace 预处理通道序）必须在有 ORT 的
// Linux 环境上做交叉验证——本机 Windows 无法运行 ORT。

import (
	"image"
	"image/color"
	"math"
	"testing"

	xdraw "golang.org/x/image/draw"
)

func TestLetterboxForMapping(t *testing.T) {
	lb := LetterboxFor(1280, 640, 640)
	if math.Abs(lb.Scale-0.5) > 1e-9 {
		t.Fatalf("scale = %v，期望 0.5", lb.Scale)
	}
	if math.Abs(lb.PadX-0) > 1e-9 || math.Abs(lb.PadY-160) > 1e-9 {
		t.Fatalf("pad = (%v,%v)，期望 (0,160)", lb.PadX, lb.PadY)
	}
	// 画布中心应映射回源图中心
	x, y := lb.ToSource(320, 320)
	if math.Abs(x-640) > 1e-9 || math.Abs(y-320) > 1e-9 {
		t.Fatalf("中心映射 = (%v,%v)，期望 (640,320)", x, y)
	}
	// 往返一致
	cx, cy := lb.ToCanvas(x, y)
	if math.Abs(cx-320) > 1e-9 || math.Abs(cy-320) > 1e-9 {
		t.Fatalf("往返 = (%v,%v)，期望 (320,320)", cx, cy)
	}
}

func TestDecodeYuNetSingleAnchor(t *testing.T) {
	const size = 640
	const stride = 32
	anchors := (size / stride) * (size / stride) // 400
	cls := make([]float32, anchors)
	obj := make([]float32, anchors)
	bbox := make([]float32, anchors*4)
	kps := make([]float32, anchors*10)
	// 仅第 0 个锚点为满分（dx=dy=dw=dh=0）
	cls[0], obj[0] = 1, 1

	dets, err := DecodeYuNet(YunetOutputs{
		InputSize: size,
		Heads: []YunetHeadData{
			{Kind: kindCls, Stride: stride, Data: cls},
			{Kind: kindObj, Stride: stride, Data: obj},
			{Kind: kindBBox, Stride: stride, Data: bbox},
			{Kind: kindKps, Stride: stride, Data: kps},
		},
	}, 0.9, 0.3)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if len(dets) != 1 {
		t.Fatalf("命中 %d 张脸，期望 1", len(dets))
	}
	d := dets[0]
	if math.Abs(d.W-stride) > 1e-6 || math.Abs(d.H-stride) > 1e-6 {
		t.Errorf("框尺寸 = (%v,%v)，期望 (%d,%d)", d.W, d.H, stride, stride)
	}
	if math.Abs(d.X-(-16)) > 1e-6 || math.Abs(d.Y-(-16)) > 1e-6 {
		t.Errorf("框左上 = (%v,%v)，期望 (-16,-16)", d.X, d.Y)
	}
	if math.Abs(d.Score-1) > 1e-6 {
		t.Errorf("score = %v，期望 1", d.Score)
	}
}

func TestDecodeYuNetThresholdAndNMS(t *testing.T) {
	const size = 640
	const stride = 32
	anchors := (size / stride) * (size / stride)
	cls := make([]float32, anchors)
	obj := make([]float32, anchors)
	bbox := make([]float32, anchors*4)
	kps := make([]float32, anchors*10)
	// 两个**重叠**的满分框（i=1 通过 dx=-1 平移到与 i=0 同心）+ 一个低分框
	cls[0], obj[0] = 1, 1
	cls[1], obj[1] = 1, 1
	bbox[1*4+0] = -1 // 第 1 个锚点左移一格，与第 0 个完全重合
	cls[2], obj[2] = 0.5, 0.5

	dets, err := DecodeYuNet(YunetOutputs{
		InputSize: size,
		Heads: []YunetHeadData{
			{Kind: kindCls, Stride: stride, Data: cls},
			{Kind: kindObj, Stride: stride, Data: obj},
			{Kind: kindBBox, Stride: stride, Data: bbox},
			{Kind: kindKps, Stride: stride, Data: kps},
		},
	}, 0.9, 0.3)
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if len(dets) != 1 {
		t.Fatalf("低分被过滤 + 同位置 NMS 后应为 1，实得 %d", len(dets))
	}
}

func TestResolveHeadsByName(t *testing.T) {
	var names []string
	var dims [][]int64
	for _, s := range []int64{8, 16, 32} {
		a := (640 / s) * (640 / s)
		names = append(names, "cls_"+itoa(int(s)), "obj_"+itoa(int(s)), "bbox_"+itoa(int(s)), "kps_"+itoa(int(s)))
		dims = append(dims,
			[]int64{1, a, 1}, []int64{1, a, 1}, []int64{1, a, 4}, []int64{1, a, 10})
	}
	plans, err := resolveHeads(names, dims, 640)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(plans) != 12 {
		t.Fatalf("规划 %d 个，期望 12", len(plans))
	}
	for i, p := range plans {
		if p.Stride != 0 && p.Kind == kindCls && names[i] != "cls_"+itoa(p.Stride) {
			t.Errorf("第 %d 项类别/stride 与名字不符: %v vs %s", i, p.Kind, names[i])
		}
	}
}

func TestResolveHeadsFallbackByShape(t *testing.T) {
	// 名字无意义时按形状回退：每个 stride 依次是 [.,1] [.,1] [.,4] [.,10]
	var names []string
	var dims [][]int64
	for _, s := range []int64{8, 16, 32} {
		a := (640 / s) * (640 / s)
		names = append(names, "out_a", "out_b", "out_c", "out_d")
		dims = append(dims,
			[]int64{a, 1}, []int64{a, 1}, []int64{a, 4}, []int64{a, 10})
	}
	plans, err := resolveHeads(names, dims, 640)
	if err != nil {
		t.Fatalf("回退解析失败: %v", err)
	}
	if plans[0].Kind != kindCls || plans[1].Kind != kindObj ||
		plans[2].Kind != kindBBox || plans[3].Kind != kindKps {
		t.Fatalf("回退类别判定错误: %v %v %v %v",
			plans[0].Kind, plans[1].Kind, plans[2].Kind, plans[3].Kind)
	}
}

func TestSimilarityTransformIdentity(t *testing.T) {
	a, b, tx, ty := SimilarityTransform(FaceAlignTemplate, FaceAlignTemplate)
	if math.Abs(a-1) > 1e-9 || math.Abs(b) > 1e-9 || math.Abs(tx) > 1e-9 || math.Abs(ty) > 1e-9 {
		t.Fatalf("自映射应为恒等，实得 a=%v b=%v tx=%v ty=%v", a, b, tx, ty)
	}
}

func TestAlignFaceIdentity(t *testing.T) {
	// landmark == 模板 ⇒ 恒等变换 ⇒ 输出应与源图 112×112 区域逐像素一致（RGB, 0..255）
	img := image.NewRGBA(image.Rect(0, 0, FaceSize, FaceSize))
	want := make([][3]int, 0, FaceSize*FaceSize)
	for y := 0; y < FaceSize; y++ {
		for x := 0; x < FaceSize; x++ {
			c := color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((x + y) % 256), A: 255}
			img.Set(x, y, c)
			want = append(want, [3]int{int(c.R), int(c.G), int(c.B)})
		}
	}
	out := AlignFace(img, FaceAlignTemplate)
	total := FaceSize * FaceSize
	for i := 0; i < total; i++ {
		got := [3]int{int(out[i]), int(out[total+i]), int(out[2*total+i])}
		if got != want[i] {
			t.Fatalf("像素 %d = %v，期望 %v", i, got, want[i])
		}
	}
}

func TestPickCluster(t *testing.T) {
	emb := []float32{1, 0, 0}
	refs := []ClusterRef{
		{ClusterID: "same", Centroid: []float32{0.99, 0.01, 0}},
		{ClusterID: "far", Centroid: []float32{0, 1, 0}},
	}
	if id, _ := PickCluster(emb, refs, 0.33); id != "same" {
		t.Fatalf("命中 %q，期望 same", id)
	}
	if id, _ := PickCluster(emb, refs, 0.999999); id != "" {
		t.Fatalf("阈值下不应命中，实得 %q", id)
	}
	if id, _ := PickCluster(emb, nil, 0.33); id != "" {
		t.Fatalf("无参考时应返回空，实得 %q", id)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// ---- 检测输入边长推导（自适应输入尺寸的核心规则）----

func TestRoundUp32(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, 0}, {-5, 0}, {1, 32}, {31, 32}, {32, 32}, {33, 64},
		{640, 640}, {641, 672}, {1000, 1024}, {1280, 1280}, {1281, 1312},
	}
	for _, c := range cases {
		if got := roundUp32(c.in); got != c.want {
			t.Errorf("roundUp32(%d) = %d，期望 %d", c.in, got, c.want)
		}
	}
}

// TestResolveInputSize 锁死优先级与各条边界：
// **显式值 > 模型静态声明 > 按源长边自适应（仅动态模型）**。
func TestResolveInputSize(t *testing.T) {
	const base, max = 640, 1280
	cases := []struct {
		name                      string
		explicit, static, srcLong int
		want                      int
		why                       string
	}{
		{"显式值优先于自适应", 768, 0, 1280, 768, "FACE_INPUT_SIZE 是运维显式意图，不得被推导覆盖"},
		{"显式值优先于静态声明（相等）", 640, 640, 1280, 640, "显式值等于声明值，等价"},
		{"静态模型压过自适应", 0, 640, 1280, 640, "⚠️ 回归保护：现役 2023mar 是静态 640，喂 1280 会被 ORT 拒绝"},

		{"动态模型 + LG 1280 宽 → 1:1", 0, 0, 1280, 1280, "本次修复的目标：不再把 1280 压成 640"},
		{"动态模型 + 小图长边 640 不变", 0, 0, 640, 640, "小图行为与改造前一致"},
		{"动态模型 + 长边 500 → 下限 640", 0, 0, 500, 640, "不得低于下限，避免小图退化"},
		{"动态模型 + 长边 0（未知）→ 下限", 0, 0, 0, 640, "构造期无源图时退化为默认值"},
		{"动态模型 + 长边 1000 → 32 对齐", 0, 0, 1000, 1024, "向上取 32 的倍数（余数 8 补到 32）"},
		{"动态模型 + 长边 641 → 32 对齐", 0, 0, 641, 672, "640 的下一档"},
		{"动态模型 + 长边 1707 → 上限", 0, 0, 1707, 1280, "竖构图 LG（宽 1280）会超过上限，被 clamp 回 1280"},
		{"上限=640 完全退化回旧行为", 0, 0, 1280, 640, "FACE_INPUT_MAX=640 即回滚开关"},
		{"上限小于下限时按下限", 0, 0, 1280, 640, "误设 FACE_INPUT_MAX=320 不得让输入低于 640"},
	}
	for _, c := range cases {
		mx := max
		if c.name == "上限=640 完全退化回旧行为" {
			mx = 640
		}
		if c.name == "上限小于下限时按下限" {
			mx = 320
		}
		if got := resolveInputSize(c.explicit, c.static, c.srcLong, base, mx); got != c.want {
			t.Errorf("%s：resolveInputSize(%d,%d,%d,640,%d) = %d，期望 %d（%s）",
				c.name, c.explicit, c.static, c.srcLong, mx, got, c.want, c.why)
		}
	}
}

// TestDerivedSizeAcceptedByResolveHeads 证明推导出的边长都能被解码头接受：
// YuNet 的 stride 为 8/16/32，resolveHeads 要求 inputSize%stride==0，
// 取 32 的整数倍正是为此（否则 strided head 的锚点数无法整除）。
func TestDerivedSizeAcceptedByResolveHeads(t *testing.T) {
	for _, srcLong := range []int{0, 320, 640, 791, 1000, 1280, 1707, 2000} {
		size := resolveInputSize(0, 0, srcLong, DefaultInputSize, DefaultInputMax)
		var names []string
		var dims [][]int64
		for _, s := range []int64{8, 16, 32} {
			names = append(names,
				"cls_"+itoa(int(s)), "obj_"+itoa(int(s)), "bbox_"+itoa(int(s)), "kps_"+itoa(int(s)))
			dims = append(dims,
				[]int64{-1, -1, 1}, []int64{-1, -1, 1},
				[]int64{-1, -1, 4}, []int64{-1, -1, 10})
		}
		plans, err := resolveHeads(names, dims, size)
		if err != nil {
			t.Errorf("源长边 %d → 输入边长 %d 无法解析解码头: %v", srcLong, size, err)
			continue
		}
		for _, p := range plans {
			if p.Anchors != (size/p.Stride)*(size/p.Stride) {
				t.Errorf("源长边 %d：stride %d 锚点数 %d，期望 %d",
					srcLong, p.Stride, p.Anchors, (size/p.Stride)*(size/p.Stride))
			}
		}
	}
}

// ---- letterbox：是补边还是拉伸 ----

// TestLetterboxPadsNonSquareSource 用真实 LG 形状（1280×791）证明 letterbox 是
// **等比缩放 + 补边**，而不是拉伸：Scale 取两轴中的小者，短轴靠黑色填充补齐。
func TestLetterboxPadsNonSquareSource(t *testing.T) {
	// 自适应下的目标情形：size=1280 时 scale 恒为 1（1:1，不再重采样）
	if lb := LetterboxFor(1280, 791, 1280); math.Abs(lb.Scale-1) > 1e-12 {
		t.Errorf("1280×791 → 1280 画布 scale = %v，期望 1（1:1）", lb.Scale)
	}

	// 旧行为：size=640 时 1280 宽被压到 0.5
	lb := LetterboxFor(1280, 791, 640)
	if math.Abs(lb.Scale-0.5) > 1e-12 {
		t.Fatalf("scale = %v，期望 0.5", lb.Scale)
	}
	// 若是**拉伸**，X 轴系数会是 640/1280=0.5、Y 轴系数 640/791≈0.809 —— 两轴不同。
	// 补边实现里两轴共用 min(0.5, 0.809)=0.5，差异全部由 PadY 承担。
	if stretched := 640.0 / 791.0; math.Abs(lb.Scale-stretched) < 1e-6 {
		t.Errorf("scale 等于 Y 轴拉伸系数 %v，说明是拉伸而非补边", stretched)
	}
	newH := int(math.Round(791 * lb.Scale))
	wantPadY := float64(640-newH) / 2
	if math.Abs(lb.PadY-wantPadY) > 1e-12 || lb.PadX != 0 {
		t.Errorf("pad = (%v,%v)，期望 (0,%v)", lb.PadX, lb.PadY, wantPadY)
	}
	if wantPadY <= 0 {
		t.Errorf("短轴补边量应 >0，实得 %v", lb.PadY)
	}
	// 补边区（画布顶部）在像素张量里必须是 letterboxPad
	img := image.NewRGBA(image.Rect(0, 0, 1280, 791))
	for y := range 791 {
		for x := range 1280 {
			img.Set(x, y, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		}
	}
	out, lb2 := LetterboxPixels(img, 640)
	total := 640 * 640
	if out[0*total+0] != 0 || out[total+0] != 0 || out[2*total+0] != 0 {
		t.Errorf("左上角（补边区）应为 0，实得 %v", out[0:3])
	}
	// 图像区第一行应有像素值
	offY := int(math.Round(lb2.PadY))
	idx := offY*640 + 0
	if out[0*total+idx] != 50 || out[total+idx] != 100 || out[2*total+idx] != 200 {
		t.Errorf("图像区首行应为 BGR(50,100,200)，实得 (%v,%v,%v)",
			out[0*total+idx], out[total+idx], out[2*total+idx])
	}
}

// TestMapDetectionSizeInvariant 是 **minpx 坐标系不变性**的直接证明：
// 同一张源图坐标下的框，在任何输入尺寸下经 letterbox 往返后都精确回到原值
// （同人同框 → 同一个 minpx 判定），因为它由同一个 lb 的正/反向映射保证。
func TestMapDetectionSizeInvariant(t *testing.T) {
	const srcW, srcH = 1280, 791
	// 一个恰好落在 minpx 边界上的源图小脸（短边 = DefaultMinFacePx）
	want := Detection{
		X: 300, Y: 400, W: float64(DefaultMinFacePx), H: 32,
		Landmarks: [5][2]float64{{310, 405}, {330, 405}, {320, 415}, {312, 425}, {328, 425}},
		Score:     0.9,
	}
	for _, size := range []int{640, 1280, 1024} {
		lb := LetterboxFor(srcW, srcH, size)
		// 源图 → 画布（模拟检测器输出的画布坐标）：注意宽高也要一起变换
		canvas := want
		canvas.X, canvas.Y = lb.ToCanvas(want.X, want.Y)
		cx2, cy2 := lb.ToCanvas(want.X+want.W, want.Y+want.H)
		canvas.W, canvas.H = cx2-canvas.X, cy2-canvas.Y
		for i := range canvas.Landmarks {
			canvas.Landmarks[i][0], canvas.Landmarks[i][1] =
				lb.ToCanvas(canvas.Landmarks[i][0], canvas.Landmarks[i][1])
		}
		// 画布 → 源图（MapDetection）
		got := lb.MapDetection(canvas)
		if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 ||
			math.Abs(got.W-want.W) > 1e-9 || math.Abs(got.H-want.H) > 1e-9 {
			t.Errorf("size=%d 往返后 = (%.12f,%.12f,%.12f,%.12f)，期望 (%.12f,%.12f,%.12f,%.12f)",
				size, got.X, got.Y, got.W, got.H, want.X, want.Y, want.W, want.H)
		}
		for i := range want.Landmarks {
			if math.Abs(got.Landmarks[i][0]-want.Landmarks[i][0]) > 1e-9 ||
				math.Abs(got.Landmarks[i][1]-want.Landmarks[i][1]) > 1e-9 {
				t.Errorf("size=%d landmark %d 往返失配: %v，期望 %v", size, i, got.Landmarks[i], want.Landmarks[i])
			}
		}
		// minpx 判定的结论必须与尺寸无关。注意这里只能断言到**浮点精度**：
		// 源图 24.0px 的短边经不同 size 的 scale 往返后可能落在 23.99999999999994，
		// 即恰好贴着门限的脸在 1e-13 量级上可能翻转。这是任何重缩放固有的量化噪声
		// （旧的固定 640 路径同样有），不是坐标系漂移 —— 坐标系若真漂了，
		// 差异会是 12 vs 24 这种量级。
		if math.Abs(got.W-want.W) > 1e-9 {
			t.Errorf("size=%d 下短边偏离源图真值：|%v - %v| > 1e-9，说明 minpx 的坐标系发生了漂移",
				size, got.W, want.W)
		}
	}
}

// TestIdentityFastPathEqualsResampler 证明 scale==1 的免重采样快路径与
// 改造前的 CatmullRom 1:1 重采样**逐元素相同** —— 即该优化对小图
// （长边 ≤640，输入仍是 640）不改变任何一个送入网络的像素值。
func TestIdentityFastPathEqualsResampler(t *testing.T) {
	const W, H = 640, 396
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	for y := range H {
		for x := range W {
			img.Set(x, y, color.RGBA{
				R: uint8((x*7 + y) % 256), G: uint8((x + y*13) % 256), B: uint8((x * y) % 251), A: 255,
			})
		}
	}

	got, lb := LetterboxPixels(img, W) // 快路径
	if math.Abs(lb.Scale-1) > 1e-12 {
		t.Fatalf("scale = %v，本用例要求 1:1", lb.Scale)
	}

	// 复刻改造前的实现：先 CatmullRom 等比缩放到 newW×newH，再逐像素写画布
	newW, newH := W, H
	resized := image.NewRGBA(image.Rect(0, 0, newW, newH))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	want := make([]float32, 3*W*W)
	offX, offY := int(math.Round(lb.PadX)), int(math.Round(lb.PadY))
	total := W * W
	for y := 0; y < newH; y++ {
		for x := 0; x < newW; x++ {
			c := color.NRGBAModel.Convert(resized.At(x, y)).(color.NRGBA)
			idx := (offY+y)*W + (offX + x)
			want[0*total+idx] = float32(c.B)
			want[total+idx] = float32(c.G)
			want[2*total+idx] = float32(c.R)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 个元素与重采样路径不同：%v vs %v（快路径不是纯优化）", i, got[i], want[i])
		}
	}
}

// TestLetterboxPixelsIdentityFastPath 覆盖 scale==1 的免重采样快路径
// （自适应输入下 1280 宽 LG 正是这一情形），含子图（非零原点）取源像素的正确性。
func TestLetterboxPixelsIdentityFastPath(t *testing.T) {
	const W, H = 640, 396 // 长边 640 → 与 size 相等，scale=1

	mk := func(w, h, seed int) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.Set(x, y, color.RGBA{
					R: uint8((x + seed) % 256), G: uint8((y + seed) % 256), B: uint8((x * y % 251)), A: 255,
				})
			}
		}
		return img
	}

	cases := []struct {
		name string
		img  image.Image
		x0   int // 源图区域左上角在本图坐标系中的位置（= Bounds().Min）
		y0   int
	}{
		{"原点图", mk(W, H, 0), 0, 0},
		// 父图放大 8×8 再取子图，避免 SubImage 被父图边界裁剪（否则尺寸会小于 W×H）。
		{"子图（非零原点）", mk(W+8, H+8, 7).SubImage(image.Rect(3, 5, 3+W, 5+H)), 3, 5},
	}
	for _, c := range cases {
		if b := c.img.Bounds(); b.Dx() != W || b.Dy() != H {
			t.Fatalf("%s：用例图像尺寸 %dx%d，期望 %dx%d", c.name, b.Dx(), b.Dy(), W, H)
		}
		lb := LetterboxFor(W, H, W)
		if math.Abs(lb.Scale-1) > 1e-12 {
			t.Fatalf("%s：scale = %v，本用例要求 1:1", c.name, lb.Scale)
		}
		out, got := LetterboxPixels(c.img, W)
		total := W * W
		offY := int(math.Round(got.PadY))
		// 抽样核对若干像素（BGR 顺序）
		for _, p := range [][2]int{{0, 0}, {17, 33}, {W - 1, H - 1}, {100, 200}} {
			sx, sy := p[0]+c.x0, p[1]+c.y0
			want := color.NRGBAModel.Convert(c.img.At(sx, sy)).(color.NRGBA)
			idx := (offY+p[1])*W + p[0]
			if out[0*total+idx] != float32(want.B) ||
				out[total+idx] != float32(want.G) ||
				out[2*total+idx] != float32(want.R) {
				t.Errorf("%s：像素 %v 期望 BGR(%d,%d,%d)，实得 (%v,%v,%v)",
					c.name, p, want.B, want.G, want.R,
					out[0*total+idx], out[total+idx], out[2*total+idx])
			}
		}
		// 补边区必须保持 letterboxPad（0）
		if out[0*total+0] != 0 || out[total+0] != 0 || out[2*total+0] != 0 {
			t.Errorf("%s：顶部补边区应为 0，实得 (%v,%v,%v)", c.name, out[0], out[total], out[2*total])
		}
	}
}
