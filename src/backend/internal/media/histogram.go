package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// 日期密度直方图（Job000005）：GET /media/date-histogram?granularity=year|month。
// 作用域与 timeline 完全一致：谓词取自 scopeConds（唯一真源，见 scope.go），
// 均排除软删；space 缺省=本人个人空间，绝不是"全部"。

// ErrInvalidGranularity 粒度非法。
var ErrInvalidGranularity = errors.New("granularity 仅支持 year|month")

// HistogramBucket 直方图桶（taken_at 为 NULL 的媒体归入 "unknown" 桶）。
type HistogramBucket struct {
	Bucket string `json:"bucket"`
	Count  int    `json:"count"`
}

var histogramTrunc = map[string]struct{ trunc, fmt string }{
	"year":  {"year", "YYYY"},
	"month": {"month", "YYYY-MM"},
}

// DateHistogram 按 taken_at 粒度聚合计数（升序；"unknown" 桶字典序自然落末位）。
func (s *Store) DateHistogram(ctx context.Context, scope MediaScope, granularity string) ([]HistogramBucket, error) {
	g, ok := histogramTrunc[granularity]
	if !ok {
		return nil, ErrInvalidGranularity
	}
	conds := []string{"m.deleted_at IS NULL"}
	// 作用域谓词最先追加，占位符从 $1 起编号（见 scopeConds 的调用约定）
	scopeWhere, args := scopeConds(scope)
	conds = append(conds, scopeWhere...)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT COALESCE(to_char(date_trunc('%s', m.taken_at), '%s'), 'unknown') AS bucket, count(*)::int
		FROM media m WHERE %s
		GROUP BY 1 ORDER BY 1 ASC`, g.trunc, g.fmt, strings.Join(conds, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	buckets := []HistogramBucket{}
	for rows.Next() {
		var b HistogramBucket
		if err := rows.Scan(&b.Bucket, &b.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, b)
	}
	return buckets, rows.Err()
}
