package faces

// 重扫幂等所依赖的「人脸身份迁移」单测（纯几何，无 CGO / 无 DB，CGO_ENABLED=0 下亦可运行）：
// 覆盖 BoxIoU 的边界与 BestFaceMatch 的选取/阈值/命名迁移行为。
//
// 这些用例直接对应缺陷修复的两个不变量：
//  1. 同一张脸重扫时框漂移 < 半框 ⇒ 必须匹配上（否则用户的命名会被无谓丢掉）；
//  2. 同一张照片里相距较远的另一张脸 ⇒ 必须匹配不上（否则会把命名张冠李戴）。

import (
	"math"
	"testing"
)

func TestBoxIoU(t *testing.T) {
	cases := []struct {
		name string
		a, b FaceRef
		want float64
	}{
		{"完全相同", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 0, Y: 0, W: 10, H: 10}, 1},
		{"完全不重叠", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 10, Y: 0, W: 10, H: 10}, 0},
		{"仅小幅擦边", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 9, Y: 0, W: 10, H: 10}, 10.0 / 190.0},
		{"半宽重叠", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 0, Y: 0, W: 5, H: 10}, 0.5},
		{"错开半框", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 5, Y: 0, W: 10, H: 10}, 1.0 / 3.0},
		{"错开十分之一框", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 1, Y: 0, W: 10, H: 10}, 90.0 / 110.0},
		// bbox IS NULL 时 DB 侧用 COALESCE 退化成全 0 的零面积框 —— 必须恒为 0
		{"零面积占位框", FaceRef{X: 0, Y: 0, W: 10, H: 10}, FaceRef{X: 0, Y: 0, W: 0, H: 0}, 0},
	}
	for _, c := range cases {
		if got := BoxIoU(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: BoxIoU = %v，期望 %v", c.name, got, c.want)
		}
		if got := BoxIoU(c.b, c.a); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: BoxIoU 不满足对称性，反序 = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestBestFaceMatchPicksHighestIoU(t *testing.T) {
	olds := []FaceRef{
		{PersonID: "far", X: 400, Y: 100, W: 60, H: 60, ClusterID: "c-far"},
		{PersonID: "near", X: 100, Y: 100, W: 60, H: 60, ClusterID: "c-near"},
	}
	// 与 near 错开 6~8px（同一张脸重扫的典型漂移），与 far 相距 300px
	got, ok := BestFaceMatch(olds, Detection{X: 106, Y: 104, W: 60, H: 60})
	if !ok {
		t.Fatalf("期望命中 near，实得未命中")
	}
	if got.PersonID != "near" || got.ClusterID != "c-near" {
		t.Fatalf("命中 %q（簇 %q），期望 near / c-near", got.PersonID, got.ClusterID)
	}
}

func TestBestFaceMatchRejectsDifferentFaceInSameMedia(t *testing.T) {
	// 同一张合影里两张相距较远的脸：新检出只能匹配到自己的那张
	olds := []FaceRef{
		{PersonID: "left", X: 100, Y: 100, W: 60, H: 60},
		{PersonID: "right", X: 400, Y: 100, W: 60, H: 60},
	}
	if _, ok := BestFaceMatch(olds, Detection{X: 400, Y: 100, W: 60, H: 60}); !ok {
		t.Fatalf("右脸应匹配上 right")
	}
	// 左脸位置的人换成了另一张脸（与右脸位置重合）时，不得把 right 的命名搬到左侧
	got, ok := BestFaceMatch([]FaceRef{{PersonID: "right", X: 400, Y: 100, W: 60, H: 60}}, Detection{X: 100, Y: 100, W: 60, H: 60})
	if ok {
		t.Fatalf("相距 300px 不应匹配，实得命中 %q", got.PersonID)
	}
}

func TestBestFaceMatchThresholdBoundary(t *testing.T) {
	// IoU 恰为 MatchMinIoU(0.5) 视为「同一张脸」（阈值是合格下界）
	if MatchMinIoU != 0.5 {
		t.Fatalf("MatchMinIoU 变为 %v，本用例需同步更新", MatchMinIoU)
	}
	olds := []FaceRef{{PersonID: "p", X: 0, Y: 0, W: 10, H: 10}}
	if _, ok := BestFaceMatch(olds, Detection{X: 0, Y: 0, W: 5, H: 10}); !ok {
		t.Errorf("IoU=0.5 应判为同一张脸")
	}
	if _, ok := BestFaceMatch(olds, Detection{X: 5, Y: 0, W: 10, H: 10}); ok {
		t.Errorf("IoU=1/3 应判为不同人脸")
	}
}

func TestBestFaceMatchZeroAreaNeverStealsIdentity(t *testing.T) {
	// DB 里若存在 bbox IS NULL 的行（退化为零面积框），绝不能吞掉任何新检出的命名
	olds := []FaceRef{{PersonID: "ghost", IsPet: true, X: 0, Y: 0, W: 0, H: 0, ClusterID: "c-ghost"}}
	if got, ok := BestFaceMatch(olds, Detection{X: 100, Y: 100, W: 60, H: 60}); ok {
		t.Fatalf("零面积旧脸不应匹配，实得命中 %q", got.PersonID)
	}
}

func TestBestFaceMatchEmptyAndUnnamed(t *testing.T) {
	if _, ok := BestFaceMatch(nil, Detection{X: 0, Y: 0, W: 10, H: 10}); ok {
		t.Fatalf("无旧脸时不应命中")
	}
	// 命中未命名的旧脸：ok 为真但 PersonID 为空 —— 调用方据此不迁移命名
	got, ok := BestFaceMatch([]FaceRef{{X: 0, Y: 0, W: 10, H: 10, ClusterID: "c-old"}}, Detection{X: 1, Y: 0, W: 10, H: 10})
	if !ok {
		t.Fatalf("应命中未命名的旧脸")
	}
	if got.PersonID != "" {
		t.Fatalf("旧脸未命名，PersonID 应为空串，实得 %q", got.PersonID)
	}
	if got.ClusterID != "c-old" {
		t.Fatalf("应保留旧簇 ID，实得 %q", got.ClusterID)
	}
}
