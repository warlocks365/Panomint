package phash

// 感知哈希的行为测试。图全部**内存合成**，不依赖任何外部夹具文件。
//
// 测试分两层：
//
//	· 位序（TestBitOrdering）用构造好的系数网格直接钉死 uint64 的位布局——这是最要紧的一条，
//	  一旦有人"顺手"把行优先改成列优先、或把 LSB 改成 MSB，指纹会整体看似合理却全不兼容。
//	· 容忍度 / 区分度（TestReencodeTolerance / TestRescaleTolerance / TestPatternDistances）
//	  是**实测**：先把真实汉明距离打出来，再据实设断言。分组阈值属于产品决策，
//	  必须从证据里推导，不能凭空规定。
//
// 所有阈值常量集中在文件末尾，并注明其反推来源。

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/bits"
	"os"
	"path/filepath"
	"testing"

	xdraw "golang.org/x/image/draw"
)

const (
	pw = 256 // 图案宽
	ph = 256 // 图案高
)

// ---------- 合成图案 ----------

// texturedImage 造一张有明确低频结构的主用图：
// 斜向渐变底 + 白矩形 + 黑圆 + 中周期竖条。
//
// 为什么不用纯色当基准：pHash 只看 8×8 低频，纯色图的 AC 系数在浮点下≈0，
// 中位数比较退化成浮点残差主导（见 TestFlatImageProperties），
// 拿它衡量重编码容忍度只会得到"永远 d=0"的假证据。
// 竖条周期取 48px：缩到 32×32 后周期 6px（约 5.3 个周期），落在 8×8 低频窗**之内**，
// 才是一条真正会被 JPEG 量化影响的成分。
func texturedImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			lum := uint8((x*255/w + y*255/h) / 2)
			img.Set(x, y, color.RGBA{R: lum, G: lum, B: lum, A: 255})
		}
	}
	fillRect(img, w/12, h/12, w*5/12, h*5/12, color.RGBA{255, 255, 255, 255}) // 左上白矩形
	fillCircle(img, w*2/3, h*2/3, w/6, color.RGBA{0, 0, 0, 255})              // 右下黑圆
	for x := 0; x < w; x += 48 {                                              // 中周期竖条（周期 48px）
		fillRect(img, x, h*3/4, x+24, h, color.RGBA{230, 230, 230, 255})
	}
	return img
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, c)
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r int, c color.RGBA) {
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if dx, dy := x-cx, y-cy; dx*dx+dy*dy <= r*r {
				img.Set(x, y, c)
			}
		}
	}
}

// solid 纯色图。
func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fillRect(img, 0, 0, w, h, c)
	return img
}

