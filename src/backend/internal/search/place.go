package search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Resolver 地名 → 坐标解析抽象（place 地理降级用）。
type Resolver interface {
	// Resolve 解析地名为 WGS-84 坐标；ok=false 表示无法解析（不降级，按文本零结果返回）。
	Resolve(ctx context.Context, query string) (lon, lat float64, ok bool, err error)
}

// Geocoder 正向地理编码提供者（契约 §7 /map/search：中国高德 / 国际 Nominatim）。
// Nominatim 实现见本文件；高德实现见 internal/geo/AmapForwardGeocoder（v3/geocode/geo），
// 由 /map/search 编排层按查询词与 system_map_config 自动选源。
type Geocoder interface {
	Geocode(ctx context.Context, query string) (lon, lat float64, ok bool, err error)
}

// CachedResolver geo_cache 缓存包装（key "fwd:<query>"，TTL 30 天；Pool 为 nil 时跳过缓存，便于测试）。
// 该 key 命名空间只存**单结果** {lon,lat}，与 /map/search 的多候选 key
// "fwd:<provider>:<query>"（internal/geo/MapSearchCacheKey）不同，互不覆盖。
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

// Place 正向地理编码候选（契约 §7 GET /map/search 用）。
// 坐标为 Nominatim 原生 WGS-84；坐标系转换由调用方按底图 provider 决定。
type Place struct {
	Name string
	Lon  float64
	Lat  float64
	// Address 详细地址（Job000143）：Nominatim 的 display_name 本身就是完整
	// 地址（如 "西湖, 杭州市, 浙江省, 中国"），与 Name（短名）语义不同，故分列。
	// 上游无 display_name 时为空。
	Address string
}

const (
	// nominatimUserAgent Nominatim 使用条款要求携带可识别的 UA（含来源说明）。
	nominatimUserAgent = "PanoAlbum/1.0 (place geocoding; self-hosted album)"
	// nominatimMinInterval 条款要求单应用每秒最多 1 次请求。
	nominatimMinInterval = time.Second
	// maxPlaceSearchLimit 单次返回候选数上限（防滥用，也避免上游拒绝）。
	maxPlaceSearchLimit = 20
	// maxNominatimBody 响应体读取上限（1MB），防止异常上游拖垮进程。
	maxNominatimBody = 1 << 20
)

// nominatimLimiter 包级限速器：条款按「应用」而非实例限速，故跨实例共享，
// 保证 /map/search 与搜索 place 降级两条路径合计不超过 1 req/s。
var nominatimLimiter = newMinIntervalLimiter(nominatimMinInterval)

// disableNominatimRateLimit 临时关闭包级限速（仅单测使用，避免用例等待 1s）。
func disableNominatimRateLimit() func() {
	old := nominatimLimiter
	nominatimLimiter = nil
	return func() { nominatimLimiter = old }
}

// minIntervalLimiter 最小间隔限流器（并发安全，不支持突发）。
type minIntervalLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newMinIntervalLimiter(interval time.Duration) *minIntervalLimiter {
	return &minIntervalLimiter{interval: interval}
}

// wait 阻塞至下一个可用时间槽；ctx 取消/超时时返回 ctx.Err()。
func (l *minIntervalLimiter) wait(ctx context.Context) error {
	if l == nil || l.interval <= 0 {
		return nil
	}
	l.mu.Lock()
	now := time.Now()
	if l.next.Before(now) {
		l.next = now
	}
	wait := l.next.Sub(now)
	l.next = l.next.Add(l.interval)
	l.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// get 发起一次 Nominatim 请求（统一 UA、限速与响应体上限），返回响应体。
func (g NominatimGeocoder) get(ctx context.Context, path string) ([]byte, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://nominatim.openstreetmap.org"
	}
	cli := g.Client
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	// 条款合规：先过限速再发请求（超时/取消直接返回，不占用上游配额）。
	if err := nominatimLimiter.wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", nominatimUserAgent)
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim 状态码 %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxNominatimBody))
}

// Geocode 调用 /search?q=&format=json&limit=1 取首个候选。
func (g NominatimGeocoder) Geocode(ctx context.Context, query string) (float64, float64, bool, error) {
	body, err := g.get(ctx, "/search?q="+url.QueryEscape(query)+"&format=json&limit=1")
	if err != nil {
		return 0, 0, false, err
	}
	var cands []struct {
		Lon string `json:"lon"`
		Lat string `json:"lat"`
	}
	if err := json.Unmarshal(body, &cands); err != nil {
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

// Search 正向地理编码多候选（契约 §7 /map/search 的国际源），
// 复用本类型既有的 UA / 限速 / 超时，避免出现第二份 Nominatim 实现。
// 无结果返回 (nil, nil)（不是错误）；上游异常返回错误，由调用方降级处理。
func (g NominatimGeocoder) Search(ctx context.Context, query string, limit int) ([]Place, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > maxPlaceSearchLimit {
		limit = maxPlaceSearchLimit
	}
	// accept-language 提升中文/多语查询命中率。
	body, err := g.get(ctx, "/search?q="+url.QueryEscape(query)+
		"&format=json&limit="+strconv.Itoa(limit)+"&accept-language=zh-CN,en")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
		Lon         string `json:"lon"`
		Lat         string `json:"lat"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("nominatim 响应解析失败: %w", err)
	}
	out := make([]Place, 0, len(raw))
	for _, c := range raw {
		lon, err1 := strconv.ParseFloat(c.Lon, 64)
		lat, err2 := strconv.ParseFloat(c.Lat, 64)
		if err1 != nil || err2 != nil {
			continue // 单条候选字段缺失/非法：跳过该条，不影响其余候选
		}
		name := strings.TrimSpace(c.DisplayName)
		if name == "" {
			name = strings.TrimSpace(c.Name)
		}
		if name == "" {
			name = query
		}
		// Address 取 display_name（Nominatim 的完整地址），与短名 name 区分。
		out = append(out, Place{
			Name:    name,
			Address: strings.TrimSpace(c.DisplayName),
			Lon:     lon,
			Lat:     lat,
		})
	}
	return out, nil
}
