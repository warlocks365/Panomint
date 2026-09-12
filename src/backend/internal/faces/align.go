package faces

// 五点对齐：把人脸按 landmark 做相似变换（旋转 + 等比缩放 + 平移，**无反光/无仿射拉伸**）
// 并裁剪为 112×112 的 SFace 输入张量。
//
// 模板坐标取自 OpenCV FaceRecognizerSFImpl::getSimilarityTransformMatrix 的 dst：
//
//	{38.2946, 51.6963}, {73.5318, 51.5014}, {56.0252, 71.7366}, {41.5493, 92.3655}, {70.7299, 92.2041}
//
// 顺序与 YuNet 输出一致：右眼、左眼、鼻尖、右嘴角、左嘴角。
//
// 预处理（严格对齐 OpenCV 参考实现）：
//
//	blobFromImage(aligned, scale=1, Size(112,112), mean=0, swapRB=true, crop=false)
//	→ 即 **取值 0..255 不归一化**，通道为 RGB。
//	本项目源图为 RGB，故与 OpenCV「BGR 源图 + swapRB」等价：直接按 R,G,B 写入即可。

import (
	"image"
	"math"
)

// FaceAlignTemplate SFace 的 112×112 参考五点。
var FaceAlignTemplate = [5][2]float64{
	{38.2946, 51.6963},
	{73.5318, 51.5014},
	{56.0252, 71.7366},
	{41.5493, 92.3655},
	{70.7299, 92.2041},
}

// SimilarityTransform 最小二乘估计 src → dst 的相似变换。
//
// 返回的参数满足：
//
//	dst_x = a*src_x - b*src_y + tx
//	dst_y = b*src_x + a*src_y + ty
//
// 这与 OpenCV 的 estimateAffinePartial2D / Umeyama(禁用反射) 在非退化输入下等价。
// 退化输入（源点全重合，sumSS≈0）返回零变换，调用方据 a²+b²≈0 判定失败。
func SimilarityTransform(src, dst [5][2]float64) (a, b, tx, ty float64) {
	var msx, msy, mdx, mdy float64
	for i := 0; i < 5; i++ {
		msx += src[i][0]
		msy += src[i][1]
		mdx += dst[i][0]
		mdy += dst[i][1]
	}
	msx, msy, mdx, mdy = msx/5, msy/5, mdx/5, mdy/5

	var sumSS, sumDot, sumCross float64
	for i := 0; i < 5; i++ {
		sx := src[i][0] - msx
		sy := src[i][1] - msy
		dx := dst[i][0] - mdx
		dy := dst[i][1] - mdy
		sumSS += sx*sx + sy*sy
		sumDot += dx*sx + dy*sy
		sumCross += dy*sx - dx*sy
	}
	if sumSS == 0 {
		return 0, 0, 0, 0
	}
	a = sumDot / sumSS
	b = sumCross / sumSS
	tx = mdx - a*msx + b*msy
	ty = mdy - b*msx - a*msy
	return a, b, tx, ty
}

// AlignFace 按五点对齐并返回 112×112 的 SFace 输入张量（NCHW 3×112×112，RGB，0..255）。
//
// 越界像素取 0（黑）。变换退化（landmark 重合）时返回全零张量，调用方应据此跳过该脸。
func AlignFace(img image.Image, landmarks [5][2]float64) []float32 {
	a, b, tx, ty := SimilarityTransform(landmarks, FaceAlignTemplate)

	out := make([]float32, 3*FaceSize*FaceSize)
	det := a*a + b*b
	if det == 0 {
		return out
	}
	bounds := img.Bounds()
	total := FaceSize * FaceSize

	for v := 0; v < FaceSize; v++ {
		for u := 0; u < FaceSize; u++ {
			// 逆变换：(u,v) 画布 → 源图坐标
			du := float64(u) - tx
			dv := float64(v) - ty
			sx := (a*du + b*dv) / det
			sy := (-b*du + a*dv) / det
			r, g, bl := bilinearRGB(img, bounds, sx, sy)
			idx := v*FaceSize + u
			// RGB 顺序、0..255 不归一化（对齐 OpenCV swapRB=true + scale=1）
			out[0*total+idx] = r
			out[1*total+idx] = g
			out[2*total+idx] = bl
		}
	}
	return out
}

// bilinearRGB 双线性采样；越界返回 0。
func bilinearRGB(img image.Image, bounds image.Rectangle, x, y float64) (float32, float32, float32) {
	if x < float64(bounds.Min.X) || y < float64(bounds.Min.Y) ||
		x > float64(bounds.Max.X-1) || y > float64(bounds.Max.Y-1) {
		return 0, 0, 0
	}
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	x1, y1 := x0+1, y0+1
	if x1 > bounds.Max.X-1 {
		x1 = bounds.Max.X - 1
	}
	if y1 > bounds.Max.Y-1 {
		y1 = bounds.Max.Y - 1
	}
	fx, fy := x-float64(x0), y-float64(y0)
	w00 := (1 - fx) * (1 - fy)
	w10 := fx * (1 - fy)
	w01 := (1 - fx) * fy
	w11 := fx * fy

	r00, g00, b00 := atRGB(img, x0, y0)
	r10, g10, b10 := atRGB(img, x1, y0)
	r01, g01, b01 := atRGB(img, x0, y1)
	r11, g11, b11 := atRGB(img, x1, y1)

	r := float32(float64(r00)*w00 + float64(r10)*w10 + float64(r01)*w01 + float64(r11)*w11)
	g := float32(float64(g00)*w00 + float64(g10)*w10 + float64(g01)*w01 + float64(g11)*w11)
	bl := float32(float64(b00)*w00 + float64(b10)*w10 + float64(b01)*w01 + float64(b11)*w11)
	return r, g, bl
}

// atRGB 取单像素的 R/G/B（0..255）。
func atRGB(img image.Image, x, y int) (float32, float32, float32) {
	r, g, b, _ := img.At(x, y).RGBA() // 16 位，需 >>8 回到 8 位
	return float32(r >> 8), float32(g >> 8), float32(b >> 8)
}
