package geo

// 模糊地理搜索（契约 §7）：GET /map/search、GET /map/providers。
//
// 选源（按 system_map_config 自动）：
//   - 查询词含汉字 → 中国地名 → 高德 Web 服务地理编码 /v3/geocode/geo；
//   - 其余（拉丁/假名/谚文等） → 国际地名 → OSM Nominatim（复用 internal/search 既有客户端）。
//
// 坐标系（与 /geo/* 严格同一约定，见 media_store.go 文件头）：
//   - 库内一律 WGS-84；对外按 provider 转换 —— provider=amap 时输出 GCJ-02，其余输出 WGS-84。
//   - 高德返回 GCJ-02、Nominatim 返回 WGS-84，因此**两者都要在响应前归一化到对外坐标系**，
//     否则同一候选列表里混着两套坐标系，前端 flyTo/bbox 会偏数百米。
//   - 缓存只存**上游原生坐标**，因此同一份缓存可服务任意 provider（转换在响应时做）。
//
// 降级（绝不 500）：
//   - 中国地名但高德 Key 不可用（当前部署即如此，system_map_config 为空表且未配 AMAP_KEY）
//     → 记录降级原因并回落 Nominatim；Nominatim 也不可用/无结果 → 200 + candidates: []。
//   - 上游超时/报错/字段异常 → 同上降级，**错误原文不下发前端**，只写服务端日志。
//
// 缓存：复用既有 geo_cache 表（见 MapSearchCacheKey），同一 q 重复查询直接命中。
// 限流：契约 §17 的「每用户 600 次/分钟」由全局中间件负责；本端点额外注意**上游配额**
// —— 高德按 Key 计 QPS（本服务内置 qpsLimiter）、Nominatim 按应用限 1 req/s
// （由 internal/search 的包级限速器保证），故务必依赖缓存，勿高频刷新调用。

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"panoalbum/internal/httperr"
	"panoalbum/internal/search"
)

const (
	nominatimProvider = "nominatim"
	// amapGeoPath 高德 Web 服务地理编码（正向）接口路径。
	amapGeoPath = "/v3/geocode/geo"

	defaultMapSearchTimeout  = 5 * time.Second    // 上游必须在数秒内返回，否则降级
	defaultMapSearchLimit    = 5                  // 候选条数默认上限
	maxMapSearchLimit        = 20                 // 候选条数硬上限
	maxMapSearchQueryRunes   = 200                // q 长度上限（护上游，也护缓存 key）
	defaultMapSearchCacheTTL = 7 * 24 * time.Hour // 搜索结果稳定性高，7 天足够
	defaultAmapForwardQPS    = 5.0                // 高德免费额度有限（与逆地理同一上游配额）
)

// MapCandidate /map/search 响应中的候选地名。
type MapCandidate struct {
	Name     string  `json:"name"`
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	Provider string  `json:"provider"` // amap|nominatim，标注来源
}

// ForwardHit 上游返回的单条候选；坐标为**上游原生坐标系**（amap=GCJ-02/nominatim=WGS-84）。
// 该结构同时是 geo_cache 中的缓存负载，故带 JSON tag。
type ForwardHit struct {
	Name     string  `json:"name"`
	Lon      float64 `json:"lon"`
	Lat      float64 `json:"lat"`
	Provider string  `json:"provider"`
}

// AmapForwarder 高德正向地理编码能力。Key/Secret 逐次注入（而非实例字段），
// 既支持 system_map_config 运行期改配置，也避免并发请求下共享字段竞态。
type AmapForwarder interface {
	ForwardWithKey(ctx context.Context, key, secret, query string, limit int) ([]ForwardHit, error)
}

// IntlForwarder 国际正向地理编码能力（Nominatim，无需 Key）。
type IntlForwarder interface {
	Forward(ctx context.Context, query string, limit int) ([]ForwardHit, error)
}

// AmapForwardGeocoder 高德正向地理编码客户端（无状态，可并发复用）。
// 复用本包既有的签名/错误码判定工具（amapSig / isAmapQuotaError / ErrNoKey / ErrRateLimited）。
type AmapForwardGeocoder struct {
	// BaseURL 接口根地址，默认 https://restapi.amap.com（测试可指向 httptest server）。
	BaseURL string
	// Client HTTP 客户端，默认超时 5s。
	Client *http.Client
}

