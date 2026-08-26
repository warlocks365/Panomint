// Package search 结构化搜索（Stage 3 / Job000001，API v1.1 §10）。
//
// 分层设计（Stage 4 语义召回接入时不改 API 契约）：
//   - 解析器：ParseParams 校验/规范化查询参数 → SearchParams
//   - 结构化过滤器：buildWhere（query.go）生成参数化 WHERE
//   - 召回管道：Store.Search 组合结构化过滤 + Recaller 插槽（SemanticRecaller 占位）
//
// 本期不实现：person 参数、pgvector 语义召回、/search/video-moment。
package search

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"panoalbum/internal/media"
)

// SearchParams GET /search 查询参数（对齐 API §10 契约子集）。
type SearchParams struct {
	Q          string // 关键词（空白分隔多 token，pg_trgm 模糊匹配）
	Tag        string
	DateAfter  time.Time // taken_at >= DateAfter（含）
	DateBefore time.Time // taken_at < DateBefore（不含；纯日期按闭区间顺延一天）
	HasAfter   bool
	HasBefore  bool
	Place      string // 地名：文本 trgm 优先，零结果自动降级地理半径
	Type       string // photo|video|360
	Favorites  bool
	Cursor     string
	Limit      int
	UserID     string // 调用者（空间可见性判定）
}

// GeoCenter 地理降级中心点（place 文本零结果时由 resolver 解析得到）。
type GeoCenter struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

// PlaceFallback 地点降级告知（响应可选字段）。
type PlaceFallback struct {
	Mode    string    `json:"mode"` // geo_radius
	Center  GeoCenter `json:"center"`
	RadiusM int       `json:"radius_m"`
}

// SearchResult GET /search 响应（对齐 API §10；place_fallback 为降级扩展字段）。
type SearchResult struct {
	Items         []media.MediaRef `json:"items"`
	NextCursor    string           `json:"next_cursor,omitempty"`
	Total         int              `json:"total"`
	PlaceFallback *PlaceFallback   `json:"place_fallback,omitempty"`
}

// geoRadiusM 地理降级检索半径（米）。
const geoRadiusM = 5000

// 参数校验错误。
var (
	ErrInvalidType = errors.New("type 仅支持 photo|video|360")
	ErrInvalidDate = errors.New("date_after/date_before 需为 YYYY-MM-DD 或 RFC3339")
)

// ParseParams 解析器层：原始查询串 → 规范化 SearchParams（含校验）。
func ParseParams(q func(string) string, userID string) (SearchParams, error) {
	p := SearchParams{
		Q:         strings.TrimSpace(q("q")),
		Tag:       strings.TrimSpace(q("tag")),
		Place:     strings.TrimSpace(q("place")),
		Type:      q("type"),
		Favorites: q("favorites") == "true",
		Cursor:    q("cursor"),
		UserID:    userID,
	}
	if v := q("limit"); v != "" {
		if _, err := fmt.Sscan(v, &p.Limit); err != nil {
			p.Limit = 0
		}
	}
	switch p.Type {
	case "", "photo", "video", "360":
	default:
		return p, ErrInvalidType
	}
	if v := strings.TrimSpace(q("date_after")); v != "" {
		t, ok := parseFlexibleDate(v)
		if !ok {
			return p, ErrInvalidDate
		}
		p.DateAfter, p.HasAfter = t, true
	}
	if v := strings.TrimSpace(q("date_before")); v != "" {
		t, ok := parseDateBefore(v)
		if !ok {
			return p, ErrInvalidDate
		}
		p.DateBefore, p.HasBefore = t, true
	}
	return p, nil
}

// parseFlexibleDate 支持 YYYY-MM-DD / YYYY-MM / RFC3339（与 albums.criteria 一致）。
func parseFlexibleDate(s string) (time.Time, bool) {
	for _, f := range []string{time.RFC3339, "2006-01-02", "2006-01"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// parseDateBefore date_before 为纯日期时按闭区间处理（返回次日 0 点，配合 < 使用）。
func parseDateBefore(s string) (time.Time, bool) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.AddDate(0, 0, 1), true
	}
	return parseFlexibleDate(s)
}