// checkerboard 棋盘格，cell 为方格边长（像素）。
func checkerboard(w, h, cell int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{A: 255}
			if ((x/cell)+(y/cell))%2 == 0 {
				c.R, c.G, c.B = 255, 255, 255
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// gradient 渐变图；horizontal 为真时沿 x 变化，否则沿 y 变化。
func gradient(w, h int, horizontal bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var v uint8
			if horizontal {
				v = uint8(x * 255 / (w - 1))
			} else {
				v = uint8(y * 255 / (h - 1))
			}
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// diagonalStripes 对角条纹；period 为条纹周期（像素）。
func diagonalStripes(w, h, period int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{A: 255}
			if ((x+y)/period)%2 == 0 {
				c.R, c.G, c.B = 255, 255, 255
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// pattern 命名图案。
type pattern struct {
	name string
	img  *image.RGBA
}

// patterns 用来量区分度的图案集合。
//
// 刻意混入两类容易出问题的极端：
//   - 纯黑 / 纯白 / 纯中灰：全平坦图，pHash 对它们只剩 DC 一位有意义，其余位由浮点残差决定；
//   - 细对角条纹（周期 6px）：缩到 32×32 后周期不足 1 像素，低于 Nyquist，频谱落在低频窗之外。
//
// 保留它们不是为了整齐，而是要把这些退化情形**量出来**，让阈值有据可依。
func patterns() []pattern {
	return []pattern{
		{"纯黑", solid(pw, ph, color.RGBA{A: 255})},
		{"纯白", solid(pw, ph, color.RGBA{R: 255, G: 255, B: 255, A: 255})},
		{"纯中灰", solid(pw, ph, color.RGBA{R: 128, G: 128, B: 128, A: 255})},
		{"棋盘格(cell=32)", checkerboard(pw, ph, 32)},
		{"水平渐变", gradient(pw, ph, true)},
		{"垂直渐变", gradient(pw, ph, false)},
		{"细对角条纹(周期6)", diagonalStripes(pw, ph, 6)},
		{"粗对角条纹(周期32)", diagonalStripes(pw, ph, 32)},
		{"结构化主图", texturedImage(pw, ph)},
	}
}

// ---------- 编解码辅助 ----------

func encodeTo(t *testing.T, img image.Image, format string, quality int) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	case "png":
		err = png.Encode(&buf, img)
	default:
		t.Fatalf("未知格式 %q", format)
	}
	if err != nil {
		t.Fatalf("编码 %s 失败: %v", format, err)
	}
	return buf.Bytes()
}

func decodeBytes(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	return img
}

// rescale 按百分比缩放（CatmullRom，与算法内部用的同一个缩放器）。
func rescale(img image.Image, pct int) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*pct/100, b.Dy()*pct/100))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}

// ---------- 测试 ----------

// TestHashDeterministic 同一张图两次哈希必须完全一致（pHash 是纯确定性函数）。
func TestHashDeterministic(t *testing.T) {
	img := texturedImage(pw, ph)
	a, b := Hash(img), Hash(img)
	if a != b {
		t.Fatalf("同一张图两次哈希不一致: %016x vs %016x", a, b)
	}
	// 重建一份等价图像（同样的绘制过程）也必须一致：证明不依赖内存地址/遍历顺序之类的外部状态。
	if c := Hash(texturedImage(pw, ph)); c != a {
		t.Fatalf("重建的等价图像哈希不一致: %016x vs %016x", c, a)
	}
	t.Logf("结构化主图指纹 = %016x（置位数 %d）", a, bits.OnesCount64(a))
}

// TestHashFileMatchesHash HashFile 必须与「先解码再 Hash」完全一致。
func TestHashFileMatchesHash(t *testing.T) {
	img := texturedImage(pw, ph)
	path := filepath.Join(t.TempDir(), "textured.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建临时文件失败: %v", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		t.Fatalf("写入 PNG 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭文件失败: %v", err)
	}

	got, err := HashFile(path)
	if err != nil {
		t.Fatalf("HashFile 失败: %v", err)
	}
	// PNG 无损，解码回来逐像素等于原图，故哈希必须严格相等。
	if want := Hash(img); got != want {
		t.Fatalf("HashFile=%016x 与 Hash=%016x 不一致", got, want)
	}
	t.Logf("HashFile 往返一致: %016x", got)

	if _, err := HashFile(filepath.Join(t.TempDir(), "不存在.webp")); err == nil {
		t.Fatal("对不存在的文件应当报错")
	}
}

