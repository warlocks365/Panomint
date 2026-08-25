// Package albums 相册 + 评论后端（Stage 1）。
//
// 契约映射说明：
//   - API kind: normal|smart|favorites ↔ albums.type: manual|smart|favorites
//   - API criteria ↔ albums.query (JSONB)
package albums

import (
	"fmt"
	"strings"
	"time"
)

// Criteria 智能相册条件（存 albums.query JSONB）。
type Criteria struct {
	Type      string `json:"type,omitempty"`       // photo|video|360
	DateFrom  string `json:"date_from,omitempty"`  // YYYY-MM-DD 或 RFC3339
	DateTo    string `json:"date_to,omitempty"`    // 日期按当天闭区间处理
	Place     string `json:"place,omitempty"`      // 地名模糊匹配
	Favorites bool   `json:"favorites,omitempty"`  // 仅收藏
}

// kindToType API kind → 数据库 album_type。
func kindToType(kind string) string {
	if kind == "smart" {
		return "smart"
	}
	return "manual"
}

// typeToKind 数据库 album_type → API kind。
func typeToKind(t string) string {
	if t == "manual" {
		return "normal"
	}
	return t
}

// buildCriteriaWhere 由智能条件组装 WHERE（参数化；模式复用 internal/media/timeline.go，
// 差异：不限 space、不限 owner，仅排除已删除媒体）。
func buildCriteriaWhere(c *Criteria) (string, []any) {
	conds := []string{"m.deleted_at IS NULL"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if c == nil {
		return strings.Join(conds, " AND "), args
	}
	switch c.Type {
	case "photo", "video":
		add("m.type = $%d AND m.is_360 = false", c.Type)
	case "360":
		conds = append(conds, "m.is_360 = true")
	}
	if c.DateFrom != "" {
		if t, ok := parseFlexibleDate(c.DateFrom); ok {
			add("m.taken_at >= $%d", t)
		}
	}
	if c.DateTo != "" {
		if t, ok := parseDateTo(c.DateTo); ok {
			add("m.taken_at < $%d", t)
		}
	}
	if c.Place != "" {
		add("m.place ILIKE '%%' || $%d || '%%'", c.Place)
	}
	if c.Favorites {
		conds = append(conds, `EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id
			WHERE ai.media_id = m.id AND a.type = 'favorites')`)
	}
	return strings.Join(conds, " AND "), args
}

// parseFlexibleDate 支持 YYYY-MM-DD / YYYY-MM / RFC3339。
func parseFlexibleDate(s string) (time.Time, bool) {
	for _, f := range []string{time.RFC3339, "2006-01-02", "2006-01"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseDateTo 解析 date_to：纯日期按闭区间处理（返回次日 0 点，配合 < 使用）。
func parseDateTo(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.AddDate(0, 0, 1), true
	}
	return parseFlexibleDate(s)
}

// effectiveCover 首图回填：cover 为空时取相册首项媒体。
func effectiveCover(coverMediaID, firstItemMediaID *string) *string {
	if coverMediaID != nil && *coverMediaID != "" {
		return coverMediaID
	}
	return firstItemMediaID
}

// ErrThirdLevel 两级评论约束：parent 自身已是回复时拒绝。
func checkReplyAllowed(parentHasParent bool) bool {
	return !parentHasParent
}
