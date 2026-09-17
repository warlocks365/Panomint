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
	Type      string `json:"type,omitempty"`      // photo|video|360
	DateFrom  string `json:"date_from,omitempty"` // YYYY-MM-DD 或 RFC3339
	DateTo    string `json:"date_to,omitempty"`   // 日期按当天闭区间处理
	Place     string `json:"place,omitempty"`     // 地名模糊匹配
	Favorites bool   `json:"favorites,omitempty"` // 仅收藏
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
// 差异：不限 space，仅排除已删除媒体）。
//
// albumOwnerID 是**相册的属主**，不是调用者：智能相册的条目集合由该相册属主的数据定义，
// 因此收敛到相册属主（m.owner_id = $N）。旧实现完全没有 owner 条件，等于把**全站**所有
// 媒体当作候选集，任何能读到某个智能相册的人都能顺带枚举别人的照片。
//
// 刻意**不**从请求上下文取身份 —— 本函数同时服务于 internal/shares 的公开分享链路
// （shares.Store.ListItems → albums.Store.Get），匿名请求根本没有主体；一旦改成按调用者过滤，
// 匿名分享会被 fail-closed 整条打死，而分享是**有意**让匿名可见的。属主只能由调用点传入。
//
// 决策（2026-09）：智能相册"是否应跨用户聚合"本身是产品问题，本次取最保守口径 ——
// 收敛到相册属主。若产品意图是跨用户聚合，需由用户重新裁决，而不是悄悄放开这里。
//
// 占位符约定：相册属主恒有一个条件，且**永远是最后一个占位符**（$len(args)），
// 既有条件的编号不受影响。编号与 args 必须自洽，否则只在运行期报 "expected N arguments"。
func buildCriteriaWhere(c *Criteria, albumOwnerID string) (string, []any) {
	conds := []string{"m.deleted_at IS NULL"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	if c != nil {
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
	}
	// c == nil（无条件的智能相册）也必须带属主约束 —— 否则它就是"全站媒体"。
	add("m.owner_id = $%d", albumOwnerID)
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
