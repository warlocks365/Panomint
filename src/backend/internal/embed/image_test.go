package embed

// 图像预处理测试。
//
// 重点回归保护：媒体缩略图由处理管线输出为 **WebP**（*_MD.webp），
// 而 Go 标准库 image 不解码 WebP。若忘记注册 x/image/webp 的解码器，
// 批量建库会全线报 "image: unknown format"（Job000010 实测踩到过）。

import (
	"bytes"
	"encoding/base64"
	"image"
	"testing"
)

// webpFixture 16×12 有损 WebP（RIFF....WEBPVP8 ），与线上缩略图同格式。
const webpFixture = "UklGRlgAAABXRUJQVlA4IEwAAACwAQCdASoQAAwAAUAmJbACdAEOtW9oAP79s7ONOU8xZfhVpUF8ztPiX//iRXmuFnomwf6dXN9PqevT3/x7Xp7//aEZVSCiZyjfDwAA"

func TestWebPDecodingSupported(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(webpFixture)
	if err != nil {
		t.Fatalf("夹具解码失败: %v", err)
	}
	if len(raw) < 16 || string(raw[8:12]) != "WEBP" {
		t.Fatalf("夹具不是 WebP：% x", raw[:min(16, len(raw))])
	}

	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("WebP 解码失败（是否漏了 _ \"golang.org/x/image/webp\" 导入？）: %v", err)
	}
	if format != "webp" {
		t.Errorf("识别格式应为 webp，实得 %q", format)
	}
	b := img.Bounds()
	if b.Dx() != 16 || b.Dy() != 12 {
		t.Errorf("尺寸应为 16×12，实得 %d×%d", b.Dx(), b.Dy())
	}

	// 预处理应产出合法的 NCHW 张量
	px := PreprocessImageData(img)
	if len(px) != 3*ImageSize*ImageSize {
		t.Fatalf("预处理长度应为 %d，实得 %d", 3*ImageSize*ImageSize, len(px))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
