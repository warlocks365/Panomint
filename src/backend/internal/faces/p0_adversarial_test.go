package faces

// P0-3 faces.bbox 坐标归一的**对抗性验证**（独立于 match_test.go / scan_source_test.go）。
//
// 攻击面（修复不完整的样子）：
//   A. lgScale 算错（用成长边/高/硬编码 1）→ 原图扫描入库的 bbox 落在错误坐标系，
//      与存量 LG 框 IoU≈0 → 用户命名静默丢失；
//   B. Scaled 只缩框不缩五点（或反过来）→ 对齐/入库两套坐标混用；
//   C. MinFacePx 不做同系换算（或换算方向反了）→ 原图扫描时阈值语义漂移；
//   D. 存在第三条写 faces.bbox 的路径没走 Scaled。

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- A. lgScale 的语义 ----

func TestAdversarialLGScaleSemantics(t *testing.T) {
	cases := []struct {
		name string
		w, h int
		want float64
	}{
		{"4K 横图 3840x2160", 3840, 2160, 1280.0 / 3840.0},
		{"LG 本图 1280x853", 1280, 853, 1.0},
		{"小图 800x600（放大归一）", 800, 600, 1.6},
		{"竖图 3000x4000（按宽不按长边）", 3000, 4000, 1280.0 / 3000.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lgScale(image.NewRGBA(image.Rect(0, 0, tc.w, tc.h)))
			if math.Abs(got-tc.want) > 1e-12 {
				t.Fatalf("lgScale(%dx%d) = %v，want %v —— 缩放系数必须 = LGWidth/宽（等比、按宽）",
					tc.w, tc.h, got, tc.want)
			}
		})
	}
	// 退化：零宽图不得 panic、不得返回 0/NaN（返回 1 = 不缩放，fail-safe）。
	if got := lgScale(image.NewRGBA(image.Rect(0, 0, 0, 0))); got != 1 || math.IsNaN(got) {
		t.Fatalf("零宽图 lgScale = %v，必须 fail-safe 返回 1", got)
	}
	// 竖图若被错改为按长边，3000x4000 会得到 1280/4000 —— 反向自证上面断言能拦住：
	if wrong := 1280.0 / 4000.0; math.Abs(lgScale(image.NewRGBA(image.Rect(0, 0, 3000, 4000)))-wrong) < 1e-12 {
		t.Fatal("lgScale 用的是长边而不是宽：与 Detect 的 minPx 换算（b.Dx()）坐标系不一致")
	}
}

// TestAdversarialScanSourceScaleEndToEnd 真实 PNG 走 LoadScanImage：
// 原图 400 宽 → Scale=3.2；1280 宽缩略图 → Scale=1。防止 lgScale 接线断在 LoadScanImage 之外。
func TestAdversarialScanSourceScaleEndToEnd(t *testing.T) {
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	thumbDir := filepath.Join(root, "thumbs")
	writePNG(t, filepath.Join(mediaRoot, "2024/x.png"), 400, 300)
	writePNG(t, filepath.Join(thumbDir, "x_LG.webp.png"), 1280, 960)

	m := MediaItem{ID: "mx", Filename: "x.png", ThumbLG: "x_LG.webp.png", Path: "2024/x.png"}
	_, src, err := LoadScanImage(m, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("LoadScanImage: %v", err)
	}
	if src.Kind != SourceOriginal {
		t.Fatalf("应用原图，实际 %s", src.Kind)
	}
	if want := 1280.0 / 400.0; math.Abs(src.Scale-want) > 1e-12 {
		t.Fatalf("原图源 Scale = %v，want %v（= LGWidth/原图宽）", src.Scale, want)
	}

	m2 := MediaItem{ID: "mx2", Filename: "x.png", ThumbLG: "x_LG.webp.png", Path: "不存在.png"}
	_, src2, err := LoadScanImage(m2, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("LoadScanImage 回退: %v", err)
	}
	if src2.Kind != SourceThumb || src2.Scale != 1 {
		t.Fatalf("LG 缩略图源必须 kind=thumb 且 Scale=1，实际 kind=%s scale=%v", src2.Kind, src2.Scale)
	}
}

// ---- B. Scaled 必须框与五点一起缩放 ----

func TestAdversarialScaledCoversBoxAndLandmarks(t *testing.T) {
	d := Detection{
		X: 300, Y: 150, W: 120, H: 90,
		Landmarks: [5][2]float64{{330, 180}, {390, 180}, {360, 210}, {340, 240}, {380, 240}},
		Score:     0.95,
	}
	got := d.Scaled(1.0 / 3.0)
	want := Detection{
		X: 100, Y: 50, W: 40, H: 30,
		Landmarks: [5][2]float64{{110, 60}, {130, 60}, {120, 70}, {340.0 / 3, 80}, {380.0 / 3, 80}},
	}
	if math.Abs(got.X-want.X) > 1e-9 || math.Abs(got.Y-want.Y) > 1e-9 ||
		math.Abs(got.W-want.W) > 1e-9 || math.Abs(got.H-want.H) > 1e-9 {
		t.Fatalf("框缩放错误: got %+v want %+v", got, want)
	}
	for i := range want.Landmarks {
		if math.Abs(got.Landmarks[i][0]-want.Landmarks[i][0]) > 1e-9 ||
			math.Abs(got.Landmarks[i][1]-want.Landmarks[i][1]) > 1e-9 {
			t.Fatalf("第 %d 个 landmark 未随框缩放: got %v want %v —— "+
				"若只缩框不缩五点，入库框与对齐特征会落在两套坐标系", i, got.Landmarks[i], want.Landmarks[i])
		}
	}
	// 分数不属于几何量，不得被缩放。
	if got.Score != d.Score {
		t.Fatalf("Score 被缩放: %v → %v", d.Score, got.Score)
	}
	// 入参不得被修改（值语义）。
	if d.X != 300 || d.Landmarks[0][0] != 330 {
		t.Fatal("Scaled 修改了入参（应为纯函数）")
	}
}