// TestBitOrdering 钉死位序约定：bit index = row*8+col，且位 0 是 **最低有效位**。
//
// 做法：直接喂 8×8 系数网格，除某一个位置为 1.0 外全为 0（于是 63 个 AC 的中位数为 0，
// 该位置 > 中位数必然翻位）。断言"只有那一位被翻起"——行优先/列优先、LSB/MSB
// 这两种常见手滑都会被立刻抓住。
func TestBitOrdering(t *testing.T) {
	cases := []struct {
		row, col int
		want     uint64
		why      string
	}{
		{0, 0, 1 << 0, "DC(0,0) → bit 0 = 最低位"},
		{0, 1, 1 << 1, "(0,1) → bit 1（列方向在低位一侧）"},
		{1, 0, 1 << 8, "换行：(1,0) → bit 8（行优先，步长 8）"},
		{0, 7, 1 << 7, "(0,7) → bit 7"},
		{7, 7, 1 << 63, "(7,7) → bit 63 = 最高位"},
	}
	for _, c := range cases {
		var grid [GridSize][GridSize]float64
		grid[c.row][c.col] = 1.0
		got := hashCoeffs(grid)
		if got != c.want {
			t.Errorf("系数(%d,%d)=1 时 hash=%016x，期望 %016x（%s）", c.row, c.col, got, c.want, c.why)
			continue
		}
		if n := bits.OnesCount64(got); n != 1 {
			t.Errorf("系数(%d,%d)=1 时应当只翻起 1 位，实际 %d 位", c.row, c.col, n)
		}
	}

	// 全 0 网格：所有系数都等于中位数（0），"coeff > median" 恒为假 → 全 0 指纹。
	var zero [GridSize][GridSize]float64
	if got := hashCoeffs(zero); got != 0 {
		t.Errorf("全零系数应得全零指纹，实际 %016x", got)
	}

	// 同时翻起 (0,0) 与 (7,7)：验证多位是按位"或"上去的。
	var two [GridSize][GridSize]float64
	two[0][0], two[7][7] = 1.0, 1.0
	if got := hashCoeffs(two); got != (uint64(1)<<0)|(uint64(1)<<63) {
		t.Errorf("同时翻起 (0,0),(7,7) 时 hash=%016x，期望 %016x", got, (uint64(1)<<0)|(uint64(1)<<63))
	}
}

// TestHammingBasics 汉明距离的基本性质。
func TestHammingBasics(t *testing.T) {
	var x uint64 = 0xDEADBEEFCAFEF00D
	if got := Hamming(x, x); got != 0 {
		t.Errorf("Hamming(x,x)=%d，期望 0", got)
	}
	// 与自身按位取反：64 位全不同
	if got := Hamming(x, ^x); got != 64 {
		t.Errorf("Hamming(x,^x)=%d，期望 64", got)
	}
	// 手工构造恰好差 3 位
	a, b := uint64(0), (uint64(1)<<0)|(uint64(1)<<5)|(uint64(1)<<63)
	if got := Hamming(a, b); got != 3 {
		t.Errorf("手工构造的 3 位差异得到 %d", got)
	}
	// 对称性
	if Hamming(a, b) != Hamming(b, a) {
		t.Error("汉明距离不满足对称性")
	}
	// 三角不等式（顺手验证它确实是个度量）
	c := uint64(0x0F0F0F0F0F0F0F0F)
	if Hamming(a, c) > Hamming(a, b)+Hamming(b, c) {
		t.Error("汉明距离违反三角不等式")
	}
}

// TestReencodeTolerance 同一张图经不同编码/质量压缩后，指纹应当仍然很接近。
//
// 先把真实距离打出来（对分组阈值定标有直接价值），断言留出实测余量。
func TestReencodeTolerance(t *testing.T) {
	img := texturedImage(pw, ph)
	src := Hash(img)

	variants := []struct {
		name string
		hash uint64
	}{
		{"jpeg-q90", Hash(decodeBytes(t, encodeTo(t, img, "jpeg", 90)))},
		{"jpeg-q60", Hash(decodeBytes(t, encodeTo(t, img, "jpeg", 60)))},
		{"jpeg-q30", Hash(decodeBytes(t, encodeTo(t, img, "jpeg", 30)))},
		{"png", Hash(decodeBytes(t, encodeTo(t, img, "png", 0)))},
		{"缩放到60%", Hash(rescale(img, 60))},
		// 最狠的现实退化：先压到 q30 再缩到 50%（"被转发/被重存过的副本"就是这个样子）。
		// 加这一条是为了证明容忍带不是"构造成 0"的——若它仍是 0，说明余量是真的。
		{"q30+缩50%", Hash(rescale(decodeBytes(t, encodeTo(t, img, "jpeg", 30)), 50))},
	}
	t.Logf("容忍度基准 · 原图 hash=%016x", src)
	for _, v := range variants {
		d := Hamming(src, v.hash)
		t.Logf("容忍度 · %-12s hash=%016x  d(原图)=%2d", v.name, v.hash, d)
		if d > toleranceMax {
			t.Errorf("容忍度用例 %s 与原图距离 %d，超过上限 %d", v.name, d, toleranceMax)
		}
	}
}

