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
	where, scoreExpr, textMatchExpr, args, whereN := buildWhere(p, hits, nil)
	res, err := s.query(ctx, p, where, scoreExpr, textMatchExpr, args, whereN)
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
				gwhere, gscore, gtextMatch, gargs, gwhereN := buildWhere(p, hits, &center)
				res, err = s.query(ctx, p, gwhere, gscore, gtextMatch, gargs, gwhereN)
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

// scoredOrderBy 评分模式的 ORDER BY 子句。
//
// hasTextMatch 为 true 时以**显式排序键** `text_matched DESC` 保证"文本命中一律排在仅语义命中之前"，
// 而不是依赖 score 的数值大小关系：语义项（10×相似度）可能高于一条弱 trgm 文本命中
// （人像相似度 0.76 → 7.6 分，弱 trgm 命中可能只有 0.3 分），只比 score 会把仅语义结果插到前面。
// 同一组内仍按 score DESC 排序，故"文本命中内部""仅语义内部"的相对次序与原先一致。
// text_matched 不加 NULLS LAST：它由 tokConds 的 COALESCE 保证非 NULL，而游标行比较无法表达
// NULLS LAST，排序键与游标必须严格同序同 NULLS 语义（TestScoredCursorShapeMatchesOrderKeys 兜底）。
func scoredOrderBy(hasTextMatch bool) string {
	if hasTextMatch {
		return "text_matched DESC, score DESC NULLS LAST, m.taken_at DESC, m.id DESC"
	}
	return "score DESC NULLS LAST, m.taken_at DESC, m.id DESC"
}

// scoredCursorWhere 生成评分模式的游标条件（不含前导 " AND "）与按序待追加的参数。
//
// ⚠️ 键序必须与 scoredOrderBy 完全一致，否则跨页会重复或漏行：
// 有文本命中 → (text_matched, score, taken_at, id) 四元组；否则 → (score, taken_at, id) 三元组。
// baseArg 为下一个可用占位符序号（游标参数位于 whereN 之后，不参与 count(*)）。
func scoredCursorWhere(textMatchExpr, scoreExpr string, hasTextMatch bool, baseArg int, tm bool, sc float64, t time.Time, id string) (string, []any) {
	if hasTextMatch {
		return fmt.Sprintf("((%s), (%s), m.taken_at, m.id) < ($%d::bool, $%d::float8, $%d::timestamptz, $%d::uuid)",
				textMatchExpr, scoreExpr, baseArg, baseArg+1, baseArg+2, baseArg+3),
			[]any{tm, sc, t, id}
	}
	return fmt.Sprintf("((%s), m.taken_at, m.id) < ($%d::float8, $%d::timestamptz, $%d::uuid)",
			scoreExpr, baseArg, baseArg+1, baseArg+2),
		[]any{sc, t, id}
}