// amapForwardResp 高德 v3 geocode（正向）响应。
type amapForwardResp struct {
	Status   string `json:"status"`
	Info     string `json:"info"`
	InfoCode string `json:"infocode"`
	Count    string `json:"count"`
	Geocodes []struct {
		FormattedAddress string `json:"formatted_address"`
		Province         string `json:"province"`
		City             string `json:"city"`
		District         string `json:"district"`
		Location         string `json:"location"` // "lng,lat"（GCJ-02）
	} `json:"geocodes"`
}

// ForwardWithKey 调用 /v3/geocode/geo 解析地名。
// 返回空切片 + nil 表示「无结果」（不是错误）；ErrNoKey 表示无可用 Key，调用方应降级。
func (g *AmapForwardGeocoder) ForwardWithKey(ctx context.Context, key, secret, query string, limit int) ([]ForwardHit, error) {
	apiKey := strings.TrimSpace(key)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("AMAP_KEY"))
	}
	if apiKey == "" {
		return nil, ErrNoKey
	}
	if limit <= 0 {
		limit = defaultMapSearchLimit
	}
	if limit > maxMapSearchLimit {
		limit = maxMapSearchLimit
	}

	base := strings.TrimRight(g.BaseURL, "/")
	if base == "" {
		base = amapDefaultBaseURL
	}
	u, err := url.Parse(base + amapGeoPath)
	if err != nil {
		return nil, fmt.Errorf("geo: 高德地址非法: %w", err)
	}
	q := u.Query()
	q.Set("key", apiKey)
	q.Set("address", query)
	q.Set("output", "JSON")
	if sec := strings.TrimSpace(secret); sec != "" {
		q.Set("sig", amapSig(q, sec))
	}
	u.RawQuery = q.Encode()

	cli := g.Client
	if cli == nil {
		cli = &http.Client{Timeout: amapDefaultTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("geo: 高德请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("geo: 高德 HTTP 状态码 %d", resp.StatusCode)
	}

	var out amapForwardResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("geo: 高德响应解析失败: %w", err)
	}
	if out.Status != "1" {
		if isAmapQuotaError(out.InfoCode, out.Info) {
			return nil, fmt.Errorf("%w: %s", ErrRateLimited, out.Info)
		}
		return nil, fmt.Errorf("geo: 高德地理编码失败: %s (infocode=%s)", out.Info, out.InfoCode)
	}

	hits := make([]ForwardHit, 0, len(out.Geocodes))
	for _, gc := range out.Geocodes {
		lng, lat, ok := parseAmapLocation(gc.Location)
		if !ok {
			continue // 单条候选坐标缺失/非法：跳过，不影响其余候选
		}
		name := strings.TrimSpace(gc.FormattedAddress)
		if name == "" {
			name = joinNonEmpty(gc.Province, gc.City, gc.District)
		}
		if name == "" {
			name = query
		}
		hits = append(hits, ForwardHit{Name: name, Lon: lng, Lat: lat, Provider: amapProvider})
		if len(hits) >= limit {
			break
		}
	}
	return hits, nil
}

// parseAmapLocation 解析高德 location 字段（"lng,lat"，GCJ-02）。
func parseAmapLocation(s string) (float64, float64, bool) {
	lngS, latS, ok := strings.Cut(strings.TrimSpace(s), ",")
	if !ok {
		return 0, 0, false
	}
	lng, err1 := strconv.ParseFloat(strings.TrimSpace(lngS), 64)
	lat, err2 := strconv.ParseFloat(strings.TrimSpace(latS), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	if lng < -180 || lng > 180 || lat < -90 || lat > 90 {
		return 0, 0, false
	}
	return lng, lat, true
}

// joinNonEmpty 用空串间隔拼接非空字段（高德 formatted_address 缺失时的兜底命名）。
func joinNonEmpty(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "")
}

// nominatimForwarder 适配 internal/search 既有的 Nominatim 客户端（多候选），
// 刻意不重写第二份 HTTP 实现：UA、限速（≥1s/请求）与超时都由该客户端保证。
type nominatimForwarder struct {
	G search.NominatimGeocoder
}

