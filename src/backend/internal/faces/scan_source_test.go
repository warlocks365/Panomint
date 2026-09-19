package faces

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writePNG 造一张纯色 PNG 并返回其路径。用尺寸区分"读到的是哪张图" ——
// 断言尺寸比断言"没报错"强得多：错误地读到缩略图时尺寸会立刻不同。
func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

func TestLoadScanImagePrefersOriginal(t *testing.T) {
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	thumbDir := filepath.Join(root, "thumbs")

	writePNG(t, filepath.Join(mediaRoot, "2024/a.png"), 400, 300)
	writePNG(t, filepath.Join(thumbDir, "a_LG.webp.png"), 50, 40)

	m := MediaItem{ID: "m1", Filename: "a.png", ThumbLG: "a_LG.webp.png", Path: "2024/a.png"}
	img, src, err := LoadScanImage(m, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("应成功：%v", err)
	}
	if src.Kind != SourceOriginal {
		t.Errorf("有原图时必须优先用原图，实际 %s（回退原因 %q）", src.Kind, src.FallbackReason)
	}
	if b := img.Bounds(); b.Dx() != 400 || b.Dy() != 300 {
		t.Errorf("读到的不是原图：尺寸 %dx%d（期望 400x300）", b.Dx(), b.Dy())
	}
}

func TestLoadScanImageFallsBackToThumb(t *testing.T) {
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	thumbDir := filepath.Join(root, "thumbs")
	writePNG(t, filepath.Join(thumbDir, "b_LG.webp.png"), 50, 40)

	// 原图不存在（只导入过缩略图的常见情形）：静默回退，不产生 reason。
	m := MediaItem{ID: "m2", Filename: "b.png", ThumbLG: "b_LG.webp.png", Path: "2024/b.png"}
	img, src, err := LoadScanImage(m, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("应回退成功：%v", err)
	}
	if src.Kind != SourceThumb {
		t.Errorf("原图缺失时应回退到缩略图，实际 %s", src.Kind)
	}
	if src.FallbackReason != "" {
		t.Errorf("「原图本就不存在」属正常情形，不该记回退原因，实际 %q", src.FallbackReason)
	}
	if b := img.Bounds(); b.Dx() != 50 {
		t.Errorf("读到的不是缩略图：宽 %d（期望 50）", b.Dx())
	}
}

// TestLoadScanImageEmptyMediaRoot：未配置 mediaRoot（例如部署时没挂原图）时
// 必须直接走缩略图，而不是把空根当成根去拼路径。
func TestLoadScanImageEmptyMediaRoot(t *testing.T) {
	root := t.TempDir()
	thumbDir := filepath.Join(root, "thumbs")
	writePNG(t, filepath.Join(thumbDir, "c_LG.webp.png"), 50, 40)

	m := MediaItem{ID: "m3", Filename: "c.png", ThumbLG: "c_LG.webp.png", Path: "2024/c.png"}
	img, src, err := LoadScanImage(m, "", thumbDir)
	if err != nil {
		t.Fatalf("应成功：%v", err)
	}
	if src.Kind != SourceThumb || img.Bounds().Dx() != 50 {
		t.Errorf("mediaRoot 为空时应直接用缩略图，实际 kind=%s 宽=%d", src.Kind, img.Bounds().Dx())
	}
}

// TestLoadScanImageRejectsPathEscape：`../` 越界必须被拒绝读取（回退到缩略图），
// 且**必须留下 reason** —— 静默回退会把路径异常伪装成正常扫描。
func TestLoadScanImageRejectsPathEscape(t *testing.T) {
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	thumbDir := filepath.Join(root, "thumbs")
	if err := os.MkdirAll(mediaRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	// 故意放在 mediaRoot 之外，若越界检查失效就会被读到。
	writePNG(t, filepath.Join(root, "outside.png"), 400, 300)
	writePNG(t, filepath.Join(thumbDir, "d_LG.webp.png"), 50, 40)

	m := MediaItem{ID: "m4", Filename: "d.png", ThumbLG: "d_LG.webp.png", Path: "../outside.png"}
	img, src, err := LoadScanImage(m, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("应回退成功而不是报错：%v", err)
	}
	if src.Kind != SourceThumb {
		t.Fatalf("越界路径必须被拒绝、回退到缩略图，实际 %s（%s）", src.Kind, src.Path)
	}
	if img.Bounds().Dx() != 50 {
		t.Errorf("越界原图被读到了：宽 %d（期望缩略图的 50）", img.Bounds().Dx())
	}
	if src.FallbackReason == "" {
		t.Error("越界属异常情形，必须留下 FallbackReason 供排障")
	}
}

// TestLoadScanImageCorruptOriginalFallsBackWithReason：原图在但解不开时同样回退，
// 但**必须带 reason** —— 这能把"原图损坏/NAS 半挂载"这类系统性问题暴露出来。
func TestLoadScanImageCorruptOriginalFallsBackWithReason(t *testing.T) {
	root := t.TempDir()
	mediaRoot := filepath.Join(root, "media")
	thumbDir := filepath.Join(root, "thumbs")
	if err := os.MkdirAll(filepath.Join(mediaRoot, "2024"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "2024/e.png"), []byte("not an image at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(thumbDir, "e_LG.webp.png"), 50, 40)

	m := MediaItem{ID: "m5", Filename: "e.png", ThumbLG: "e_LG.webp.png", Path: "2024/e.png"}
	img, src, err := LoadScanImage(m, mediaRoot, thumbDir)
	if err != nil {
		t.Fatalf("应回退成功：%v", err)
	}
	if src.Kind != SourceThumb {
		t.Fatalf("原图损坏时应回退，实际 %s", src.Kind)
	}
	if img.Bounds().Dx() != 50 {
		t.Errorf("应读到缩略图，实际宽 %d", img.Bounds().Dx())
	}
	if src.FallbackReason == "" {
		t.Error("原图损坏必须留下 FallbackReason（否则系统性问题会被伪装成正常扫描）")
	}
}

// TestLoadScanImageNoSourceAtAll：两个来源都没有时必须**报错**，不能返回 nil 图，
// 否则调用方会在 nil 上做检测而 panic。
func TestLoadScanImageNoSourceAtAll(t *testing.T) {
	root := t.TempDir()
	m := MediaItem{ID: "m6", Filename: "f.png"} // ThumbLG 与 Path 均为空
	img, _, err := LoadScanImage(m, filepath.Join(root, "media"), filepath.Join(root, "thumbs"))
	if err == nil {
		t.Fatal("无任何可用图源时必须报错")
	}
	if img != nil {
		t.Error("报错时不得返回图像")
	}
}

// TestLoadScanImageThumbDecodeFailure：缩略图也解不开时必须报错（而不是回退到空图）。
func TestLoadScanImageThumbDecodeFailure(t *testing.T) {
	root := t.TempDir()
	thumbDir := filepath.Join(root, "thumbs")
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(thumbDir, "g_LG.webp"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := MediaItem{ID: "m7", Filename: "g.png", ThumbLG: "g_LG.webp"}
	if _, _, err := LoadScanImage(m, filepath.Join(root, "media"), thumbDir); err == nil {
		t.Fatal("缩略图解码失败时必须报错")
	}
}
