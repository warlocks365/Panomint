package geo

// 地图模式数据访问层（Job000009）。
//
// 坐标系约定：media.gps 库内一律 WGS-84；对外输出时按 provider 决定是否转 GCJ-02
// （provider=amap 转换，osm/其他原样返回），与 Clusters（cluster.go）保持同一约定。

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BBox 地理边界框（WGS-84 经纬度，度）。
type BBox struct {
	MinLng, MinLat, MaxLng, MaxLat float64
}

// Valid 边界合法性校验：经纬度在合理区间且左下 < 右上。
func (b BBox) Valid() bool {
	if b.MinLng < -180 || b.MaxLng > 180 || b.MinLat < -90 || b.MaxLat > 90 {
		return false
	}
	return b.MinLng < b.MaxLng && b.MinLat < b.MaxLat
}

// BBoxFromGCJ02 视口 bounds 的坐标系还原（底图为高德时必需）。
//
// 背景：前端 map.getBounds() 拿到的是**地图显示坐标系**，底图为高德即 GCJ-02；
// 而 media.gps 库内一律 WGS-84。直接拿 GCJ-02 的 bbox 去查 WGS-84 会产生数百米偏移，
// 表现为"明明在视野里的点查不出来"。故查询前把矩形两角反变换回 WGS-84 再重组。
// 形变误差（矩形→非矩形）远小于 GCJ-02 偏移量级，且前端请求时通常已外扩视口。
func BBoxFromGCJ02(b BBox) BBox {
	lngA, latA := GCJ02ToWGS84(b.MinLng, b.MinLat)
	lngB, latB := GCJ02ToWGS84(b.MaxLng, b.MaxLat)
	return BBox{
		MinLng: math.Min(lngA, lngB),
		MaxLng: math.Max(lngA, lngB),
		MinLat: math.Min(latA, latB),
		MaxLat: math.Max(latA, latB),
	}
}

// MediaPoint 地图上的单个媒体条目（用于点击簇后展开列表）。
type MediaPoint struct {
	ID       string     `json:"id"`
	Filename string     `json:"filename"`
	Type     string     `json:"type"`
	Is360    bool       `json:"is_360"`
	Place    string     `json:"place"`
	TakenAt  *time.Time `json:"taken_at"`
	Lng      float64    `json:"lng"`
	Lat      float64    `json:"lat"`
}

// Bucket 时间直方图桶（Job000009 扩展：四类媒体分类计数，count=四类之和保持向后兼容）。
type Bucket struct {
	Bucket      string `json:"bucket"`
	Count       int    `json:"count"`        // 四类之和（向后兼容旧前端）
	Photos      int    `json:"photos"`       // type=photo && !is_360
	Videos      int    `json:"videos"`       // type=video && !is_360
	PanoPhotos  int    `json:"pano_photos"`  // type=photo && is_360
	PanoVideos  int    `json:"pano_videos"`  // type=video && is_360
}

// MediaStore 媒体库空间数据访问（数据源：media.gps）。
type MediaStore struct {
	Pool *pgxpool.Pool
}

// ErrInvalidGranularity 直方图粒度非法。
var ErrInvalidGranularity = errors.New("granularity 仅支持 year|month|day")

var mediaHistogramTrunc = map[string]struct{ trunc, layout string }{
	"year":  {"year", "YYYY"},
	"month": {"month", "YYYY-MM"},
	"day":   {"day", "YYYY-MM-DD"},
}

// baseCond 公共过滤条件：有 GPS、未软删、落在 bbox 内；args 前缀为 bbox 四元组。
func baseCond(b BBox) ([]string, []any) {
	return []string{
			"gps IS NOT NULL",
			"deleted_at IS NULL",
			"gps && ST_MakeEnvelope($1,$2,$3,$4,4326)",
		},
		[]any{b.MinLng, b.MinLat, b.MaxLng, b.MaxLat}
}

// appendTimeRange 追加可选时间范围条件（taken_at 区间，闭区间语义：>= from AND <= to）。
func appendTimeRange(conds []string, args []any, from, to *time.Time) ([]string, []any) {
	if from != nil {
		args = append(args, *from)
		conds = append(conds, fmt.Sprintf("taken_at >= $%d", len(args)))
	}
	if to != nil {
		args = append(args, *to)
		conds = append(conds, fmt.Sprintf("taken_at <= $%d", len(args)))
	}
	return conds, args
}