// TestAdversarialCrossSourceSameFrame 核心不变式：同一张脸，3840 原图检出经 Scaled 归一后，
// 必须与 1280 LG 图直接检出的坐标**逐分量相等**（浮点误差内）。若归一因子错一丝，
// 命名迁移 IoU 就会系统性偏移。
func TestAdversarialCrossSourceSameFrame(t *testing.T) {
	onOriginal := Detection{X: 300, Y: 150, W: 120, H: 90}
	onLG := Detection{X: 100, Y: 50, W: 40, H: 30} // 同一张脸在 1280 宽 LG 上的坐标
	scaled := onOriginal.Scaled(1280.0 / 3840.0)
	for _, pair := range [][2]float64{{scaled.X, onLG.X}, {scaled.Y, onLG.Y}, {scaled.W, onLG.W}, {scaled.H, onLG.H}} {
		if math.Abs(pair[0]-pair[1]) > 1e-9 {
			t.Fatalf("跨图源归一后坐标不等: %v vs %v —— SM/LG 两源 bbox 不在同一坐标系", pair[0], pair[1])
		}
	}
}

// ---- C. MinFacePx 同系换算（detect.go 源码形状钉死） ----

// TestAdversarialMinFacePxConversion 原图扫描时 minpx 必须换算到图源坐标：
// minPx = MinFacePx × 图源宽 / LGWidth。缺换算 → 4K 原图上 24px 阈值实际只有 8px@LG
// （语义漂移）；方向反了（÷宽×LGWidth）→ 阈值放大 9 倍，合影小脸全灭。
func TestAdversarialMinFacePxConversion(t *testing.T) {
	b, err := os.ReadFile("detect.go")
	if err != nil {
		t.Fatalf("读不到 detect.go: %v", err)
	}
	src := string(b)
	const want = "float64(d.opts.MinFacePx) * float64(b.Dx()) / float64(LGWidth)"
	if !strings.Contains(src, want) {
		t.Fatalf("detect.go 里找不到 minPx 同系换算 %q —— MinFacePx 阈值在跨图源时语义漂移", want)
	}
	// 自证：三种坏版本（不换算 / 反向换算 / 用长边）都必须匹配不到 want。
	for _, broken := range []string{
		"minPx := float64(d.opts.MinFacePx)",
		"float64(d.opts.MinFacePx) / float64(b.Dx()) * float64(LGWidth)",
		"float64(d.opts.MinFacePx) * float64(srcLong) / float64(LGWidth)",
	} {
		if strings.Contains(broken, want) {
			t.Fatalf("守卫失效：坏版本 %q 也能通过断言", broken)
		}
	}
	// 换算基准必须与 lgScale 同轴（宽 b.Dx()），不能一个按宽一个按长边。
	if !strings.Contains(src, "b.Dx()") {
		t.Fatal("detect.go 的换算不基于图宽 b.Dx() —— 与 lgScale（按宽）坐标轴不一致")
	}
}

// ---- D. 全仓唯一写入路径守卫 ----

// TestAdversarialSingleBBoxWritePath 全仓扫描：写 faces.bbox 的 SQL 只允许有一处
// （internal/faces/store.go 的 SaveFace），且 SaveFace 的业务调用方只允许有
// cmd/facesgen/main.go 一处。新增写入路径而绕过 Scaled 归一时本测试即红。
func TestAdversarialSingleBBoxWritePath(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var insertSites, updateBBoxSites, saveFaceCallers []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") { // 测试自身含这些字面量，且不入生产路径
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		src := string(b)
		if strings.Contains(src, "INSERT INTO faces") {
			insertSites = append(insertSites, rel)
		}
		// UPDATE faces 不得触碰 bbox 列（person_id/is_pet 迁移允许）。
		for line := range strings.Lines(src) {
			if strings.Contains(line, "UPDATE faces") && strings.Contains(line, "bbox") {
				updateBBoxSites = append(updateBBoxSites, rel+": "+strings.TrimSpace(line))
			}
		}
		// SaveFace 调用点（排除定义行与测试不算 —— 测试也不许调，一并列出）。
		if strings.Contains(src, ".SaveFace(") && !strings.HasSuffix(rel, "store.go") {
			saveFaceCallers = append(saveFaceCallers, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(insertSites) != 1 || !strings.HasSuffix(insertSites[0], filepath.Join("internal", "faces", "store.go")) {
		t.Fatalf("INSERT INTO faces 必须只有 internal/faces/store.go 一处，实际: %v", insertSites)
	}
	if len(updateBBoxSites) != 0 {
		t.Fatalf("发现 UPDATE faces 直接改 bbox 的旁路: %v", updateBBoxSites)
	}
	if len(saveFaceCallers) != 1 || !strings.HasSuffix(saveFaceCallers[0], filepath.Join("cmd", "facesgen", "main.go")) {
		t.Fatalf("SaveFace 的业务调用方必须只有 cmd/facesgen/main.go 一处，实际: %v —— "+
			"新写入路径必须复用 scanOne 的 Scaled 归一", saveFaceCallers)
	}
}
