package embed

// CLIP 图像预处理：最短边缩放到 224 → 中心裁剪 224×224 → 归一化 → NCHW float32。
//
// 参数直接取自模型的 preprocessor_config.json（CLIPFeatureExtractor），
// 与 HF CLIPImageProcessor 的默认行为一致：
//   do_resize(shortest_edge=224) → do_center_crop(224×224, 保持宽高比)
//   → do_rescale(1/255) → do_normalize(mean/std) → CHW

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"

	xdraw "golang.org/x/image/draw"
	// 缩略图由处理管线输出为 WebP（thumbnail_sm/md/lg 均为 *_MD.webp），
	// 标准库 image 不解码 WebP —— 缺此注册会全线报 "image: unknown format"。
	_ "golang.org/x/image/webp"
)

// ImageSize CLIP 输入边长。
const ImageSize = 224

// CLIP 归一化参数（preprocessor_config.json）。
var (
	clipMean = [3]float32{0.48145466, 0.4578275, 0.40821073}
	clipStd  = [3]float32{0.26862954, 0.26130258, 0.27577711}
)

// PreprocessImage 读图 → CLIP 输入张量（NCHW，长度 3*224*224）。
func PreprocessImage(path string) ([]float32, error) {
	img, err := DecodeImageFile(path)
	if err != nil {
		return nil, err
	}
	return PreprocessImageData(img), nil
}

// DecodeImageFile 解码图片文件（供按模型族选择不同预处理时复用）。
func DecodeImageFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开图片失败: %w", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("解码图片失败: %w", err)
	}
	return img, nil
}

// PreprocessImageData 对已解码图像做预处理。
func PreprocessImageData(img image.Image) []float32 {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	// 1) 最短边缩放到 ImageSize，保持宽高比（对齐 HF：new_short=224, new_long=int(224*long/short)）
	short, long := w, h
	if w > h {
		short, long = h, w
	}
	newShort := ImageSize
	newLong := int(float64(ImageSize) * float64(long) / float64(short))
	if newLong < ImageSize {
		newLong = ImageSize
	}
	var rw, rh int
	if w <= h {
		rw, rh = newShort, newLong
	} else {
		rw, rh = newLong, newShort
	}
	resized := image.NewRGBA(image.Rect(0, 0, rw, rh))
	// CatmullRom 近似 PIL 的 BICUBIC
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, b, xdraw.Over, nil)

	// 2) 中心裁剪 ImageSize×ImageSize 后归一化
	offX := (rw - ImageSize) / 2
	offY := (rh - ImageSize) / 2
	return toNCHW(resized, offX, offY)
}

// PreprocessImageDataResize 直接缩放到 ImageSize×ImageSize（**不裁剪**）。
//
// Chinese-CLIP 的 preprocessor_config 为 do_center_crop=false + size=224×224，
// 即整幅图直接缩放成方形（宽高比会被改变），与 OpenAI CLIP 的
// 「最短边缩放 + 中心裁剪」不同，故单独提供该变体。
func PreprocessImageDataResize(img image.Image) []float32 {
	resized := image.NewRGBA(image.Rect(0, 0, ImageSize, ImageSize))
	xdraw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	return toNCHW(resized, 0, 0)
}

// toNCHW 从 (offX, offY) 起取 ImageSize×ImageSize，归一化并按 NCHW 排布。
func toNCHW(src *image.RGBA, offX, offY int) []float32 {
	out := make([]float32, 3*ImageSize*ImageSize)
	const plane = ImageSize * ImageSize
	for y := 0; y < ImageSize; y++ {
		for x := 0; x < ImageSize; x++ {
			c := color.NRGBAModel.Convert(src.At(offX+x, offY+y)).(color.NRGBA)
			idx := y*ImageSize + x
			out[0*plane+idx] = (float32(c.R)/255.0 - clipMean[0]) / clipStd[0]
			out[1*plane+idx] = (float32(c.G)/255.0 - clipMean[1]) / clipStd[1]
			out[2*plane+idx] = (float32(c.B)/255.0 - clipMean[2]) / clipStd[2]
		}
	}
	return out
}
