// Package media 媒体/时间轴查询（API v1.1 §3）。
package media

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store 媒体查询。
type Store struct {
	Pool *pgxpool.Pool
}

// ListParams GET /media 查询参数（对齐 API §3 契约）。
type ListParams struct {
	Scope     MediaScope // 空间作用域（唯一真源，见 scope.go；零值会被收敛为空结果）
	View      string     // year|month|day|all
	Date      string     // YYYY / YYYY-MM / YYYY-MM-DD
	Type      string     // photo|video|360
	Favorites bool
	Tag       string
	Person    string
	Place     string
	Cursor    string
	Limit     int
	Folder    string // 目录前缀过滤（folder_path = Folder 或 Folder/ 前缀子目录）
}

// MediaRef 时间轴条目（契约 MediaRef 轻量结构）。
type MediaRef struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Filename    string    `json:"filename"`
	FolderPath  string    `json:"folder_path"`
	TakenAt     time.Time `json:"taken_at"`
	Width       *int      `json:"width,omitempty"`
	Height      *int      `json:"height,omitempty"`
	Duration    *float64  `json:"duration,omitempty"`
	Codec       *string   `json:"codec,omitempty"`
	Is360       bool      `json:"is_360"`
	Place       *string   `json:"place,omitempty"`
	Rating      *int      `json:"rating,omitempty"`
	ThumbnailSM *string   `json:"thumbnail_sm,omitempty"`
	ThumbnailMD *string   `json:"thumbnail_md,omitempty"`
	ThumbnailLG *string   `json:"thumbnail_lg,omitempty"`
	Score       *float64  `json:"score,omitempty"` // 搜索相关度（仅 /search 带 q 时挂载）
	// SemanticOnly 仅由语义召回命中（未命中任何文本 token 条件）。
	// 仅 /search 且启用语义召回时挂载；前端据此标注"语义匹配"。
	SemanticOnly bool `json:"semantic_only,omitempty"`
}

