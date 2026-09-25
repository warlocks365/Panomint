package index

// 目录树浏览（Job000117）：GET /admin/fs/tree——管理端扫描导入的目录选择器数据源。
//
// 设计要点：
//   - **懒加载单层**：每次只返回目标目录的直接子目录，前端展开到哪层才请求哪层——
//     层级再深也只付当前层的成本，天然规避性能/内存问题。
//   - **只列目录**：文件不上树（扫描对象是目录）。os.ReadDir 的 DirEntry 来自 lstat
//     语义，符号链接（即使指向目录）IsDir()=false 被天然排除——挂载目录里常见的
//     符号链接不会把树引出 MEDIA_ROOT 之外。
//   - **无权限=锁定态而非报错**：子目录逐项做"打开+读 1 条"的真实探测，readable=false
//     的项原样返回给前端渲染锁定图标；目标目录本身不可读时返回 200 + unreadable=true
//     （同样不报错，前端就地提示）。
//   - **防穿越复用 resolveScanDir**：与 POST /admin/scan 同一条边界（ErrDirEscapesRoot）。
//
// 路由（cmd/api/main.go）：
//
//	authed.GET("/admin/fs/tree", auth.RequirePerm(authStore, "admin:system"), indexH.ListDirTree)
//
// nginx 无需改动：/admin 已在 docker/web/Dockerfile 的「纯 API 组」正则内。

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
)

// treeEntry 目录树节点（仅目录）。
type treeEntry struct {
	// Name 目录名（展示用）。
	Name string `json:"name"`
	// Rel 相对 MEDIA_ROOT 的斜杠路径（传给 POST /admin/scan 的 dir 值）。
	Rel string `json:"rel"`
	// Readable api 进程是否可打开该目录：false = 前端以锁定态/提示展示，不允许展开。
	Readable bool `json:"readable"`
}

// ListDirTree GET /admin/fs/tree?dir=<相对 MEDIA_ROOT 的目录>。
//
//	200 {root,dir,unreadable,items:[{name,rel,readable}]}   unreadable=true 表示目标目录本身无权限
//	400 INVALID_INPUT   dir 越界 / 不存在 / 非目录
//	500 INTERNAL        列举出现非权限类底层错误
func (h *Handler) ListDirTree(c *gin.Context) {
	absDir, err := resolveScanDir(h.MediaRoot, c.Query("dir"))
	if errors.Is(err, ErrDirEscapesRoot) {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "INVALID_INPUT", "目录路径无效", err)
		return
	}
	info, err := os.Stat(absDir)
	if err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "目录不存在或不可访问")
		return
	}
	if !info.IsDir() {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "dir 必须是目录而非文件")
		return
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			// 目标目录本身无权限：不报错，给锁定语义让前端就地提示。
			c.JSON(http.StatusOK, gin.H{
				"root":       h.MediaRoot,
				"dir":        relToRoot(h.MediaRoot, absDir),
				"unreadable": true,
				"items":      []treeEntry{},
			})
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "列举目录失败", err)
		return
	}

	parentRel := relToRoot(h.MediaRoot, absDir)
	items := make([]treeEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue // 文件不上树；符号链接（含指向目录的）同样被 lstat 语义排除
		}
		rel := e.Name()
		if parentRel != "." {
			rel = parentRel + "/" + e.Name()
		}
		items = append(items, treeEntry{
			Name:     e.Name(),
			Rel:      rel,
			Readable: dirReadable(filepath.Join(absDir, e.Name())),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"root":       h.MediaRoot,
		"dir":        parentRel,
		"unreadable": false,
		"items":      items,
	})
}

// dirReadable 真实探测目录可读性：打开并尝试读 1 条目录项。空目录返回 io.EOF 也算可读。
func dirReadable(abs string) bool {
	f, err := os.Open(abs)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.ReadDir(1)
	return err == nil || errors.Is(err, io.EOF)
}
