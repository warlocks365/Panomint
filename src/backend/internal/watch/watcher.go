package watch

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"panoalbum/internal/index"
)

// skipDir 监听/扫描时跳过的目录名：
// @eaDir 为群晖缩略图/sidecar 目录，内含 jpeg 副本，导入会污染媒体库。
func skipDir(name string) bool {
	return name == "@eaDir" || strings.HasPrefix(name, ".")
}

// Watcher fsnotify 递归目录监听器（Linux inotify / Windows ReadDirectoryChangesW，
// 跨平台差异由 fsnotify 抹平；Rename 拆分事件统一按“路径待复核”处理）。
type Watcher struct {
	root string
	fw   *fsnotify.Watcher
	deb  *Debouncer
}

// NewWatcher 监听 root（递归）；变更路径去抖 delay 后回调 onBatch（绝对路径列表）。
func NewWatcher(root string, delay time.Duration, onBatch func(paths []string)) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("创建 fsnotify: %w", err)
	}
	w := &Watcher{root: root, fw: fw, deb: NewDebouncer(delay, onBatch)}
	if err := w.addRecursive(root); err != nil {
		fw.Close()
		return nil, err
	}
	return w, nil
}

// addRecursive 把 root 下全部子目录加入监听（跳过 @eaDir / 隐藏目录）。
func (w *Watcher) addRecursive(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if err := w.fw.Add(path); err != nil {
			return fmt.Errorf("监听 %s: %w", path, err)
		}
		return nil
	})
}

// Run 事件循环直至 ctx 取消或监听出错。
func (w *Watcher) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.fw.Events:
			if !ok {
				return fmt.Errorf("fsnotify 事件通道关闭")
			}
			w.handle(ev)
		case err, ok := <-w.fw.Errors:
			if !ok {
				return fmt.Errorf("fsnotify 错误通道关闭")
			}
			log.Printf("fsnotify 错误: %v", err)
		}
	}
}

// handle 把事件归一为“待复核路径”：
//   - 新建目录 → 递归补监听（fsnotify 不自动递归）；
//   - Create/Write/Rename → 入去抖集合（Rename 的旧路径 stat 不到会在导入时跳过）；
//   - Remove/Chmod → 忽略（P1 不做删除同步，媒体软删归 API 管）。
func (w *Watcher) handle(ev fsnotify.Event) {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 {
		return
	}
	base := filepath.Base(ev.Name)
	if strings.HasPrefix(base, ".") {
		return // 隐藏文件/目录
	}
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			if !skipDir(base) {
				if err := w.addRecursive(ev.Name); err != nil {
					log.Printf("补监听 %s 失败: %v", ev.Name, err)
				}
			}
			return
		}
	}
	w.deb.Add(ev.Name)
}

// Close 停止去抖并释放 fsnotify 资源。
func (w *Watcher) Close() error {
	w.deb.Stop()
	return w.fw.Close()
}

// EntriesFromPaths 把一批变更绝对路径转成索引条目：
// 过滤不存在/目录/不支持扩展名，rel 以 root 为基准，含内容 sha256。
// 供监听模式增量导入与断点状态标记使用。
func EntriesFromPaths(root string, paths []string) []index.FileEntry {
	var out []index.FileEntry
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue // Rename 旧路径 / 已删除 / 目录
		}
		kind, ok := index.ClassifyExt(info.Name())
		if !ok {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // 不在监听根内
		}
		rel = filepath.ToSlash(rel)
		hash, err := index.HashFile(p)
		if err != nil {
			log.Printf("哈希 %s 失败: %v", p, err)
			continue
		}
		out = append(out, index.FileEntry{
			Path:     p,
			Rel:      rel,
			Folder:   folderOfRel(rel),
			Filename: info.Name(),
			Kind:     kind,
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Hash:     hash,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	return out
}

// folderOfRel 与 index 包 folderOf 同逻辑（未导出，局部复刻避免改既有 API）。
func folderOfRel(rel string) string {
	dir := filepath.ToSlash(filepath.Dir(rel))
	if dir == "." || dir == "/" {
		return ""
	}
	return dir
}
