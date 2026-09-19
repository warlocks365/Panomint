package faces

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
)

// ScanSourceKind 表示本次检测实际用的图像来源。
type ScanSourceKind string

const (
	SourceOriginal ScanSourceKind = "original" // 原图（首选）
	SourceThumb    ScanSourceKind = "thumb"    // LG 缩略图（回退）
)

// ScanSource 记录一次检测实际用了哪张图。
//
// 为什么要把它返回出来而不是只返回 image：人脸召回出问题时第一个要回答的问题就是
// "这张图到底是原图还是缩略图"，而两者的人脸尺度完全不同。没有这个信息就只能靠猜。
type ScanSource struct {
	Kind ScanSourceKind
	Path string
	// Scale 「图源像素坐标 → LG/1280 坐标系」的缩放系数（= LGWidth / 图源宽，等比）。
	// faces.bbox 与命名迁移 IoU 统一判定在 LG 坐标系（P0-02）：scanOne 检出后逐框
	// Detection.Scaled(src.Scale) 再 SaveFace / BestFaceMatch；SourceThumb 时恒为 1。
	Scale float64
	// FallbackReason 非空表示"原图本应可用却没用上"（解码失败等），调用方应记日志：
	// 静默回退会把"原图坏了/NAS 没挂上"这类系统性问题伪装成正常的缩略图扫描。
	FallbackReason string
}

// lgScale 计算图源 → LG/1280 坐标系的缩放系数（LG 宽固定 1280、等比缩放，见 LGWidth）。
func lgScale(img image.Image) float64 {
	w := img.Bounds().Dx()
	if w <= 0 {
		return 1
	}
	return float64(LGWidth) / float64(w)
}

// LoadScanImage 按「原图优先、LG 缩略图回退」解码一张用于人脸检测的图像。
//
// **为什么优先原图**：检测输入边长按源图长边自适应（resolveInputSize，上限 FACE_INPUT_MAX），
// 而 LG 缩略图宽被固定成 1280。对 4K 原图而言，喂缩略图等于把检测输入砍到约 1/3，
// 人脸在图上相应变小，靠近 MinFacePx 的那批就检不出来。改喂原图能直接抬升召回，
// 且**不增加推理次数**（仍是一遍），代价只是解码更大的图。
//
// **为什么必须有回退**：原图可能不存在（只导入过缩略图 / NAS 未挂载 / 源文件被移走）。
// 若不做回退，少数缺原图的行会让整轮报错；而 scanOne 的错误语义是"保留旧数据、不回填扫描标记、
// 下轮重试"，于是这些行会**每轮都重试同一批**、永远不收敛。
// ListPending 只挑有 LG 缩略图的行，所以回退目标一定存在。
//
// ⚠️ **路径安全**：m.Path 来自数据库（由本仓 indexer 写入），正常情况下是相对路径；
// 但拼接后仍校验结果落在 mediaRoot 之内，避免 `../` 之类把读取引出媒体根。
func LoadScanImage(m MediaItem, mediaRoot, thumbDir string) (image.Image, ScanSource, error) {
	var fallbackReason string

	if mediaRoot != "" && m.Path != "" {
		p := filepath.Join(mediaRoot, m.Path)
		switch {
		case !underRoot(p, mediaRoot):
			// 越界不尝试读取，只记因由并回退 —— 这是防御性判断，不是预期路径。
			fallbackReason = fmt.Sprintf("原图路径越出媒体根，已拒绝读取：%s", m.Path)
		case !fileExists(p):
			// 最常见且完全正常的情形（只导入了缩略图）：不记 reason，避免刷日志。
			fallbackReason = ""
		default:
			img, err := DecodeImage(p)
			if err == nil {
				return img, ScanSource{Kind: SourceOriginal, Path: p, Scale: lgScale(img)}, nil
			}
			// 文件在但解不开（截断/损坏/格式不支持）：回退，但**必须把原因带出去**。
			fallbackReason = fmt.Sprintf("原图存在但解码失败，已回退缩略图：%s（%v）", p, err)
		}
	}

	if m.ThumbLG == "" {
		return nil, ScanSource{}, errors.New("既无可用原图也无 LG 缩略图")
	}
	p := filepath.Join(thumbDir, filepath.Base(m.ThumbLG))
	img, err := DecodeImage(p)
	if err != nil {
		return nil, ScanSource{}, fmt.Errorf("LG 缩略图解码失败（%s）: %w", p, err)
	}
	return img, ScanSource{Kind: SourceThumb, Path: p, Scale: lgScale(img), FallbackReason: fallbackReason}, nil
}

// fileExists 只判断"存在且是普通文件"，不做可读性探测（交给 DecodeImage 报真实错误）。
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// underRoot 判断 p 是否落在 root 之内（含 root 本身）。用于阻断 `../` 越界读取。
func underRoot(p, root string) bool {
	ap, err1 := filepath.Abs(p)
	ar, err2 := filepath.Abs(root)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(ar, ap)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