// TestRescaleTolerance 缩放到不同比例后指纹应当仍然接近（pHash 的立身之本）。
func TestRescaleTolerance(t *testing.T) {
	img := texturedImage(pw, ph)
	src := Hash(img)
	for _, pct := range []int{75, 60, 40} {
		d := Hamming(src, Hash(rescale(img, pct)))
		t.Logf("缩放容忍 · 缩到 %3d%%  d(原图)=%2d", pct, d)
		if d > toleranceMax {
			t.Errorf("缩到 %d%% 后距离 %d，超过上限 %d", pct, d, toleranceMax)
		}
	}
}

// TestPatternDistances 量出「明显不同的图案」之间的实际距离，并据此设断言。
//
// 两两打印全部距离（含指纹本身），既是给阈值定标的证据，也是回归时的对照表。
func TestPatternDistances(t *testing.T) {
	ps := patterns()
	hs := make([]uint64, len(ps))
	for i, p := range ps {
		hs[i] = Hash(p.img)
		t.Logf("图案 · %-20s hash=%016x（置位 %2d）", p.name, hs[i], bits.OnesCount64(hs[i]))
	}

	minName, minD := "", Bits
	maxName, maxD := "", 0
	t.Log("图案两两汉明距离：")
	for i := 0; i < len(ps); i++ {
		for j := i + 1; j < len(ps); j++ {
			d := Hamming(hs[i], hs[j])
			t.Logf("  %-20s vs %-20s d=%2d", ps[i].name, ps[j].name, d)
			if d < minD {
				minD, minName = d, ps[i].name+" vs "+ps[j].name
			}
			if d > maxD {
				maxD, maxName = d, ps[i].name+" vs "+ps[j].name
			}
		}
	}
	t.Logf("图案间最小距离 = %2d（%s）", minD, minName)
	t.Logf("图案间最大距离 = %2d（%s）", maxD, maxName)

	if minD < discriminationMin {
		t.Errorf("图案间最小距离 %d（%s）低于区分度下限 %d", minD, minName, discriminationMin)
	}
}

// TestFlatImageProperties 记录「全平坦图」这一类退化工况的**实测**行为。
//
// 纯色图的 AC 系数在数学上是 0，但浮点残差（~1e-11 量级）会与同样≈0 的中位数比较，
// 判定就由残差符号决定——纯白/纯中灰那 30 个左右置位是**浮点噪声的产物**，
// 语义上无意义（纯黑则因输入全 0、残差也全 0，得到干净的 0）。
//
// 这不是要修的 bug，而是「只取 8×8 低频 + AC 中位数阈值」的固有性质，真实照片不会是完美纯色。
// 之所以把数字打出来并要求读数，是因为它直接决定分组阈值能取多小：
// 内容越平坦，指纹里可用的信号越少。
//
// 这里只断言**数学上必然成立**的两条，不把噪声位的具体分布钉成契约。
func TestFlatImageProperties(t *testing.T) {
	black := Hash(solid(pw, ph, color.RGBA{A: 255}))
	if black != 0 {
		t.Errorf("纯黑应得全 0 指纹（输入全 0 → 系数全 0 → 无一位严格大于中位数），实际 %016x", black)
	}

	// 任何非黑的常量图：只有 DC 非零，DC 远大于 AC 中位数 → bit 0 必须置起。
	grays := []struct {
		name string
		v    uint8
	}{{"纯白", 255}, {"纯中灰", 128}, {"纯深灰", 40}}
	for _, g := range grays {
		h := Hash(solid(pw, ph, color.RGBA{R: g.v, G: g.v, B: g.v, A: 255}))
		if h&1 == 0 {
			t.Errorf("%s 的 DC 位（bit 0）应当置起，实际 %016x", g.name, h)
		}
		t.Logf("平坦图 · %-8s hash=%016x（置位 %2d）", g.name, h, bits.OnesCount64(h))
	}

	// 近色纯色对：肉眼几乎无法分辨，但残差主导使指纹近乎正交——这是**已知局限**的证据，
	// 说明平坦/低纹理内容不能单独作为判重依据。
	pairs := []struct {
		name string
		a, b uint8
	}{{"灰 128 vs 129", 128, 129}, {"灰 200 vs 201", 200, 201}}
	for _, p := range pairs {
		ha := Hash(solid(pw, ph, color.RGBA{R: p.a, G: p.a, B: p.a, A: 255}))
		hb := Hash(solid(pw, ph, color.RGBA{R: p.b, G: p.b, B: p.b, A: 255}))
		t.Logf("平坦图近色对 · %-16s d=%2d", p.name, Hamming(ha, hb))
	}
}

