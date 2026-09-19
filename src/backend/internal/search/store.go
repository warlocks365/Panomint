package search

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/cursor"
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
			tm, sc, t, id, err := cursor.DecodeScored(p.Cursor)
			if err != nil {
				return nil, ErrInvalidCursor
			}
			csql, cparams := scoredCursorWhere(textMatchExpr, scoreExpr, textOrder, len(args)+1, tm, sc, t, id)
			args = append(args, cparams...)
			cursorWhere = " AND " + csql
		} else {
			t, id, err := cursor.Decode(p.Cursor)
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
	// 显式 NULLS LAST（P2-05）：PG 的 DESC 默认 NULLS FIRST，而上方游标行比较
	// `(m.taken_at, m.id) < ($n,$m)` 遇 NULL 求值为 NULL（即"不成立"）——若 taken_at
	// 为 NULL 的行超过一页，翻页后会被永久排除。与 faces/embed/tags 的 ListPending 对齐。
	orderBy := "m.taken_at DESC NULLS LAST, m.id DESC"
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
	// 列清单走 internal/media 的唯一真源；score / text_matched 是**本端点额外的两列**，
	// 必须排在 MediaRefColumns 之后（扫描目标同序，见下方 Dests 追加）。
	rows, err := s.Pool.Query(ctx, `
		SELECT `+media.MediaRefColumns+`,
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
		var textMatched *bool
		sc := media.NewMediaRefScanner()
		dests := append(sc.Dests(), &sc.Ref.Score, &textMatched)
		if err := rows.Scan(dests...); err != nil {
			return nil, err
		}
		it := sc.Finish()
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
			res.NextCursor = cursor.EncodeScored(!last.SemanticOnly, *last.Score, last.TakenAt, last.ID)
		} else {
			res.NextCursor = cursor.Encode(last.TakenAt, last.ID)
		}
		res.Items = res.Items[:p.Limit]
	}
	return res, nil
}

// 游标编解码见 internal/cursor（唯一真源，含 v3 评分游标与 v2 兼容回退）。
// 本包曾与 audit/query.go、media/timeline.go 各存一份**逐字节相同**的复合游标实现；
// 游标是对外契约，重复实现会让"改一处忘一处"变成静默的翻页错位（跨页丢行/重复行）。
