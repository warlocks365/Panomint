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
	Space     string // personal|shared
	View      string // year|month|day|all
	Date      string // YYYY / YYYY-MM / YYYY-MM-DD
	Type      string // photo|video|360
	Favorites bool
	Tag       string
	Person    string
	Place     string
	Cursor    string
	Limit     int
	OwnerID   string
	Folder    string // 目录前缀过滤（folder_path = Folder 或 Folder/ 前缀子目录）
}

// MediaRef 时间轴条目（契约 MediaRef 轻量结构）。
type MediaRef struct {
	ID          string     `json:"id"`
	Type        string     `json:"type"`
	Filename    string     `json:"filename"`
	FolderPath  string     `json:"folder_path"`
	TakenAt     time.Time  `json:"taken_at"`
	Width       *int       `json:"width,omitempty"`
	Height      *int       `json:"height,omitempty"`
	Duration    *float64   `json:"duration,omitempty"`
	Codec       *string    `json:"codec,omitempty"`
	Is360       bool       `json:"is_360"`
	Place       *string    `json:"place,omitempty"`
	Rating      *int       `json:"rating,omitempty"`
	ThumbnailSM *string    `json:"thumbnail_sm,omitempty"`
	ThumbnailMD *string    `json:"thumbnail_md,omitempty"`
	ThumbnailLG *string    `json:"thumbnail_lg,omitempty"`
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

var viewTrunc = map[string]string{"year": "year", "month": "month", "day": "day"}
var viewKeyFmt = map[string]string{"year": "2006", "month": "2006-01", "day": "2006-01-02"}

// buildWhere 组装过滤条件（参数化，防注入）。
func (p *ListParams) buildWhere() (string, []any) {
	var conds []string
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, fmt.Sprintf(cond, len(args)))
	}

	conds = append(conds, "m.deleted_at IS NULL")
	if p.OwnerID != "" {
		add("m.owner_id = $%d", p.OwnerID)
	}
	if p.Space != "" {
		add("m.space = $%d", p.Space)
	}
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
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,
		       m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg
		FROM media m WHERE `+where+`
		ORDER BY m.taken_at DESC, m.id DESC
		LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := &ListResult{Items: []MediaRef{}, Buckets: []Bucket{}, Total: total}
	for rows.Next() {
		var it MediaRef
		if err := rows.Scan(&it.ID, &it.Type, &it.Filename, &it.FolderPath, &it.TakenAt, &it.Width, &it.Height,
			&it.Duration, &it.Codec, &it.Is360, &it.Place, &it.Rating,
			&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG); err != nil {
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

	// 时间桶（view=year|month|day 时聚合；all 返回空数组）
	if trunc, ok := viewTrunc[p.View]; ok {
		bwhere, bargs := p.buildWhere() // 桶聚合不带游标
		brows, err := s.Pool.Query(ctx, fmt.Sprintf(`
			SELECT date_trunc('%s', m.taken_at) AS b, count(*)::int
			FROM media m WHERE %s
			GROUP BY b ORDER BY b DESC`, trunc, bwhere), bargs...)
		if err != nil {
			return nil, err
		}
		defer brows.Close()
		for brows.Next() {
			var t time.Time
			var n int
			if err := brows.Scan(&t, &n); err != nil {
				return nil, err
			}
			res.Buckets = append(res.Buckets, Bucket{Key: t.Format(viewKeyFmt[p.View]), Count: n})
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
