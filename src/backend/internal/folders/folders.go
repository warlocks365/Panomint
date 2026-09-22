// Package folders 文件夹视图端点（API v1.1 §9）。
// 目录树从 media.folder_path 聚合生成（folders 表无数据时的降级策略，决策见任务说明）。
package folders

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/httperr"
)

// Handler 文件夹端点。
type Handler struct {
	Pool *pgxpool.Pool
}

// Node 目录树节点。
type Node struct {
	ID         string   `json:"id"` // 以路径为稳定 ID（根为 ""）
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Count      int      `json:"count"` // 直接位于该目录的媒体数
	Registered bool     `json:"registered,omitempty"` // 注册目录（含空目录，Job000069）
	Owner      bool     `json:"owner,omitempty"`      // 当前用户是否该目录属主（管理操作入口显示依据）
	Grants     []grant  `json:"grants,omitempty"`     // 仅属主视角返回（授权编辑用）
	Children   []*Node  `json:"children"`
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
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var p string
		var n int
		if err := rows.Scan(&p, &n); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		counts[p] = n
	}
	rows.Close()

	// 注册目录合并（Job000069）：空目录在 folder_path 派生中不存在，需 UNION 注册表；
	// 口径与媒体一致——owner/admin 全量，其余=本人 owner 行 ∪ 授予本人 read 的行。
	// grants 仅属主视角带出（授权编辑），他人只见目录本身。
	regWhere := "1=1"
	regArgs := []any{}
	if role != "owner" && role != "admin" {
		regArgs = append(regArgs, userID, userID)
		regWhere = "(f.owner_id = $1 OR EXISTS (SELECT 1 FROM jsonb_array_elements(f.grants) ge WHERE ge->>'user_id' = $2 AND (ge->>'read')::boolean))"
	}
	regRows, err := h.Pool.Query(c.Request.Context(),
		`SELECT f.path, f.owner_id::text = $1, f.owner_id, f.grants::text
		 FROM folder_dirs f WHERE `+regWhere, append(regArgs, userID)...)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer regRows.Close()
	registered := map[string]regMeta{}
	for regRows.Next() {
		var p, ownMark, ownerID, gtext string
		if err := regRows.Scan(&p, &ownMark, &ownerID, &gtext); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		if _, ok := counts[p]; !ok {
			counts[p] = 0 // 空目录：在派生 counts 中补位
		}
		var gs []grant
		if gtext != "[]" && gtext != "" {
			_ = json.Unmarshal([]byte(gtext), &gs)
		}
		registered[p] = regMeta{owner: ownMark == "true", grants: gs}
	}

	tree := BuildTree(counts)
	annotate(tree, registered)
	c.JSON(http.StatusOK, tree)
}

// regMeta 注册目录的标注元数据（annotate 用）。
type regMeta struct {
	owner  bool
	grants []grant
}

// annotate 把注册元数据回填到树节点（按 path 匹配）。
func annotate(n *Node, registered map[string]regMeta) {
	if meta, ok := registered[n.Path]; ok {
		n.Registered = true
		n.Owner = meta.owner
		if meta.owner {
			n.Grants = meta.grants
		}
	}
	for _, ch := range n.Children {
		annotate(ch, registered)
	}
}
