package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolver 地名 → 坐标解析抽象（place 地理降级用）。
type Resolver interface {
	// Resolve 解析地名为 WGS-84 坐标；ok=false 表示无法解析（不降级，按文本零结果返回）。
	Resolve(ctx context.Context, query string) (lon, lat float64, ok bool, err error)
}

// Geocoder 正向地理编码提供者（契约 §7 /map/search：中国高德 / 国际 Nominatim；
// 现有代码库无服务端实现，本期先落地 Nominatim，高德待 system_map_config 密钥装配后补充）。
type Geocoder interface {
	Geocode(ctx context.Context, query string) (lon, lat float64, ok bool, err error)
}

// CachedResolver geo_cache 缓存包装（key "fwd:<query>"，TTL 30 天；Pool 为 nil 时跳过缓存，便于测试）。
type CachedResolver struct {
	Pool     *pgxpool.Pool
	Provider Geocoder
	TTL      time.Duration // 默认 30 天
}

type cachedGeo struct {
	Lon float64 `json:"lon"`
	Lat float64 `json:"lat"`
}

// Resolve 先查 geo_cache，未命中/过期走 Provider 并回填缓存。
func (r *CachedResolver) Resolve(ctx context.Context, query string) (float64, float64, bool, error) {
	key := "fwd:" + query
	if r.Pool != nil {
		var raw []byte
		err := r.Pool.QueryRow(ctx,
			`SELECT result FROM geo_cache WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
			key).Scan(&raw)
		if err == nil {
			var g cachedGeo
			if json.Unmarshal(raw, &g) == nil {
				return g.Lon, g.Lat, true, nil
			}
		}
	}
	if r.Provider == nil {
		return 0, 0, false, nil
	}
	lon, lat, ok, err := r.Provider.Geocode(ctx, query)
	if err != nil || !ok {
		return 0, 0, ok, err
	}
	if r.Pool != nil {
		ttl := r.TTL
		if ttl <= 0 {
			ttl = 30 * 24 * time.Hour
		}
		raw, _ := json.Marshal(cachedGeo{Lon: lon, Lat: lat})
		_, _ = r.Pool.Exec(ctx, `
			INSERT INTO geo_cache (key, provider, result, expires_at)
			VALUES ($1, 'nominatim', $2, now() + $3::interval)
			ON CONFLICT (key) DO UPDATE SET result = EXCLUDED.result, expires_at = EXCLUDED.expires_at`,
			key, raw, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	}
	return lon, lat, true, nil
}

// NominatimGeocoder OSM Nominatim 正向地理编码（国际地名；需外网可达）。
type NominatimGeocoder struct {
	BaseURL string       // 默认 https://nominatim.openstreetmap.org
	Client  *http.Client // 默认 10s 超时
}

// Geocode 调用 /search?q=&format=json&limit=1 取首个候选。
func (g NominatimGeocoder) Geocode(ctx context.Context, query string) (float64, float64, bool, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://nominatim.openstreetmap.org"
	}
	cli := g.Client
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/search?q="+url.QueryEscape(query)+"&format=json&limit=1", nil)
	if err != nil {
		return 0, 0, false, err
	}
	req.Header.Set("User-Agent", "PanoAlbum/1.0 (place search fallback)")
	resp, err := cli.Do(req)
	if err != nil {
		return 0, 0, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, false, fmt.Errorf("nominatim 状态码 %d", resp.StatusCode)
	}
	var cands []struct {
		Lon string `json:"lon"`
		Lat string `json:"lat"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cands); err != nil {
		return 0, 0, false, err
	}
	if len(cands) == 0 {
		return 0, 0, false, nil
	}
	lon, err1 := strconv.ParseFloat(cands[0].Lon, 64)
	lat, err2 := strconv.ParseFloat(cands[0].Lat, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false, fmt.Errorf("nominatim 坐标解析失败")
	}
	return lon, lat, true, nil
}
