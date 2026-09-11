package search

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"strconv"
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

	// 召回管道：语义召回命中（ID + 相似度）。既并入候选集，也参与打分。
	var hits []RecallHit
	if s.Recaller != nil {
		h, err := s.Recaller.Recall(ctx, p)
		if err != nil {
			return nil, err
		}
		hits = h
	}

	// 第一轮：place 文本 trgm 匹配
	where, scoreExpr, args, whereN := buildWhere(p, hits, nil)
	res, err := s.query(ctx, p, where, scoreExpr, args, whereN)
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
				gwhere, gscore, gargs, gwhereN := buildWhere(p, hits, &center)
				res, err = s.query(ctx, p, gwhere, gscore, gargs, gwhereN)
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
// scoreExpr 非空（q 带关键词或有语义命中）时走相关度模式：排序 score DESC, taken_at DESC, id DESC，
// 游标为 v2 三元组 (score, taken_at, id)；否则保持原 (taken_at, id) 行为。
//
// whereN：WHERE 实际引用的参数个数。args 中位于 whereN 之后的参数只被评分表达式引用
// （如语义召回的 ids/sims 数组），count(*) 查询不能接收它们。
func (s *Store) query(ctx context.Context, p SearchParams, where, scoreExpr string, args []any, whereN int) (*SearchResult, error) {
	scored := scoreExpr != ""

	// 游标条件独立于 where 拼装（total 统计不含游标，避免字符串剥离的脆弱性）
	cursorWhere := ""
	if p.Cursor != "" {
		if scored {
			sc, t, id, err := decodeScoredCursor(p.Cursor)
			if err != nil {
				return nil, ErrInvalidCursor
			}
			args = append(args, sc, t, id)
			cursorWhere = fmt.Sprintf(" AND ((%s), m.taken_at, m.id) < ($%d::float8, $%d::timestamptz, $%d::uuid)",
				scoreExpr, len(args)-2, len(args)-1, len(args))
		} else {
			t, id, err := decodeCursor(p.Cursor)
			if err != nil {
				return nil, ErrInvalidCursor
			}
			args = append(args, t, id)
			cursorWhere = fmt.Sprintf(" AND (m.taken_at, m.id) < ($%d, $%d)", len(args)-1, len(args))
		}
	}

	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM media m WHERE `+where,
		args[:whereN]...).Scan(&total); err != nil {
		return nil, err
	}

	selectScore := "NULL::float8"
	orderBy := "m.taken_at DESC, m.id DESC"
	if scored {
		selectScore = "(" + scoreExpr + ")"
		// NULLS LAST：Postgres 的 DESC 默认 NULLS FIRST，若评分为 NULL 会把无关项排到最前。
		// 评分表达式已做 COALESCE 防 NULL，这里再兜一层（Job000010 语义召回并入后实测踩到）。
		orderBy = "score DESC NULLS LAST, m.taken_at DESC, m.id DESC"
	}
	args = append(args, p.Limit+1)
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,
		       m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg,
		       `+selectScore+` AS score
		FROM media m WHERE `+where+cursorWhere+`
		ORDER BY `+orderBy+`
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
			&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG, &it.Score); err != nil {
			return nil, err
		}
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Items) > p.Limit {
		last := res.Items[p.Limit-1]
		if scored && last.Score != nil {
			res.NextCursor = encodeScoredCursor(*last.Score, last.TakenAt, last.ID)
		} else {
			res.NextCursor = encodeCursor(last.TakenAt, last.ID)
		}
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

// ---- v2 评分游标（score, taken_at, id）降序（Job000005；前缀 v2 与旧格式明确版本化隔离）----

func encodeScoredCursor(score float64, t time.Time, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("v2|%g|%s|%s",
		score, t.UTC().Format(time.RFC3339Nano), id)))
}

func decodeScoredCursor(s string) (float64, time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return 0, time.Time{}, "", err
	}
	parts := strings.SplitN(string(b), "|", 4)
	if len(parts) != 4 || parts[0] != "v2" {
		return 0, time.Time{}, "", errors.New("评分游标格式错误（需 v2 前缀三元组）")
	}
	sc, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, time.Time{}, "", err
	}
	t, err := time.Parse(time.RFC3339Nano, parts[2])
	if err != nil {
		return 0, time.Time{}, "", err
	}
	return sc, t, parts[3], nil
}