// convert 按 provider 决定是否转 GCJ-02（详见 WGS84ToGCJ02；境外坐标原样返回）。
func convert(lng, lat float64, provider string) (float64, float64) {
	if provider == "amap" {
		return WGS84ToGCJ02(lng, lat)
	}
	return lng, lat
}

// Clusters 按 bbox + zoom 做网格聚合，支持时间范围过滤（时间轴 → 地图 联动）。
// 返回质心 + 计数，坐标按 provider 转换。
func (s *MediaStore) Clusters(ctx context.Context, b BBox, zoom int, provider string, from, to *time.Time) ([]Cluster, error) {
	conds, args := baseCond(b)
	conds, args = appendTimeRange(conds, args, from, to)
	args = append(args, GridSize(zoom))
	grid := fmt.Sprintf("$%d", len(args))

	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT ST_X(ST_Centroid(ST_Collect(gps))),
		       ST_Y(ST_Centroid(ST_Collect(gps))),
		       count(*)::int
		FROM media
		WHERE %s
		GROUP BY ST_SnapToGrid(gps, %s)
		ORDER BY count(*) DESC`, strings.Join(conds, " AND "), grid), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Cluster{}
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.Lng, &c.Lat, &c.Count); err != nil {
			return nil, err
		}
		c.Lng, c.Lat = convert(c.Lng, c.Lat, provider)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Items 列出 bbox 内的媒体条目（点击簇 / 框选后展开用），按拍摄时间倒序。
// limit 由调用方收敛（handler 层做上下界校验）。
func (s *MediaStore) Items(ctx context.Context, b BBox, provider string, from, to *time.Time, limit int) ([]MediaPoint, error) {
	conds, args := baseCond(b)
	conds, args = appendTimeRange(conds, args, from, to)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT id::text, filename, type::text,
		       COALESCE(is_360, false), COALESCE(place, ''),
		       taken_at, ST_X(gps), ST_Y(gps)
		FROM media
		WHERE %s
		ORDER BY taken_at DESC NULLS LAST
		LIMIT $%d`, strings.Join(conds, " AND "), len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []MediaPoint{}
	for rows.Next() {
		var p MediaPoint
		if err := rows.Scan(&p.ID, &p.Filename, &p.Type, &p.Is360, &p.Place, &p.TakenAt, &p.Lng, &p.Lat); err != nil {
			return nil, err
		}
		p.Lng, p.Lat = convert(p.Lng, p.Lat, provider)
		out = append(out, p)
	}
	return out, rows.Err()
}

// Histogram bbox 内的时间分布直方图（地图 viewport → 时间轴 联动）。
// taken_at 为空的媒体归入 "unknown" 桶（字典序自然落末位）。
// Job000009：每桶同时返回四类媒体分类计数（照片/视频/全景照片/全景视频），
// 供时间轴实时统计卡片使用，与直方图一次查询返回、天然随 bbox 与粒度联动。
func (s *MediaStore) Histogram(ctx context.Context, b BBox, granularity string) ([]Bucket, error) {
	g, ok := mediaHistogramTrunc[granularity]
	if !ok {
		return nil, ErrInvalidGranularity
	}
	conds, args := baseCond(b)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		SELECT COALESCE(to_char(date_trunc('%s', taken_at), '%s'), 'unknown') AS bucket,
		       count(*)::int,
		       count(*) FILTER (WHERE type='photo' AND NOT COALESCE(is_360,false))::int,
		       count(*) FILTER (WHERE type='video' AND NOT COALESCE(is_360,false))::int,
		       count(*) FILTER (WHERE type='photo' AND COALESCE(is_360,false))::int,
		       count(*) FILTER (WHERE type='video' AND COALESCE(is_360,false))::int
		FROM media
		WHERE %s
		GROUP BY 1 ORDER BY 1 ASC`, g.trunc, g.layout, strings.Join(conds, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Bucket{}
	for rows.Next() {
		var bkt Bucket
		if err := rows.Scan(&bkt.Bucket, &bkt.Count, &bkt.Photos, &bkt.Videos, &bkt.PanoPhotos, &bkt.PanoVideos); err != nil {
			return nil, err
		}
		out = append(out, bkt)
	}
	return out, rows.Err()
}

// ParseTime 解析时间参数：优先 RFC3339，失败回退日期格式（2006-01-02）。
// 空串返回 nil 表示不限制。
func ParseTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return &t, nil
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		return nil, fmt.Errorf("时间格式非法（需 RFC3339 或 YYYY-MM-DD）：%s", v)
	}
	return &t, nil
}
