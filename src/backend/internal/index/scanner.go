// Package index 媒体索引服务：扫描目录、提取元数据、写 media 表、派发缩略图任务。
// 对应任务 T1.4；设计依据：TDD v1.1 §3.2 索引管线。
package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Kind 媒体大类（对齐 media.type 枚举；360 判定后续任务处理，扫描阶段只分照片/视频）。
type Kind string

const (
	KindPhoto Kind = "photo"
	KindVideo Kind = "video"
)

// 支持的扩展名 → 媒体大类（小写，含点）。
// 含 Insta360 原生格式（P1 迁移工具）：.insp 照片、.insv/.lrv 视频。
var extKind = map[string]Kind{
	".jpg":  KindPhoto,
	".jpeg": KindPhoto,
	".png":  KindPhoto,
	".webp": KindPhoto,
	".insp": KindPhoto,
	".mp4":  KindVideo,
	".mov":  KindVideo,
	".insv": KindVideo,
	".lrv":  KindVideo,
}

// ClassifyExt 按扩展名识别媒体类型；不支持则 ok=false。
func ClassifyExt(name string) (Kind, bool) {
	k, ok := extKind[strings.ToLower(filepath.Ext(name))]
	return k, ok
}

// FileEntry 一个待索引的媒体文件。
type FileEntry struct {
	Path     string    // 绝对路径
	Rel      string    // 相对扫描根的路径（斜杠分隔，写入 media.path）
	Folder   string    // 相对目录（media.folder_path，根目录为 ""）
	Filename string    // 文件名
	Kind     Kind      // photo|video
	Size     int64     // 字节数
	ModTime  time.Time // 文件 mtime（EXIF 缺失时回退为 taken_at）
	Hash     string    // 内容 sha256（hex，64 字符，对齐 media.hash 宽度）
}

// HashFile 计算文件内容 sha256。
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ScanDir 递归遍历 root，返回全部受支持媒体文件（含 sha256），按相对路径排序保证可重放。
// ctx 取消时返回已收集结果与 ctx.Err()。
func ScanDir(ctx context.Context, root string) ([]FileEntry, error) {
	var out []FileEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			// 跳过群晖 sidecar 目录（内含缩略图 jpeg 副本，导入会污染媒体库；TDD §8.8）
			if d.Name() == "@eaDir" {
				return filepath.SkipDir
			}
			return nil
		}
		kind, ok := ClassifyExt(d.Name())
		if !ok {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("扫描 %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		hash, err := HashFile(path)
		if err != nil {
			return fmt.Errorf("哈希 %s: %w", path, err)
		}
		out = append(out, FileEntry{
			Path:     path,
			Rel:      rel,
			Folder:   folderOf(rel),
			Filename: d.Name(),
			Kind:     kind,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Hash:     hash,
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out, err
}

// folderOf 取相对路径的目录部分；根目录文件返回 ""。
func folderOf(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
