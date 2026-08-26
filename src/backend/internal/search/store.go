package search

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/media"
)

// ErrInvalidCursor 游标格式非法。
var ErrInvalidCursor = errors.New("无效游标")

// Store 搜索查询执行（召回管道编排：Recaller 插槽 → 结构化过滤 → place 地理降级）。
type Store struct {
	Pool     *pgxpool.Pool
	Recaller Recaller // 语义召回插槽（nil 跳过；MVP 装配 SemanticRecaller 占位）
	Resolver Resolver // place 地理降级解析器（nil 不降级）
}

// Search GET /search 主流程。
func (s *Store) Search(ctx context.Context, p SearchParams) (*SearchResult, error) {
	if p.Limit <= 0 || p.Limit > 200 {
		p.Limit = 50
	}

	// 召回管道：语义召回补充 ID（MVP 占位恒空）
	var extraIDs []string
	if s.Recaller != nil {
		ids, err := s.Recaller.Recall(ctx, p)
		if err != nil {
			return nil, err
		}
		extraIDs = ids
	}

	// 第一轮：place 文本 trgm 匹配
	where, args := buildWhere(p, extraIDs, nil)
	res, err := s.query(ctx, p, where, args)
	if err != nil {
		return nil, err
	}

	// place 地理降级：文本零结果 + 库内有带 GPS 媒体 + 地名可解析 → 5km 半径重查
	if p.Place != "" && res.Total == 0 && s.Resolver != nil {
		hasGPS, err := s.hasGPS(ctx)
		if err != nil {
			return nil, err
		}
		if needGeoFallback(p.Place, res.Total, hasGPS) {
			lon, lat, ok, err := s.Resolver.Resolve(ctx, p.Place)
			if err != nil {
				// 解析器故障（如 Nominatim 网络不可达）不应 500：降级为文本检索零结果
				log.Printf("place 地理解析失败 %q: %v（按不降级继续）", p.Place, err)
			} else if ok {
				center := GeoCenter{Lon: lon, Lat: lat}
				gwhere, gargs := buildWhere(p, extraIDs, &center)
				res, err = s.query(ctx, p, gwhere, gargs)
				if err != nil {
					return nil, err
				}
				res.PlaceFallback = &PlaceFallback{Mode: "geo_radius", Center: center, RadiusM: geoRadiusM}
			}
		}
	}
	return res, nil
}

// needGeoFallback 是否进入地理降级：place 非空 + 文本检索零结果 + 库内有带 GPS 媒体。
func needGeoFallback(place string, total int, hasGPS bool) bool {
	return place != "" && total == 0 && hasGPS
}

// hasGPS 库内是否存在带 GPS 的媒体（降级前提）。
func (s *Store) hasGPS(ctx context.Context) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM media WHERE gps IS NOT NULL AND deleted_at IS NULL)`).Scan(&ok)
	return ok, err
}

// query 执行过滤查询：total（不含游标）+ 复合游标分页（与 timeline.go 同构）。
func (s *Store) query(ctx context.Context, p SearchParams, where string, args []any) (*SearchResult, error) {
	if p.Cursor != "" {
		t, id, err := decodeCursor(p.Cursor)
		if err != nil {
			return nil, ErrInvalidCursor
		}
		args = append(args, t, id)
		where += fmt.Sprintf(" AND (m.taken_at, m.id) < ($%d, $%d)", len(args)-1, len(args))
	}

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM media m WHERE `+stripCursor(where),
		args[:len(args)-cursorArgCount(p)]...).Scan(&total); err != nil {
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

	res := &SearchResult{Items: []media.MediaRef{}, Total: total}
	for rows.Next() {
		var it media.MediaRef
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
	return res, nil
}

// ---- 复合游标（taken_at, id）降序，与 internal/media/timeline.go 同构 ----

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

func cursorArgCount(p SearchParams) int {
	if p.Cursor != "" {
		return 2
	}
	return 0
}

func stripCursor(where string) string {
	if i := strings.LastIndex(where, " AND (m.taken_at, m.id) < "); i != -1 {
		return where[:i]
	}
	return where
}
