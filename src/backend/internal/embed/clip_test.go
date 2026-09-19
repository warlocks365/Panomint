//go:build cgo

package embed

import (
	"context"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"panoalbum/internal/vecutil"
)

// libPath 定位本平台的原生库（assets/lib/onnxruntime-<platform>-*/lib/...）。
func libPath(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "assets", "lib"))
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "onnxruntime-*-*/lib", defaultLibName()))
	if len(matches) == 0 {
		nested, _ := filepath.Glob(filepath.Join(root, "onnxruntime-*", "lib", defaultLibName()))
		matches = append(matches, nested...)
	}
	if len(matches) == 0 {
		t.Skipf("未找到原生库 %s（目录 %s）", defaultLibName(), root)
	}
	return matches[0]
}

func newTestEncoder(t *testing.T) *Encoder {
	t.Helper()
	enc, err := NewEncoder(Config{ModelDir: modelDir(t), LibPath: libPath(t)})
	if err != nil {
		t.Fatalf("加载 CLIP 编码器失败: %v", err)
	}
	t.Cleanup(enc.Close)
	return enc
}

// synthImage 生成合成测试图（纯色块，避免依赖外部素材）。
func synthImage(c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := 0; y < 240; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestEncodeTextMechanics(t *testing.T) {
	enc := newTestEncoder(t)
	ctx := context.Background()

	v, err := enc.EncodeText(ctx, "sunset")
	if err != nil {
		t.Fatalf("EncodeText 失败: %v", err)
	}
	if len(v) != EmbeddingDim {
		t.Fatalf("维度应为 %d，实得 %d", EmbeddingDim, len(v))
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-4 {
		t.Errorf("L2 范数应≈1，实得 %.6f", math.Sqrt(norm))
	}
	t.Logf("text 'sunset' 前5维: %.4f %.4f %.4f %.4f %.4f", v[0], v[1], v[2], v[3], v[4])
}

func TestEncodeTextDistinguishesSemantics(t *testing.T) {
	enc := newTestEncoder(t)
	ctx := context.Background()

	a, err := enc.EncodeText(ctx, "a photo of a sunset over the sea")
	if err != nil {
		t.Fatal(err)
	}
	b, err := enc.EncodeText(ctx, "a photo of a car")
	if err != nil {
		t.Fatal(err)
	}
	sim := vecutil.CosineSimilarity(a, b)
	t.Logf("相似度(夕阳 vs 汽车) = %.4f", sim)
	if sim > 0.99 {
		t.Errorf("不同语义文本的相似度不应接近 1，实得 %.4f", sim)
	}
	// 自相似应为 1
	if s := vecutil.CosineSimilarity(a, a); math.Abs(s-1) > 1e-5 {
		t.Errorf("自相似应≈1，实得 %.6f", s)
	}
}

func TestEncodeImageMechanics(t *testing.T) {
	enc := newTestEncoder(t)
	ctx := context.Background()

	px := PreprocessImageData(synthImage(color.RGBA{R: 200, G: 60, B: 40, A: 255}))
	if len(px) != 3*ImageSize*ImageSize {
		t.Fatalf("预处理输出长度应为 %d，实得 %d", 3*ImageSize*ImageSize, len(px))
	}
	v, err := enc.EncodePixels(ctx, px)
	if err != nil {
		t.Fatalf("EncodePixels 失败: %v", err)
	}
	if len(v) != EmbeddingDim {
		t.Fatalf("维度应为 %d，实得 %d", EmbeddingDim, len(v))
	}
	var norm float64
	for _, x := range v {
		norm += float64(x) * float64(x)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-4 {
		t.Errorf("L2 范数应≈1，实得 %.6f", math.Sqrt(norm))
	}
}

// TestPreprocessResizesNonSquare 覆盖非正方形与极小图的缩放/裁剪路径。
func TestPreprocessNonSquare(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 800, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 800; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: 128, B: 64, A: 255})
		}
	}
	px := PreprocessImageData(img)
	if len(px) != 3*ImageSize*ImageSize {
		t.Fatalf("长度应为 %d，实得 %d", 3*ImageSize*ImageSize, len(px))
	}
}

// TestEncodeTextConcurrent 并发回归（P0-01）：共享输入张量必须在锁内写入。
// 两个 goroutine 分别编码不同文本，断言各自结果与串行基线一致（张冠李戴会立即暴露）。
// 需要真实 ORT 运行时；不可用（如缺原生库/驱动）时跳过。
func TestEncodeTextConcurrent(t *testing.T) {
	enc, err := NewEncoder(Config{ModelDir: modelDir(t), LibPath: libPath(t)})
	if err != nil {
		t.Skipf("ORT 不可用，跳过并发回归: %v", err)
	}
	defer enc.Close()
	ctx := context.Background()

	texts := []string{
		"a photo of a sunset over the sea",
		"a photo of a red car on a highway",
	}
	baseline := make([][]float32, len(texts))
	for i, s := range texts {
		v, err := enc.EncodeText(ctx, s)
		if err != nil {
			t.Fatalf("串行基线编码失败: %v", err)
		}
		baseline[i] = v
	}

	const rounds = 20
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		got := make([][]float32, len(texts))
		errs := make([]error, len(texts))
		for i, s := range texts {
			wg.Add(1)
			go func(i int, s string) {
				defer wg.Done()
				got[i], errs[i] = enc.EncodeText(ctx, s)
			}(i, s)
		}
		wg.Wait()
		for i := range texts {
			if errs[i] != nil {
				t.Fatalf("并发编码失败: %v", errs[i])
			}
			if sim := vecutil.CosineSimilarity(got[i], baseline[i]); math.Abs(sim-1) > 1e-5 {
				t.Fatalf("第 %d 轮：文本 %d 并发结果与串行基线不一致（相似度 %.6f），疑似输入张量串台", r, i, sim)
			}
		}
	}
}

func TestModelDirResolvable(t *testing.T) {
	dir := modelDir(t)
	for _, f := range []string{"tokenizer.json", "text_model_quantized.onnx", "vision_model_quantized.onnx"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("缺少模型文件 %s: %v", f, err)
		}
	}
}
