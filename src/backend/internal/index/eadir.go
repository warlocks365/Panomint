package index

// @eaDir 缩略图复用（Job000056，T6.1 数据迁移工具的第一块）。
//
// 背景：从群晖 Synology Photos 迁移时，源目录里每个媒体旁边有群晖预生成的缩略图：
//
//	<媒体目录>/@eaDir/<媒体文件名>/SYNOFILE_THUMB_S.jpg   （小）
//	<媒体目录>/@eaDir/<媒体文件名>/SYNOFILE_THUMB_M.jpg   （中）
//	<媒体目录>/@eaDir/<媒体文件名>/SYNOFILE_THUMB_L.jpg   （大）
//
// 这些图是用户已付过一次算力的产物。逐张重新跑缩略图管线是大批量迁移里最耗时
// 的一环——直接转码复用（JPEG→WebP，尺寸不变）能把这段成本消掉。
//
// 语义取舍（[自行决策]，均可复议）：
//  1. **仅当 S/M/L 三档齐全才复用**：本系统缩略图是三档一体 UPDATE 落库（worker.go
//     的 UPDATE 一次写三列），缺档复用会造成"两档旧一档新"的拼接态；不齐→整组走
//     原有队列生成（worker 一次出三档，语义干净）。
//  2. **转码在 indexer 进程内同步 ffmpeg subprocess**：不动 worker/队列协议（生产
//     路径零影响）；单张失败不致命——清理已落盘的半成品并**回落**到队列生成。
//  3. **仅新插入的媒体参与**（duplicate 已有行不重复处理）。
//  4. 命名/落库与 worker 逐字节一致（<id>_<SM|MD|LG>.webp + 同一条 UPDATE），
//     由本文件头注释与 eadir_test.go 的命名守卫钉住，防两处漂移。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"panoalbum/internal/ffmpeg"
)

// synoThumbNames SYNOFILE 后缀 → 本系统档位常量（与 ffmpeg.ThumbSize 同串）。
var synoThumbNames = map[string]ffmpeg.ThumbSize{
	"S": ffmpeg.ThumbSM,
	"M": ffmpeg.ThumbMD,
	"L": ffmpeg.ThumbLG,
}

// FindEAThumbs 查媒体文件旁边的群晖 sidecar 缩略图。
//
// 返回 size→sidecar 绝对路径；**三档齐全**才返回非 nil，否则返回 nil（调用方
// 走原有队列生成）。媒体旁无 @eaDir 属常态（非群晖来源），同样 nil，无日志噪音。
func FindEAThumbs(mediaAbs string) map[ffmpeg.ThumbSize]string {
	dir := filepath.Join(filepath.Dir(mediaAbs), "@eaDir", filepath.Base(mediaAbs))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := map[ffmpeg.ThumbSize]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		rest, ok := strings.CutPrefix(name, "SYNOFILE_THUMB_")
		if !ok {
			continue
		}
		// 文件名形如 SYNOFILE_THUMB_M.jpg：取扩展名前的档位字母。
		ext := filepath.Ext(rest) // ".jpg"
		sizeKey := strings.TrimSuffix(rest, ext)
		size, ok := synoThumbNames[sizeKey]
		if !ok {
			continue
		}
		if ext != ".jpg" && ext != ".jpeg" && ext != ".JPG" && ext != ".JPEG" {
			continue
		}
		out[size] = filepath.Join(dir, name)
	}
	if len(out) != len(synoThumbNames) {
		return nil // 缺档：整组交回队列生成
	}
	return out
}

// eaThumbPath 复用落盘路径：与 worker.thumbPath 同规则（<thumbDir>/<id>_<档>.webp）。
// 两处独立实现由 eadir_test.go 的命名守卫钉住——不导出 worker 的私有方法是为了
// 不扩大 Indexer 与 ThumbWorker 的耦合，规则本身只有一行。
func eaThumbPath(thumbDir, mediaID string, size ffmpeg.ThumbSize) string {
	return filepath.Join(thumbDir, fmt.Sprintf("%s_%s.webp", mediaID, size))
}

// ConvertEAThumbs 把三档 sidecar JPEG 转码为本系统 WebP 并落盘（尺寸不变，quality 82，
// 与 worker 的 libwebp 参数一致——见 ffmpeg 包预设）。任一失败即清理已落盘者并返回错误
// （调用方回落队列生成，不留半成品）。
func ConvertEAThumbs(ctx context.Context, thumbDir, mediaID string, thumbs map[ffmpeg.ThumbSize]string) error {
	var done []string
	for _, size := range []ffmpeg.ThumbSize{ffmpeg.ThumbSM, ffmpeg.ThumbMD, ffmpeg.ThumbLG} {
		src, ok := thumbs[size]
		if !ok {
			return fmt.Errorf("缺 %s 档 sidecar", size)
		}
		dst := eaThumbPath(thumbDir, mediaID, size)
		args := []string{"-y", "-i", src, "-c:v", "libwebp", "-quality", "82", dst}
		if _, err := ffmpeg.New(args).Run(ctx); err != nil {
			for _, p := range done {
				os.Remove(p)
			}
			return fmt.Errorf("sidecar %s 档转码: %w", size, err)
		}
		done = append(done, dst)
	}
	return nil
}

// eaThumbDir 复用落盘目录：与 worker 同源自 THUMB_DIR 环境变量。
// 空 = 未配置 → 调用方跳过复用（保守回落队列生成）。
func eaThumbDir() string { return os.Getenv("THUMB_DIR") }
