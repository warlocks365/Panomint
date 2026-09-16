// Package phash 感知哈希（pHash）：把一张缩略图压成 64 位指纹，用于「同一张图的不同副本」去重。
//
// **为什么不复用 media.hash**：media.hash 自迁移 00004 起存的就是 **sha256 内容哈希**
// （internal/index/scanner.go 写入），并且是既有导入去重链路的承重墙——index.go 与
// upload.go 都以「sha256 相同 → 视为同一文件、跳过导入」为判据。pHash 的语义完全不同：
// 它要认出**字节不同、观感相同**的图（重新编码、缩放、加滤镜），两者判据不可互换。
// 故 pHash 单独占 media.phash 一列（BIGINT 装 64 位，SQL 侧用
// `bit_count((a # b)::bit(64))` 算汉明距离——bit_count 只吃 bit 类型，必须显式转位串）。
//
// 算法（DCT pHash）：
//
//  1. 缩到 32×32（CatmullRom，与 internal/embed 的缩放器一致）
//  2. 32×32 块上做二维 DCT-II，取左上 8×8 低频系数
//  3. 阈值 = 63 个 **AC** 系数的中位数（**排除 DC**：DC 是整幅平均亮度，
//     量级远大于其余系数，若把它算进中位数会把阈值整体抬高，结果是全图只翻出 DC 一位）
//  4. 按行优先输出 64 位：bit = coeff > median（**DC 参与比较**，只是不参与求中位数）
//
// 位序约定（phash_test.go 会钉死，勿改）：
//
//	位索引 = row*8+col，且位 0 是 uint64 的**最低位**（LSB）；即 `h |= 1 << (row*8+col)`。
//	于是系数 (0,0)=DC → bit 0，(0,7) → bit 7，(1,0) → bit 8，(7,7) → bit 63。
//
// 依赖：只用标准库 image + golang.org/x/image（WebP 解码注册）。本项目对第三方依赖保守，
// 故不引入任何现成 pHash 库；也不复用 internal/embed 的解码函数——pHash 只要亮度，
// 不该让哈希工具反向依赖向量包。
package phash

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/bits"
	"os"
	"sort"

	xdraw "golang.org/x/image/draw"
	// 缩略图由处理管线输出为 WebP（thumbnail_sm/md/lg 均为 <id>_<SIZE>.webp），
	// 标准库 image 不解码 WebP —— 缺此注册会全线报 "image: unknown format"。
	_ "golang.org/x/image/webp"
)

const (
	// InputSize 送入 DCT 的方块边长。
	InputSize = 32
	// GridSize 取用的低频系数边长（8×8 恰好 64 位，与 BIGINT 对齐）。
	GridSize = 8
	// Bits 指纹位数。
	Bits = GridSize * GridSize
)

// Hash 计算图像感知哈希。
func Hash(img image.Image) uint64 {
	return hashCoeffs(dctLowFreq(luma32(img)))
}

// Hamming 两个指纹的汉明距离（不同位数）。
//
// 这是**唯一**用于判定「两张图像不像」的度量：分组阈值就是在这个坐标系里表达的
// （阈值越小越严：0 = 完全一致，64 = 每一位都相反）。
func Hamming(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// HashFile 解码图片文件后计算感知哈希。
func HashFile(path string) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("打开图片失败: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return 0, fmt.Errorf("解码图片失败: %w", err)
	}
	return Hash(img), nil
}