// Bucket 时间桶（钻取计数）。
type Bucket struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// ListResult 响应（对齐 API §3）。
type ListResult struct {
	Items      []MediaRef `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Total      int        `json:"total"`
	Buckets    []Bucket   `json:"buckets"`
}

// MediaRef 的查询列与扫描器见 mediaref.go（唯一真源，7 处调用共用）。
// 曾经这里的 listMediaCols 只是"其中一份"，其余 6 份各自裸选可空列而漂移。

// timelineBucketSQL 时间桶聚合。%[1]s=date_trunc 粒度，%[2]s=桶键 to_char 格式，%[3]s=WHERE。
//
// 桶键在 SQL 侧用 to_char 渲染并对 NULL 兜底为 'unknown'（与 histogram.go 的
// /media/date-histogram 同口径）：只改 SELECT 列表不够，taken_at 为 NULL 的行走到
// date_trunc 再扫回 time.Time 同样会报错。
//
// 排序用 min(m.taken_at)：桶区间互不重叠，故跨桶即为时间新→旧；'unknown' 桶的 min 为 NULL，
// 配 NULLS LAST 落到末位（与 histogram.go 用 ASC 让 "unknown" 自然落末位的一致意图）。
// ⚠️ 不能用 `ORDER BY (b = 'unknown')`：Postgres 只允许输出别名以**裸名**出现在 ORDER BY，
// 嵌进表达式会报 `column "b" does not exist`（SQLSTATE 42703，已实测踩到）。
const timelineBucketSQL = `
	SELECT COALESCE(to_char(date_trunc('%[1]s', m.taken_at), '%[2]s'), 'unknown') AS b, count(*)::int
	FROM media m WHERE %[3]s
	GROUP BY 1 ORDER BY min(m.taken_at) DESC NULLS LAST`

// viewTrunc 时间桶粒度 → (date_trunc 粒度, 桶键的 to_char 格式)。
var viewTrunc = map[string]struct{ trunc, toChar string }{
	"year":  {"year", "YYYY"},
	"month": {"month", "YYYY-MM"},
	"day":   {"day", "YYYY-MM-DD"},
}

// buildWhere 组装过滤条件（参数化，防注入）。
func (p *ListParams) buildWhere() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	conds = append(conds, "m.deleted_at IS NULL")
	// 空间作用域：**必须最先追加**，因为 scopeConds 的占位符从 $1 起编号；
	// 其余条件靠 len(args) 续编，故顺序是正确性的一部分，不是风格问题。
	// 谓词本体与安全规则见 scope.go（唯一真源，三个端点共用，避免各写一份而漂移）。
	scopeWhere, scopeArgs := scopeConds(p.Scope)
	args = append(args, scopeArgs...)
	conds = append(conds, scopeWhere...)
	if p.Folder != "" {
		// 目录过滤：精确匹配或子目录前缀（G3 文件夹视图需求）
		args = append(args, p.Folder)
		n := len(args)
		conds = append(conds, fmt.Sprintf("(m.folder_path = $%d OR m.folder_path LIKE $%d || '/%%')", n, n))
	}
	switch p.Type {
	case "photo", "video":
		add("m.type = $%d AND m.is_360 = false", p.Type)
	case "360":
		conds = append(conds, "m.is_360 = true")
	}
	if p.Tag != "" {
		add(`EXISTS(SELECT 1 FROM media_tags mt JOIN tags t ON t.id = mt.tag_id
			WHERE mt.media_id = m.id AND t.name ILIKE '%%' || $%d || '%%')`, p.Tag)
	}
	if p.Person != "" {
		add(`EXISTS(SELECT 1 FROM faces f JOIN people pe ON pe.id = f.person_id
			WHERE f.media_id = m.id AND pe.name ILIKE '%%' || $%d || '%%')`, p.Person)
	}
	if p.Place != "" {
		add("m.place ILIKE '%%' || $%d || '%%'", p.Place)
	}
	if p.Favorites {
		// 收藏模型：favorites 相册成员（DDL 无 is_favorite 字段，决策记录见报告）
		conds = append(conds, `EXISTS(SELECT 1 FROM album_items ai JOIN albums a ON a.id = ai.album_id
			WHERE ai.media_id = m.id AND a.type = 'favorites')`)
	}
	if p.Date != "" {
		if start, end, ok := parseDateRange(p.Date); ok {
			args = append(args, start, end)
			conds = append(conds, fmt.Sprintf("m.taken_at >= $%d AND m.taken_at < $%d", len(args)-1, len(args)))
		}
	}
	return strings.Join(conds, " AND "), args
}

// parseDateRange 解析 date 参数为 [start, end)。
func parseDateRange(d string) (time.Time, time.Time, bool) {
	for _, f := range []string{"2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(f, d); err == nil {
			var end time.Time
			switch f {
			case "2006-01-02":
				end = t.AddDate(0, 0, 1)
			case "2006-01":
				end = t.AddDate(0, 1, 0)
			default:
				end = t.AddDate(1, 0, 0)
			}
			return t, end, true
		}
	}
	return time.Time{}, time.Time{}, false
}

func encodeCursor(t time.Time, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodeCursor(s string) (time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", err
	}
	ts, id, ok := strings.Cut(string(b), "|")
	if !ok {
		return time.Time{}, "", errors.New("游标格式错误")
	}
	t, err := time.Parse(time.RFC3339Nano, ts)
	return t, id, err
}

// List 时间轴查询：过滤 + 复合游标分页 + 时间桶聚合。
func (s *Store) List(ctx context.Context, p ListParams) (*ListResult, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}
	where, args := p.buildWhere()

	// 复合游标：(taken_at, id) 降序
	if p.Cursor != "" {
		t, id, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, errors.New("无效游标")
		}
		args = append(args, t, id)
		where += fmt.Sprintf(" AND (m.taken_at, m.id) < ($%d, $%d)", len(args)-1, len(args))
	}

	// 总数（不含游标条件，供 UI 显示；游标条件下 total 为估算意义不大，给过滤全集）
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM media m WHERE `+stripCursor(where, len(args)), args[:len(args)-cursorArgs(p)]...).Scan(&total); err != nil {
		return nil, err
	}

	args = append(args, p.Limit+1)
	// 列与扫描器见 mediaref.go（唯一真源）。
	// NULLS LAST：Postgres 的 DESC 默认 NULLS FIRST，会把无拍摄时间的媒体顶到时间轴最前，
	// 而其桶又落在 "unknown"（末位）——显式 NULLS LAST 让两者一致；现有数据无 NULL，故零影响。
	rows, err := s.Pool.Query(ctx, `SELECT `+MediaRefColumns+`
		FROM media m WHERE `+where+`
		ORDER BY m.taken_at DESC NULLS LAST, m.id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &ListResult{Items: []MediaRef{}, Buckets: []Bucket{}, Total: total}
	for rows.Next() {
		// taken_at 可空由扫描器在边界承接（NULL → 零值），见 mediaref.go 的取舍说明。
		it, err := ScanMediaRef(rows)
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Items) > p.Limit {
		last := res.Items[p.Limit-1]
		res.NextCursor = encodeCursor(last.TakenAt, last.ID)
		res.Items = res.Items[:p.Limit]
	}

	// 时间桶（view=year|month|day 时聚合；all 返回空数组）。SQL 与取舍说明见 timelineBucketSQL。
	if g, ok := viewTrunc[p.View]; ok {
		bwhere, bargs := p.buildWhere() // 桶聚合不带游标
		brows, err := s.Pool.Query(ctx, fmt.Sprintf(timelineBucketSQL, g.trunc, g.toChar, bwhere), bargs...)
		if err != nil {
			return nil, err
		}
		defer brows.Close()
		for brows.Next() {
			var key string
			var n int
			if err := brows.Scan(&key, &n); err != nil {
				return nil, err
			}
			res.Buckets = append(res.Buckets, Bucket{Key: key, Count: n})
		}
	}
	return res, nil
}

func cursorArgs(p ListParams) int {
	if p.Cursor != "" {
		return 2
	}
	return 0
}

func stripCursor(where string, _ int) string {
	if i := strings.LastIndex(where, " AND (m.taken_at, m.id) < "); i != -1 {
		return where[:i]
	}
	return where
}
