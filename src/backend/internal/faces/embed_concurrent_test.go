//go:build cgo

package faces

import (
	"math"
	"path/filepath"
	"sync"
	"testing"

	"panoalbum/internal/vecutil"
)

// testModelDir 定位仓库内人脸模型目录（assets/models/faces）。
func testModelDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "assets", "models", "faces"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestEmbedAlignedConcurrent 并发回归（P0-01）：共享输入张量必须在锁内写入。
// 两个 goroutine 分别提取不同像素的特征，断言各自结果与串行基线一致。
// 需要真实 ORT 运行时；不可用（如缺原生库/驱动）时跳过。
func TestEmbedAlignedConcurrent(t *testing.T) {
	rec, err := NewRecognizer(Options{ModelDir: testModelDir(t)})
	if err != nil {
		t.Skipf("ORT 不可用，跳过并发回归: %v", err)
	}
	defer rec.Close()

	mkPixels := func(v float32) []float32 {
		px := make([]float32, 3*FaceSize*FaceSize)
		for i := range px {
			px[i] = v + float32(i%256)/255.0
		}
		return px
	}
	inputs := [][]float32{mkPixels(10), mkPixels(200)}
	baseline := make([][]float32, len(inputs))
	for i, px := range inputs {
		v, err := rec.EmbedAligned(px)
		if err != nil {
			t.Fatalf("串行基线提取失败: %v", err)
		}
		baseline[i] = v
	}

	const rounds = 20
	for r := 0; r < rounds; r++ {
		var wg sync.WaitGroup
		got := make([][]float32, len(inputs))
		errs := make([]error, len(inputs))
		for i, px := range inputs {
			wg.Add(1)
			go func(i int, px []float32) {
				defer wg.Done()
				got[i], errs[i] = rec.EmbedAligned(px)
			}(i, px)
		}
		wg.Wait()
		for i := range inputs {
			if errs[i] != nil {
				t.Fatalf("并发提取失败: %v", errs[i])
			}
			if sim := vecutil.CosineSimilarity(got[i], baseline[i]); math.Abs(sim-1) > 1e-5 {
				t.Fatalf("第 %d 轮：输入 %d 并发结果与串行基线不一致（相似度 %.6f），疑似输入张量串台", r, i, sim)
			}
		}
	}
}