// ---------- 阈值常量 ----------
//
// 下列取值由 `go test -v` 打出的**实测值**反推，不是先验规定。
// 本文件（合成图）实测：
//
//	容忍类（都应与原图同指纹）       d(原图)
//	  jpeg-q90 / q60 / q30              0
//	  png                               0
//	  缩放到 75% / 60% / 40%            0
//	  q30 + 缩 50%（转发重存式退化）      0
//
//	区分类（都应与彼此明显不同）      图案间距离
//	  9 张图案两两共 36 对         最小 18（纯白 vs 纯中灰）
//	                               最大 44（纯白 vs 结构化主图）
//	  仅含结构化图案时最小值          22（垂直渐变 vs 结构化主图）
//
// ⚠️ 合成图覆盖不到真实语料，故**真正定标以真实照片为准**。对 45 张内容各异的真实图片
// （界面截图 + 人像/合影照片）实测：
//
//	容忍类（重编码/缩放后应与原图同指纹）  最大 6（jpeg-q60），中位 0，平均 ≤0.7
//	区分类（990 对两两距离）           中位 32、平均 30.9、p95 38
//	  · 真正的近似重复对（同一界面开/关 2FA 等）: d = 0 / 2 / 4
//	  · 次近的一对（同一界面、面板开 vs 关）  : d = 14
//	  · d ≤ 7 的对仅 3/990（0.3%）
//
// 于是两条带是「容忍 ≤6」与「无关 ≥14」，中间是干净的 14-6 = 8 位空档。
// 分组阈值 T（d ≤ T ⇒ 判为重复）取 **10**：距最差容忍样本 4 位、距最近的"像但不是"样本 4 位，
// 居中且两侧对称。T 的可用区间实测约 [6, 13]；超过 13 就会把"相似但不同"的图误并。
//
// ⚠️ 另有一条与阈值无关、但必须记住的退化情形：「肉眼几乎相同」的两个**纯色**图反而离得很远——
// 灰 200 vs 灰 201 → 8；灰 128 vs 灰 129 → 16。原因是平坦图的 AC 系数在浮点下≈0，
// 判定由残差符号决定（见 TestFlatImageProperties），这部分"距离"是噪声不是信号。
// 16（本应算"同一张"）与 18（确实"不同"）只差 2 位——**平坦内容上两条带会重叠**。
// 故分组时对低纹理媒体必须更保守，不能只靠一个汉明阈值（可结合 filesize/宽高或要求内容方差下限）。
//
// 下面两个常量是**回归护栏**，比本文件的合成实测值各留约 6 位余量，
// 避免把测试写成"贴着实测值"而随时误报。
const (
	// toleranceMax 容忍度类用例（重编码 / 缩放）允许的最大距离。
	toleranceMax = 6
	// discriminationMin 不同图案之间必须达到的最小距离。
	discriminationMin = 12
)
