// Package folders 文件夹视图端点（API v1.1 §9）。
// 目录树从 media.folder_path 聚合生成（folders 表无数据时的降级策略，决策见任务说明）。
package folders

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler 文件夹端点。
type Handler struct {
	Pool *pgxpool.Pool
}

// Node 目录树节点。
type Node struct {
	ID       string  `json:"id"` // 以路径为稳定 ID（根为 ""）
	Name     string  `json:"name"`
	Path     string  `json:"path"`
	Count    int     `json:"count"` // 直接位于该目录的媒体数
	Children []*Node `json:"children"`
}

// BuildTree 由目录路径集合构建树（paths 元素为 "a/b/c" 形式的相对路径，"" 表示根）。
// 纯函数，便于单测。
func BuildTree(counts map[string]int) *Node {
	root := &Node{ID: "", Name: "", Path: "", Children: []*Node{}}
	nodes := map[string]*Node{"": root}

	// 先按路径排序保证父先于子创建
	paths := make([]string, 0, len(counts))
	for p := range counts {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		if p == "" {
			root.Count = counts[p]
			continue
		}
		segs := strings.Split(p, "/")
		cur := ""
		for i, s := range segs {
			parent := cur
			if cur == "" {
				cur = s
			} else {
				cur = cur + "/" + s
			}
			n, ok := nodes[cur]
			if !ok {
				n = &Node{ID: cur, Name: s, Path: cur, Children: []*Node{}}
				nodes[cur] = n
				nodes[parent].Children = append(nodes[parent].Children, n)
			}
			if i == len(segs)-1 {
				n.Count = counts[p]
			}
		}
	}
	return root
}

// Tree GET /folders/tree：当前用户可见媒体的目录树。
func (h *Handler) Tree(c *gin.Context) {
	userID := c.GetString("user_id")
	role := c.GetString("role")

	// owner/admin 见全量，其余见本人
	where := "deleted_at IS NULL AND folder_path IS NOT NULL"
	args := []any{}
	if role != "owner" && role != "admin" {
		args = append(args, userID)
		where += " AND owner_id = $1"
	}
	rows, err := h.Pool.Query(c.Request.Context(),
		`SELECT COALESCE(folder_path,''), count(*)::int FROM media WHERE `+where+`
		 GROUP BY 1`, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"code": "QUERY_FAILED", "message": err.Error()}})
			return
		}
		counts[p] = n
	}
	c.JSON(http.StatusOK, BuildTree(counts))
}
