package geo

import (
	"context"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Cluster 一个聚合簇（质心 + 计数）。
type Cluster struct {
	Lng   float64 `json:"lng"`
	Lat   float64 `json:"lat"`
	Count int     `json:"count"`
}

// Store 空间数据访问。
type Store struct {
	Pool *pgxpool.Pool
	// Table 数据来源表（原型用 map_proto_points；正式版切 media.gps）
	Table string
}

// GridSize 按 zoom 计算聚合网格边长（度）：z0=180°，逐级减半。
func GridSize(zoom int) float64 {
	if zoom < 0 {
		zoom = 0
	}
	g := 180.0 / math.Pow(2, float64(zoom))
	if g < 0.0005 {
		g = 0.0005
	}
	return g
}

// Clusters bbox + zoom 网格聚合（supercluster 服务端等效，AC-14）。
// provider=amap 时返回坐标转换为 GCJ-02（应用层转换，AC-15）；osm/其他返回 WGS-84。
func (s *Store) Clusters(ctx context.Context, minLng, minLat, maxLng, maxLat float64, zoom int, provider string) ([]Cluster, error) {
	g := GridSize(zoom)
	rows, err := s.Pool.Query(ctx, `
		SELECT ST_X(ST_Centroid(ST_Collect(geom))),
		       ST_Y(ST_Centroid(ST_Collect(geom))),
		       count(*)::int
		FROM `+s.Table+`
		WHERE geom && ST_MakeEnvelope($1,$2,$3,$4,4326)
		GROUP BY ST_SnapToGrid(geom, $5)
		ORDER BY count(*) DESC`,
		minLng, minLat, maxLng, maxLat, g)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Cluster
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.Lng, &c.Lat, &c.Count); err != nil {
			return nil, err
		}
		if provider == "amap" {
			c.Lng, c.Lat = WGS84ToGCJ02(c.Lng, c.Lat)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
