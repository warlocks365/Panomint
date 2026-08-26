// Package watch P1 数据迁移工具的支撑库：fsnotify 增量监听、JSON 断点续扫状态、rsync 封装。
// 设计依据：TDD v1.1 §8.8 / 开发任务计划 T6.1（rsync + inotify 扫描器）。
package watch

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"panoalbum/internal/index"
)

// stateVersion 状态文件格式版本（结构变更时递增并做迁移）。
const stateVersion = 1

// FileState 单个已处理文件的指纹：size+mtime 快速判变，hash 记录入库内容。
type FileState struct {
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mtime"`
	Hash    string    `json:"hash,omitempty"`
}

// State 断点续扫状态：key 为相对扫描根路径（斜杠分隔，对齐 index.FileEntry.Rel）。
type State struct {
	Version int                  `json:"version"`
	Root    string               `json:"root"`
	Files   map[string]FileState `json:"files"`

	path string // 落盘路径（不序列化）
}

// LoadState 读取 path 的状态文件；不存在或 root 不匹配时返回全新空状态。
func LoadState(path, root string) (*State, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &State{Version: stateVersion, Root: filepath.ToSlash(abs), Files: map[string]FileState{}, path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("读状态文件: %w", err)
	}
	var old State
	if err := json.Unmarshal(data, &old); err != nil {
		return nil, fmt.Errorf("解析状态文件 %s: %w", path, err)
	}
	if old.Root != s.Root || old.Files == nil {
		return s, nil // 根目录变了：从头扫
	}
	old.path = path
	return &old, nil
}

// Save 原子落盘（tmp + rename，避免中断时写坏状态文件）。
func (s *State) Save() error {
	if s.path == "" {
		return fmt.Errorf("状态路径为空")
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写状态文件: %w", err)
	}
	return os.Rename(tmp, s.path)
}

// Path 返回状态文件落盘路径。
func (s *State) Path() string { return s.path }

// Pending 过滤出需要导入的条目：不在状态中，或 size/mtime 已变化（重扫覆盖）。
// 已记录且指纹未变的条目视为已导入，跳过（幂等断点续扫）。
func (s *State) Pending(entries []index.FileEntry) []index.FileEntry {
	var out []index.FileEntry
	for _, e := range entries {
		fs, ok := s.Files[e.Rel]
		if ok && fs.Size == e.Size && fs.ModTime.Equal(e.ModTime) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Mark 记录条目已处理（inserted/duplicate 均视为完成；failed 不记录，下次重试）。
func (s *State) Mark(e index.FileEntry) {
	s.Files[e.Rel] = FileState{Size: e.Size, ModTime: e.ModTime, Hash: e.Hash}
}

// Done 返回已完成条目数。
func (s *State) Done() int { return len(s.Files) }
