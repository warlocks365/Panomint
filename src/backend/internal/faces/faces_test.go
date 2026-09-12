package faces

// 纯逻辑单测（无 CGO 依赖，CGO_ENABLED=0 下亦可运行）：
// letterbox 坐标映射、YuNet 12 头解码与 NMS、输出头解析、五点相似变换与对齐、聚类判定。
//
// ⚠️ 这些用例只能覆盖**数学/几何**正确性；与 OpenCV 参考实现的逐位一致性
// （输出头命名与解码、对齐模板等价性、SFace 预处理通道序）必须在有 ORT 的
// Linux 环境上做交叉验证——本机 Windows 无法运行 ORT。

import (
	"image"
	"image/color"
	"math"
	"testing"
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