// luma32 缩放到 32×32 并取亮度。
//
// 先缩放后转灰度与「先灰度后缩放」在数学上等价：亮度 Y 是 R/G/B 的**线性**组合，
// 而缩放是线性加权平均，二者可交换（差别仅在定点舍入）。故合并成一趟，少一次全图遍历。
func luma32(img image.Image) [InputSize][InputSize]float64 {
	dst := image.NewRGBA(image.Rect(0, 0, InputSize, InputSize))
	// CatmullRom 近似 PIL 的 BICUBIC，与 internal/embed 的预处理保持一致。
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), xdraw.Over, nil)

	var out [InputSize][InputSize]float64
	for y := 0; y < InputSize; y++ {
		for x := 0; x < InputSize; x++ {
			// 缩略图恒为不透明，GrayModel 走的是 ITU-R 601 亮度（299/587/114）。
			out[y][x] = float64(color.GrayModel.Convert(dst.At(x, y)).(color.Gray).Y)
		}
	}
	return out
}

// dctLowFreq 对 32×32 亮度块做二维 DCT-II，返回左上 8×8 低频系数。
//
// 用**正交归一化**形式（系数带 c(u)c(v)，c(0)=1/√2、c(k)=1，整体乘 2/N）：
// 归一化对「系数 > 中位数」这种比较本身无影响（全局缩放可约），但 c(0) 与 c(k) 相差 √2，
// 会改变 DC 相对 AC 的量级——即影响 bit 0 的判定。这里取正交归一化，
// 是教科书 DCT-II 的标准形式，避免归一化方式成为隐式约定。
//
// 二维 DCT 可分离，故先对每行做 1D 变换（只算前 8 个 u），再对列做 1D 变换（只算前 8 个 v），
// 复杂度从 8×8×32×32 降到 32×32×8 + 8×32×8，高频系数本来就要丢弃，不必算。
func dctLowFreq(f [InputSize][InputSize]float64) [GridSize][GridSize]float64 {
	// 预计算 cos((2x+1)uπ/2N)：u∈[0,8) 是输出频率，x∈[0,32) 是输入采样。
	var cosTab [GridSize][InputSize]float64
	for u := 0; u < GridSize; u++ {
		for x := 0; x < InputSize; x++ {
			cosTab[u][x] = math.Cos(float64(2*x+1) * float64(u) * math.Pi / (2 * InputSize))
		}
	}

	// 行变换：rows[y][u] = Σ_x f[y][x]·cos(…u…)
	var rows [InputSize][GridSize]float64
	for y := 0; y < InputSize; y++ {
		for x := 0; x < InputSize; x++ {
			v := f[y][x]
			for u := 0; u < GridSize; u++ {
				rows[y][u] += v * cosTab[u][x]
			}
		}
	}

	// 列变换 + 归一化。
	var out [GridSize][GridSize]float64
	const scale = 2.0 / InputSize
	for u := 0; u < GridSize; u++ {
		cu := 1.0
		if u == 0 {
			cu = 1 / math.Sqrt2
		}
		for v := 0; v < GridSize; v++ {
			cv := 1.0
			if v == 0 {
				cv = 1 / math.Sqrt2
			}
			var sum float64
			for y := 0; y < InputSize; y++ {
				sum += rows[y][u] * cosTab[v][y]
			}
			out[u][v] = scale * cu * cv * sum
		}
	}
	return out
}

// hashCoeffs 以 63 个 AC 系数的中位数为阈值，把 8×8 系数网格压成 64 位。
// 位序见包注释：bit index = row*8+col，位 0 为最低位。
func hashCoeffs(c [GridSize][GridSize]float64) uint64 {
	// 63 个 AC 系数（跳过 DC (0,0)）——奇数个，中位数即排序后正中那个，无需取平均。
	var ac [Bits - 1]float64
	n := 0
	for u := 0; u < GridSize; u++ {
		for v := 0; v < GridSize; v++ {
			if u == 0 && v == 0 {
				continue
			}
			ac[n] = c[u][v]
			n++
		}
	}
	sort.Float64s(ac[:])
	median := ac[len(ac)/2]

	var h uint64
	for u := 0; u < GridSize; u++ {
		for v := 0; v < GridSize; v++ {
			if c[u][v] > median { // DC 参与比较，只是不参与求中位数
				h |= 1 << uint(u*GridSize+v)
			}
		}
	}
	return h
}