// Forward 实现 IntlForwarder。
func (a nominatimForwarder) Forward(ctx context.Context, query string, limit int) ([]ForwardHit, error) {
	places, err := a.G.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	hits := make([]ForwardHit, 0, len(places))
	for _, p := range places {
		hits = append(hits, ForwardHit{Name: p.Name, Lon: p.Lon, Lat: p.Lat, Provider: nominatimProvider})
	}
	return hits, nil
}

// cacheStore 搜索结果缓存抽象：默认落 geo_cache 表，测试可注入内存实现。
type cacheStore interface {
	Get(ctx context.Context, key string) ([]ForwardHit, bool)
	Put(ctx context.Context, key, provider string, hits []ForwardHit)
}

// pgCacheStore geo_cache 表实现（与逆地理/place 降级复用同一张表与失效语义）。
type pgCacheStore struct {
	Pool *pgxpool.Pool
	TTL  time.Duration
}

// mapSearchCachePayload geo_cache.result 的负载结构。
type mapSearchCachePayload struct {
	Hits []ForwardHit `json:"hits"`
}

// Get 读缓存；未命中/已过期/结构不符一律视为未命中（缓存异常不影响主流程）。
func (c *pgCacheStore) Get(ctx context.Context, key string) ([]ForwardHit, bool) {
	if c == nil || c.Pool == nil {
		return nil, false
	}
	var raw []byte
	err := c.Pool.QueryRow(ctx,
		`SELECT result FROM geo_cache WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
		key).Scan(&raw)
	if err != nil {
		return nil, false
	}
	var p mapSearchCachePayload
	if json.Unmarshal(raw, &p) != nil {
		return nil, false
	}
	return p.Hits, true
}

// Put 回填缓存（provider 记录实际来源，便于排查）；写入失败忽略。
func (c *pgCacheStore) Put(ctx context.Context, key, provider string, hits []ForwardHit) {
	if c == nil || c.Pool == nil {
		return
	}
	ttl := c.TTL
	if ttl <= 0 {
		ttl = defaultMapSearchCacheTTL
	}
	raw, err := json.Marshal(mapSearchCachePayload{Hits: hits})
	if err != nil {
		return
	}
	_, _ = c.Pool.Exec(ctx, `
		INSERT INTO geo_cache (key, provider, result, expires_at)
		VALUES ($1, $2, $3, now() + $4::interval)
		ON CONFLICT (key) DO UPDATE SET provider = EXCLUDED.provider, result = EXCLUDED.result, expires_at = EXCLUDED.expires_at`,
		key, provider, raw, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
}

// MapSearchCacheKey /map/search 的缓存 key："fwd:<provider>:<query>"。
// provider 参与 key，避免高德与 Nominatim 结果互相串味；与逆地理 "rev:..." 及
// 单结果正向 "fwd:<query>"（search.CachedResolver）分属不同命名空间，互不覆盖。
// key 列上限 128 字节，超长查询退化为查询词的 md5，避免写库失败。
func MapSearchCacheKey(provider, query string) string {
	q := normalizeQuery(query)
	key := "fwd:" + provider + ":" + q
	if len(key) > 128 {
		sum := md5.Sum([]byte(q))
		key = "fwd:" + provider + ":" + hex.EncodeToString(sum[:])
	}
	return key
}

// normalizeQuery 归一化查询词：去首尾空白、折叠内部连续空白（提升缓存命中率）。
func normalizeQuery(q string) string {
	return strings.Join(strings.Fields(q), " ")
}

// configLoader 系统地图配置读取抽象（默认实现 MapConfigStore 读 system_map_config）。
type configLoader interface {
	Load(ctx context.Context) MapConfig
}

// MapSearchService /map/search 与 /map/providers 的编排层。
type MapSearchService struct {
	// Pool 连接池（构造默认 geo_cache 缓存用）。
	Pool *pgxpool.Pool
	// Cache 结果缓存；nil 时用 Pool 构造 pgCacheStore（Pool 也为 nil 则跳过缓存）。
	Cache cacheStore
	// Config system_map_config 读取；nil 时按内置默认值工作。
	Config configLoader
	// Amap 高德正向编码（中国源）；nil 表示该源不可用。
	Amap AmapForwarder
	// Intl 国际正向编码（Nominatim）；nil 表示该源不可用。
	Intl IntlForwarder
	// EnvAmapKey 环境变量兜底的高德 Key（AMAP_KEY），system_map_config 无 key 时使用。
	EnvAmapKey string
	// EnvAmapSecret 高德安全密钥（AMAP_SECRET，随 Key 绑定后必填）。
	EnvAmapSecret string
	// Timeout 单次上游调用超时，默认 5s。
	Timeout time.Duration
	// Limit 候选条数上限，默认 5。
	Limit int
	// QPS 高德上游 QPS 上限，默认 5；负数表示不限流。
	QPS float64
	// DisplayProvider 对外坐标系默认底图（amap=GCJ-02），默认 amap。
	DisplayProvider string
	// Log 可选日志器（降级/上游失败记录）。
	Log *zap.Logger

	once    sync.Once
	limiter *qpsLimiter
}

// NewMapSearchService 装配 /map/search：读取 system_map_config、复用 geo_cache 与
// internal/search 既有 Nominatim 客户端；高德走 v3/geocode/geo。
func NewMapSearchService(pool *pgxpool.Pool, envAmapKey, envAmapSecret string, logger *zap.Logger) *MapSearchService {
	return &MapSearchService{
		Pool:            pool,
		Cache:           &pgCacheStore{Pool: pool},
		Config:          &MapConfigStore{Pool: pool},
		Amap:            &AmapForwardGeocoder{},
		Intl:            nominatimForwarder{G: search.NominatimGeocoder{}},
		EnvAmapKey:      envAmapKey,
		EnvAmapSecret:   envAmapSecret,
		Timeout:         defaultMapSearchTimeout,
		Limit:           defaultMapSearchLimit,
		DisplayProvider: amapProvider,
		Log:             logger,
	}
}

// cacheStoreOrDefault 返回可用缓存实现。
func (s *MapSearchService) cacheStoreOrDefault() cacheStore {
	if s.Cache != nil {
		return s.Cache
	}
	return &pgCacheStore{Pool: s.Pool}
}

// loadConfig 读取系统地图配置；未装配时返回内置默认值（配置缺失不阻断端点）。
func (s *MapSearchService) loadConfig(ctx context.Context) MapConfig {
	if s.Config == nil {
		return (&MapConfigStore{}).Load(ctx)
	}
	return s.Config.Load(ctx)
}

// limit 候选条数上限（含钳制）。
func (s *MapSearchService) limit() int {
	if s.Limit <= 0 {
		return defaultMapSearchLimit
	}
	if s.Limit > maxMapSearchLimit {
		return maxMapSearchLimit
	}
	return s.Limit
}

// throttle 高德上游限流（共享一次 limiter，避免同进程内多请求打爆配额）。
func (s *MapSearchService) throttle(ctx context.Context) error {
	s.once.Do(func() {
		qps := s.QPS
		if qps == 0 {
			qps = defaultAmapForwardQPS
		}
		if qps > 0 {
			s.limiter = newQPSLimiter(qps)
		}
	})
	if s.limiter == nil {
		return nil
	}
	return s.limiter.Wait(ctx)
}

// Candidates 执行模糊地理搜索：选源 → 缓存 → 上游 → 降级 → 归一化到对外坐标系。
//
// 返回值 degraded 非空表示本次发生了降级/源回落（用于日志与 X-Map-Search-Degraded 观测头），
// 取值形如 amap_absent / amap_undecryptable / amap_load_failed / amap_provider_null /
// amap_client_unavailable / amap_error / amap_empty / intl_error（可组合，以 "+" 连接）。
// candidates 永不为 nil（无结果为长度 0 的切片，序列化为 []）。
func (s *MapSearchService) Candidates(ctx context.Context, query, displayProvider string) ([]MapCandidate, string) {
	if displayProvider == "" {
		displayProvider = s.DisplayProvider
	}
	if displayProvider == "" {
		displayProvider = amapProvider
	}
	limit := s.limit()
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = defaultMapSearchTimeout
	}

	cfg := s.loadConfig(ctx)
	key, keyState := s.resolveAmapKey(cfg)
	// china_provider=null 表示系统显式关闭中国底图 → 高德地名源一并停用。
	amapOff := strings.EqualFold(cfg.ChinaProvider, "null")
	amapUsable := s.Amap != nil && key != "" && !amapOff

	var hits []ForwardHit
	var degraded []string

	if isChinaQuery(query) {
		if amapUsable {
			h, err := s.forwardCached(ctx, amapProvider, query, limit, timeout, func(cctx context.Context) ([]ForwardHit, error) {
				if terr := s.throttle(cctx); terr != nil { // 高德按 Key 计 QPS，先过限流
					return nil, terr
				}
				return s.Amap.ForwardWithKey(cctx, key, s.EnvAmapSecret, query, limit)
			})
			if err != nil {
				s.warn("高德地理编码失败，回落 Nominatim", zap.String("q", query), zap.Error(err))
				degraded = append(degraded, "amap_error")
			} else {
				hits = h
				if len(h) == 0 {
					// 高德覆盖不到（如日文汉字地名）或无匹配：标记后交 Nominatim 兜底。
					degraded = append(degraded, "amap_empty")
				}
			}
		} else {
			degraded = append(degraded, amapUnavailableReason(amapOff, s.Amap == nil, keyState))
			s.warn("高德地名源不可用，回落 Nominatim",
				zap.String("q", query), zap.String("reason", keyState), zap.String("china_provider", cfg.ChinaProvider))
		}
	}

	// 国际地名直接走 Nominatim；中国地名在高德不可用/无结果时同样回落（优雅降级）。
	// Nominatim 侧限速由 internal/search 的包级限速器保证（≥1s/请求）。
	if len(hits) == 0 && s.Intl != nil {
		h, err := s.forwardCached(ctx, nominatimProvider, query, limit, timeout, func(cctx context.Context) ([]ForwardHit, error) {
			return s.Intl.Forward(cctx, query, limit)
		})
		if err != nil {
			s.warn("Nominatim 地理编码失败", zap.String("q", query), zap.Error(err))
			degraded = append(degraded, "intl_error")
		} else {
			hits = h
		}
	}

	out := make([]MapCandidate, 0, len(hits))
	for _, h := range hits {
		lon, lat := toDisplayCoord(h, displayProvider)
		out = append(out, MapCandidate{Name: h.Name, Lon: lon, Lat: lat, Provider: h.Provider})
	}
	return out, strings.Join(degraded, "+")
}

// amapUnavailableReason 高德源不可用的降级归因（用于日志与降级头）：
// 系统显式关闭 > 客户端未装配 > 密钥不可用（absent/undecryptable/load_failed）。
func amapUnavailableReason(providerOff, clientMissing bool, keyState string) string {
	switch {
	case providerOff:
		return "amap_provider_null"
	case clientMissing:
		return "amap_client_unavailable"
	case keyState == "" || keyState == ChinaKeyOK:
		return "amap_unavailable"
	default:
		return "amap_" + keyState
	}
}

// resolveAmapKey 高德 Key 优先级：system_map_config（解密值）> 环境变量 AMAP_KEY。
// 两者皆无时返回空 key 与失败原因（用于降级归因）。
func (s *MapSearchService) resolveAmapKey(cfg MapConfig) (string, string) {
	if k := strings.TrimSpace(cfg.ChinaAPIKey); k != "" {
		return k, ChinaKeyOK
	}
	if k := strings.TrimSpace(s.EnvAmapKey); k != "" {
		return k, ChinaKeyOK
	}
	switch cfg.ChinaKeyState {
	case ChinaKeyLoadFailed:
		return "", ChinaKeyLoadFailed
	case ChinaKeyUndecryptable:
		return "", ChinaKeyUndecryptable
	default:
		return "", ChinaKeyAbsent
	}
}

// forwardCached 先查缓存（key 含 provider），未命中才调上游并回填。
// 上游报错时**不写缓存**，避免把瞬时故障固化 TTL。
func (s *MapSearchService) forwardCached(
	ctx context.Context, provider, query string, limit int, timeout time.Duration,
	call func(context.Context) ([]ForwardHit, error),
) ([]ForwardHit, error) {
	cache := s.cacheStoreOrDefault()
	cacheKey := MapSearchCacheKey(provider, query)
	if hits, ok := cache.Get(ctx, cacheKey); ok {
		if len(hits) > limit {
			hits = hits[:limit]
		}
		return hits, nil
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hits, err := call(cctx)
	if err != nil {
		return nil, err
	}
	// 无结果同样缓存：避免对无意义查询反复打上游（同时是上游配额的第一道保护）。
	cache.Put(ctx, cacheKey, provider, hits)
	return hits, nil
}

// warn 可选日志（Log 为 nil 时静默）。
func (s *MapSearchService) warn(msg string, fields ...zap.Field) {
	if s.Log == nil {
		return
	}
	s.Log.Warn(msg, fields...)
}

// isChinaQuery 粗判查询词是否指向中国地名：含汉字（CJK 统一表意文字）即视为中国。
//
// 依据：高德地理编码只覆盖中国行政区划（含港澳台）。拉丁字母/西里尔等 → 国际。
// 假名/谚文属于日韩 → 不算中国（走 Nominatim）。已知局限：日文汉字地名（如"東京"）
// 会被判为中国而先走高德；高德必返空，随即按「空结果回落」交给 Nominatim 兜底。
func isChinaQuery(q string) bool {
	hasHan := false
	for _, r := range q {
		if unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return false // 日文/韩文：明确不属于中国行政区划
		}
		if unicode.Is(unicode.Han, r) {
			hasHan = true
		}
	}
	return hasHan
}

// toDisplayCoord 把上游原生坐标归一化为对外坐标系（与 /geo/* 同一约定）：
// provider=amap → GCJ-02；其余 → WGS-84。
func toDisplayCoord(h ForwardHit, provider string) (float64, float64) {
	if provider == amapProvider {
		if h.Provider == amapProvider {
			return h.Lon, h.Lat // 上游已是 GCJ-02
		}
		return convert(h.Lon, h.Lat, amapProvider) // WGS-84 → GCJ-02
	}
	if h.Provider == amapProvider {
		return GCJ02ToWGS84(h.Lon, h.Lat) // 上游 GCJ-02 → WGS-84
	}
	return h.Lon, h.Lat
}

// Search GET /map/search?q=<关键词>&provider=<amap|osm>
//
// q 为空/全空白/超长 → 400 INVALID_PARAMS（不静默返回空数组）；
// 无结果 → 200 + candidates: []；上游失败 → 降级（200），绝不把上游错误原文下发。
func (s *MapSearchService) Search(c *gin.Context) {
	q := normalizeQuery(c.Query("q"))
	if q == "" {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_PARAMS", "q 不能为空")
		return
	}
	if utf8.RuneCountInString(q) > maxMapSearchQueryRunes {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_PARAMS", fmt.Sprintf("q 过长（最多 %d 字符）", maxMapSearchQueryRunes))
		return
	}
	display := strings.TrimSpace(c.Query("provider"))
	candidates, degraded := s.Candidates(c.Request.Context(), q, display)
	if degraded != "" {
		// 降级观测头（非契约字段，便于部署后排查；不含上游错误原文）。
		c.Header("X-Map-Search-Degraded", degraded)
	}
	c.JSON(http.StatusOK, gin.H{"candidates": candidates})
}

// Providers GET /map/providers 返回可用底图与当前配置（不含密钥）。
func (s *MapSearchService) Providers(c *gin.Context) {
	cfg := s.loadConfig(c.Request.Context())
	china := strings.TrimSpace(cfg.ChinaProvider)
	if china == "" {
		china = defaultChinaProvider
	}
	intl := strings.TrimSpace(cfg.IntlProvider)
	if intl == "" {
		intl = defaultIntlProvider
	}
	// default=auto：由查询词自动选源（见 isChinaQuery）。
	c.JSON(http.StatusOK, gin.H{"china": china, "intl": intl, "default": "auto"})
}

// 编译期断言：确保装配契约稳定，重构时不会静默失配。
var (
	_ AmapForwarder = (*AmapForwardGeocoder)(nil)
	_ IntlForwarder = nominatimForwarder{}
	_ cacheStore    = (*pgCacheStore)(nil)
	_ configLoader  = (*MapConfigStore)(nil)
)
