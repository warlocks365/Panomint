// Package dirscope 目录作用域解析与钳制（Job000123）。
//
// 存在的理由：扫描导入（管理端 POST /admin/scan、成员端 POST /scan）与目录树浏览
// 都允许调用方给一个「目录」参数，而服务端必须保证最终路径**绝不越出**允许的范围
// （管理端 = MEDIA_ROOT；成员端 = 管理员分配的 scan_root）。穿越判定（filepath.Rel
// 的 ".." 前缀、跨盘符绝对路径）曾在 internal/index 内实现过一次（resolveScanDir，
// Job000113）；Job000123 起成员端复用同一套边界语义，抽出本包作为唯一真源，避免
// 两处各自演化出 subtly 不同的钳制。
//
// 本包只做**纯路径**判定：不碰文件系统（存在性/可读性由调用方 os.Stat/探测），
// 不碰权限（那是 auth 中间件的事）。
package dirscope

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrEscapesScope dir 解析后越出 root（路径穿越）的哨兵。
//
// 与 internal/index 的 ErrDirEscapesRoot 同一条语义（该变量现为本哨兵的别名）。
// handler 只应对**这个哨兵**回显 err.Error()（固定中文文案，须在 errEchoRegistry
// 登记）；filepath.Abs/Rel 的底层错误走固定文案，绝不回显。
var ErrEscapesScope = errors.New("dir 必须位于根目录之内（不允许路径穿越）")

// ReservedSeg 系统保留路径段：挂载导入旧式落点 _imports/<id8>（迁移 00036 语义），
// 成员侧一律不可见/不可扫（管理端 storagectl 专用）。判定覆盖**任意**层级——
// 不管 scan_root 设在哪一层，_imports 子树都不向成员敞开。
const ReservedSeg = "_imports"

// Resolve 把调用方给的 dir 解析为 root 内的绝对路径。
//
// dir 允许相对（相对 root）或绝对；无论哪种，解析结果必须仍位于 root 之内，
// 否则返回 ErrEscapesScope。空 / "." / 空白均表示 root 本身。
func Resolve(root, dir string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("解析根路径: %w", err)
	}
	absRoot = filepath.Clean(absRoot)

	d := strings.TrimSpace(dir)
	var absDir string
	switch {
	case d == "" || d == ".":
		absDir = absRoot
	case filepath.IsAbs(d):
		absDir = filepath.Clean(d)
	default:
		absDir = filepath.Clean(filepath.Join(absRoot, d))
	}

	if !within(absRoot, absDir) {
		return "", ErrEscapesScope
	}
	return absDir, nil
}

// Within 报告 absDir 是否位于 root 之内（两端都先取 Abs+Clean）。
// root 非法（Abs 失败）时 fail-closed 返回 false。
func Within(root, absDir string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	return within(filepath.Clean(absRoot), filepath.Clean(absDir))
}

// RelDisplay 返回 absDir 相对 root 的斜杠展示路径（供响应回显；root 本身返回 "."）。
func RelDisplay(root, absDir string) string {
	rel, err := filepath.Rel(root, absDir)
	if err != nil || rel == "." {
		return "."
	}
	return filepath.ToSlash(rel)
}

// HasReserved 报告相对路径 rel（斜杠或系统分隔符均可）的**任意**路径段是否为保留段。
// rel 传 "." 或空串 = 无保留段。
func HasReserved(rel string) bool {
	for _, seg := range strings.FieldsFunc(rel, func(r rune) bool {
		return r == '/' || r == filepath.Separator
	}) {
		if seg == ReservedSeg {
			return true
		}
	}
	return false
}

// within 已 Clean 的两条绝对路径的包含判定：rel 以 ".." 开头、或 rel 本身是
// 绝对路径（Windows 跨盘符），即越界。
func within(absRoot, absDir string) bool {
	rel, err := filepath.Rel(absRoot, absDir)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}
