package faces

// Job000041 多尺度合并的纯函数守卫（不依赖 ORT，硬编码期望值穷举）。
// 真实召回/耗时数字由测试服 benchmark 产出（multiscale.go 头注释的表）。

import "testing"

func det(x, y, w, h, score float64) Detection {
	return Detection{X: x, Y: y, W: w, H: h, Score: score}
}

func TestMultiSizes(t *testing.T) {
	cases := []struct {
		name       string
		adaptive   int
		staticSize int
		multi      bool
		want       []int
	}{
		{"关→单尺度（与旧行为一致）", 1280, 0, false, []int{1280}},
		{"静态模型→单尺度（只有声明尺寸可用）", 640, 640, true, []int{640}},
		{"动态但小图≤640→单尺度", 640, 0, true, []int{640}},
		{"动态+大图+开→双尺度，自适应在前", 1280, 0, true, []int{1280, 640}},
		{"动态+4K 大图+开→[自适应,640]", 1920, 0, true, []int{1920, 640}},
	}
	for _, tc := range cases {
		got := multiSizes(tc.adaptive, tc.staticSize, tc.multi)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: want %v got %v", tc.name, tc.want, got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: want %v got %v", tc.name, tc.want, got)
			}
		}
	}
}

func TestIoU(t *testing.T) {
	if iou(det(0, 0, 10, 10, 0), det(100, 100, 10, 10, 0)) != 0 {
		t.Fatal("不相交应为 0")
	}
	if iou(det(0, 0, 10, 10, 0), det(0, 0, 10, 10, 0)) != 1 {
		t.Fatal("重合应为 1")
	}
	// 半重叠：inter=5*10=50，union=100+100-50=150 → 1/3
	if v := iou(det(0, 0, 10, 10, 0), det(5, 0, 10, 10, 0)); v < 0.3332 || v > 0.3334 {
		t.Fatalf("半重叠应≈1/3，实得 %f", v)
	}
	// 包含：inter=25（小框），union=100 → 0.25
	if v := iou(det(0, 0, 10, 10, 0), det(0, 0, 5, 5, 0)); v != 0.25 {
		t.Fatalf("包含应=小/大=0.25，实得 %f", v)
	}
}

func TestMergeScales(t *testing.T) {
	t.Run("同脸跨尺度去重，高 conf 胜出", func(t *testing.T) {
		big := det(100, 100, 40, 40, 0.95)   // 1280 尺度检出
		small := det(102, 99, 41, 41, 0.88) // 640 尺度检出（同脸，轻微几何差）
		got := mergeScales([][]Detection{{big}, {small}})
		if len(got) != 1 {
			t.Fatalf("同脸应合并为 1，实得 %d", len(got))
		}
		if got[0].Score != 0.95 {
			t.Fatalf("应保留高 conf 检出，实得 %f", got[0].Score)
		}
	})
	t.Run("不同脸全部保留", func(t *testing.T) {
		a := det(0, 0, 50, 50, 0.9)
		b := det(500, 500, 50, 50, 0.8)
		got := mergeScales([][]Detection{{a}, {b}})
		if len(got) != 2 {
			t.Fatalf("两脸应全保留，实得 %d", len(got))
		}
	})
	t.Run("阈值两侧：0.41 压制 / 0.39 保留", func(t *testing.T) {
		// 50×20 框水平错开 d：IoU = (50-d)*20 / (2000-(50-d)*20)。
		// d=14 → 36*20=720，union=1280 → 0.5625（压）；d=22 → 28*20=560/1440 ≈ 0.389（留）。
		pressed := mergeScales([][]Detection{{det(0, 0, 50, 20, 0.9)}, {det(14, 0, 50, 20, 0.85)}})
		if len(pressed) != 1 {
			t.Fatalf("IoU≈0.56 应压成 1，实得 %d", len(pressed))
		}
		keptBoth := mergeScales([][]Detection{{det(0, 0, 50, 20, 0.9)}, {det(22, 0, 50, 20, 0.85)}})
		if len(keptBoth) != 2 {
			t.Fatalf("IoU≈0.39 应保留 2，实得 %d", len(keptBoth))
		}
	})
	t.Run("单尺度过车=恒等（检测器内部已 NMS）", func(t *testing.T) {
		a := det(0, 0, 50, 50, 0.9)
		b := det(500, 500, 50, 50, 0.8)
		c := det(1000, 0, 50, 50, 0.7)
		got := mergeScales([][]Detection{{a, b, c}})
		if len(got) != 3 {
			t.Fatalf("单尺度应全保留，实得 %d", len(got))
		}
	})
	t.Run("空输入→nil", func(t *testing.T) {
		if got := mergeScales(nil); got != nil {
			t.Fatalf("nil 输入应 nil，实得 %v", got)
		}
		if got := mergeScales([][]Detection{{}, {}}); got != nil {
			t.Fatalf("空尺度应 nil，实得 %v", got)
		}
	})
	t.Run("conf 降序稳定（同 conf 小尺度在前）", func(t *testing.T) {
		a := det(0, 0, 10, 10, 0.9)   // scale 0
		b := det(500, 0, 10, 10, 0.9) // scale 1
		got := mergeScales([][]Detection{{a}, {b}})
		if len(got) != 2 || got[0].X != 0 {
			t.Fatalf("同 conf 应先 scale0 的框：%v", got)
		}
	})
	t.Run("小脸仅大尺度可见→保留（多尺度的全部意义）", func(t *testing.T) {
		bigFace := det(0, 0, 300, 300, 0.9)    // 两尺度都见
		tinyFace := det(800, 800, 18, 18, 0.87) // 仅 1280 尺度见（640 下 <10px）
		got := mergeScales([][]Detection{
			{bigFace, tinyFace}, // 1280
			{det(1, 1, 299, 299, 0.91)}, // 640：同一大脸
		})
		if len(got) != 2 {
			t.Fatalf("大脸合并 + 小脸保留 = 2，实得 %d", len(got))
		}
		foundTiny := false
		for _, g := range got {
			if g.W == 18 {
				foundTiny = true
			}
		}
		if !foundTiny {
			t.Fatal("小脸必须保留")
		}
	})
}

func TestParseBoolDefaultTrue(t *testing.T) {
	for _, v := range []string{"", "true", "1", "on", "TRUE", " yes"} {
		if !parseBoolDefaultTrue(v) {
			t.Fatalf("%q 应为 true", v)
		}
	}
	for _, v := range []string{"false", "0", "off", "NO", " n"} {
		if parseBoolDefaultTrue(v) {
			t.Fatalf("%q 应为 false", v)
		}
	}
}