// query 执行过滤查询：total（不含游标）+ 复合游标分页（与 timeline.go 同构）。
// scoreExpr 非空（q 带关键词或有语义命中）时走相关度模式：排序 text_matched DESC, score DESC,
// taken_at DESC, id DESC（textMatchExpr 为空时退化为 score DESC, taken_at DESC, id DESC），
// 游标为 v3 四元组 (text_matched, score, taken_at, id)；否则保持原 (taken_at, id) 行为。
//
// whereN：WHERE 实际引用的参数个数。args 中位于 whereN 之后的参数只被评分表达式引用
// （如语义召回的 ids/sims 数组），count(*) 查询不能接收它们。
// textMatchExpr 非空时，查询会额外返回每行的"是否命中文本条件"，
// 用于标记 SemanticOnly（仅语义召回命中）——前端据此标注"语义匹配"；
// 该表达式同时充当排序键 text_matched，故游标必须一并携带它，否则跨页会重复/漏掉行。
func (s *Store) query(ctx context.Context, p SearchParams, where, scoreExpr, textMatchExpr string, args []any, whereN int) (*SearchResult, error) {
	scored := scoreExpr != ""
	textOrder := scored && textMatchExpr != ""

	// 游标条件独立于 where 拼装（total 统计不含游标，避免字符串剥离的脆弱性）
	cursorWhere := ""
	if p.Cursor != "" {
		if scored {
			tm, sc, t, id, err := decodeScoredCursor(p.Cursor)
			if err != nil {
				return nil, ErrInvalidCursor
			}
			csql, cparams := scoredCursorWhere(textMatchExpr, scoreExpr, textOrder, len(args)+1, tm, sc, t, id)
			args = append(args, cparams...)
			cursorWhere = " AND " + csql
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
	selectTextMatch := "NULL::bool"
	orderBy := "m.taken_at DESC, m.id DESC"
	if scored {
		selectScore = "(" + scoreExpr + ")"
		if textMatchExpr != "" {
			selectTextMatch = "(" + textMatchExpr + ")"
		}
		// 评分表达式已做 COALESCE 防 NULL；NULLS LAST 的兜底在 scoredOrderBy 内
		// （Postgres 的 DESC 默认 NULLS FIRST，会把评分为 NULL 的无关项排到最前，Job000010 实测踩到）。
		orderBy = scoredOrderBy(textMatchExpr != "")
	}
	args = append(args, p.Limit+1)
	rows, err := s.Pool.Query(ctx, `
		SELECT m.id, m.type, m.filename, m.folder_path, m.taken_at, m.width, m.height, m.duration,
		       m.codec, m.is_360, m.place, m.rating, m.thumbnail_sm, m.thumbnail_md, m.thumbnail_lg,
		       `+selectScore+` AS score,
		       `+selectTextMatch+` AS text_matched
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
		var textMatched *bool
		if err := rows.Scan(&it.ID, &it.Type, &it.Filename, &it.FolderPath, &it.TakenAt, &it.Width, &it.Height,
			&it.Duration, &it.Codec, &it.Is360, &it.Place, &it.Rating,
			&it.ThumbnailSM, &it.ThumbnailMD, &it.ThumbnailLG, &it.Score, &textMatched); err != nil {
			return nil, err
		}
		// text_matched 为 false 且本行确实在结果集中 → 只能是语义召回带进来的
		it.SemanticOnly = textMatched != nil && !*textMatched
		res.Items = append(res.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(res.Items) > p.Limit {
		last := res.Items[p.Limit-1]
		if scored && last.Score != nil {
			// text_matched 与 SemanticOnly 互补（见上方赋值），游标需带上分组键才能续排
			res.NextCursor = encodeScoredCursor(!last.SemanticOnly, *last.Score, last.TakenAt, last.ID)
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

// ---- v3 评分游标（text_matched, score, taken_at, id）降序（与 scoredOrderBy 一一对应）----
//
// v3 在 v2（score, taken_at, id）之上补了分组键 text_matched：
// 排序键多了一列后，若游标仍只带三元组，跨页比较会与 ORDER BY 不一致
// （仅语义行可能被重复返回或漏掉）。前缀 v1/v2/v3 明确版本化隔离。

func encodeScoredCursor(textMatched bool, score float64, t time.Time, id string) string {
	tm := "0"
	if textMatched {
		tm = "1"
	}
	return base64.URLEncoding.EncodeToString([]byte(fmt.Sprintf("v3|%s|%g|%s|%s",
		tm, score, t.UTC().Format(time.RFC3339Nano), id)))
}

// decodeScoredCursor 解析评分游标，返回 (是否文本命中, score, taken_at, id)。
// 兼容 v2 三元组：旧游标没带分组键，按 text_matched=true 处理（仅在升级瞬间的跨版本续页可见，
// 最坏情况是重复若干"仅语义"行，不会漏行、不会报错）。
func decodeScoredCursor(s string) (bool, float64, time.Time, string, error) {
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return false, 0, time.Time{}, "", err
	}
	parts := strings.SplitN(string(b), "|", 5)
	switch {
	case len(parts) == 5 && parts[0] == "v3":
		sc, err := strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标 score 非法")
		}
		t, err := time.Parse(time.RFC3339Nano, parts[3])
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标时间非法")
		}
		return parts[1] == "1", sc, t, parts[4], nil
	case len(parts) == 4 && parts[0] == "v2":
		sc, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标 score 非法")
		}
		t, err := time.Parse(time.RFC3339Nano, parts[2])
		if err != nil {
			return false, 0, time.Time{}, "", errors.New("评分游标时间非法")
		}
		return true, sc, t, parts[3], nil
	}
	return false, 0, time.Time{}, "", errors.New("评分游标格式错误（需 v3 前缀四元组）")
}
