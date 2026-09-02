// Package geo 逆地理编码：高德（Amap）regeo 实现。
//
// 坐标系约定（重要）：库内一律存 WGS-84（GPS 原始坐标），高德接口要求 GCJ-02。
// 本文件在发起请求前统一调用 WGS84ToGCJ02 转换，否则地名会产生数百米偏移。
// 缓存 key 以**输入的 WGS-84 坐标**为准，见 ReverseCacheKey。
package geo

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 哨兵错误：调用方用 errors.Is 判定并优雅降级（尤其 ErrNoKey 绝不能阻断媒体入库）。
var (
	// ErrNoKey 未配置高德 Key（AMAP_KEY 为空）。
	ErrNoKey = errors.New("geo: 未配置高德 Key（环境变量 AMAP_KEY 为空）")
	// ErrRateLimited 高德配额或 QPS 超限（infocode 10021/10003 等）。
	ErrRateLimited = errors.New("geo: 高德接口配额/QPS 超限")
	// ErrEmptyAddress 接口成功但 formatted_address 为空。
	ErrEmptyAddress = errors.New("geo: 高德返回空地址")
)

const (
	amapDefaultBaseURL = "https://restapi.amap.com"
	amapRegeoPath      = "/v3/geocode/regeo"
	amapDefaultQPS     = 5.0 // 高德免费额度有限，默认每秒 5 次
	amapDefaultTimeout = 5 * time.Second
	amapDefaultTTL     = 30 * 24 * time.Hour
	amapProvider       = "amap"
)

// AmapGeocoder 高德逆地理编码客户端。零值字段自动取默认值，可直接用结构体字面量构造。
type AmapGeocoder struct {
	// Key 高德 Web 服务 Key；为空时回退读取环境变量 AMAP_KEY。
	Key string
	// Secret 高德安全密钥（jscode）；为空时回退读取环境变量 AMAP_SECRET。
	// 非空时为每个请求附加 sig 数字签名（Key 在控制台绑定安全密钥后必须签名）。
	Secret string
	// BaseURL 接口根地址，默认 https://restapi.amap.com（测试可指向 httptest server）。
	BaseURL string
	// Client HTTP 客户端，默认超时 5s。
	Client *http.Client
	// Pool 数据库连接池；为 nil 时跳过 geo_cache 读写（便于测试）。
	Pool *pgxpool.Pool
	// TTL 缓存有效期，默认 30 天。
	TTL time.Duration
	// QPS 每秒请求上限，默认 5；设为负数表示不限流。
	QPS float64

	once    sync.Once
	limiter *qpsLimiter
}

// cachedAddress geo_cache.result 的逆地理结构。
type cachedAddress struct {
	Address string `json:"address"`
}

// amapRegeoResp 高德 v3 regeo 响应。
type amapRegeoResp struct {
	Status    string `json:"status"`
	Info      string `json:"info"`
	InfoCode  string `json:"infocode"`
	Regeocode struct {
		FormattedAddress string `json:"formatted_address"`
	} `json:"regeocode"`
}

// ReverseCacheKey 逆地理缓存 key：格式 "rev:<lat4>,<lng4>"（经纬度保留 4 位小数）。
// 与正向地理编码的 "fwd:<query>" 命名空间隔离；入参为库内存储的 WGS-84 坐标。
func ReverseCacheKey(lat, lng float64) string {
	return "rev:" + strconv.FormatFloat(lat, 'f', 4, 64) + "," + strconv.FormatFloat(lng, 'f', 4, 64)
}

// ReverseGeocode WGS-84 坐标 → 可读地名。
// 先查 geo_cache，未命中/过期才请求高德并回填缓存。
// 返回 ErrNoKey 表示未配置 Key，调用方应静默降级（不填充 place）而非报错中断。
func (g *AmapGeocoder) ReverseGeocode(ctx context.Context, lat, lng float64) (string, error) {
	key := ReverseCacheKey(lat, lng)
	if addr, ok := g.loadCache(ctx, key); ok {
		return addr, nil
	}
	addr, err := g.reverseGeocode(ctx, lat, lng)
	if err != nil {
		return "", err
	}
	g.saveCache(ctx, key, addr)
	return addr, nil
}

// reverseGeocode 实际发起 HTTP 请求（无缓存）。
func (g *AmapGeocoder) reverseGeocode(ctx context.Context, lat, lng float64) (string, error) {
	apiKey := strings.TrimSpace(g.Key)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("AMAP_KEY"))
	}
	if apiKey == "" {
		return "", ErrNoKey
	}
	if err := g.throttle(ctx); err != nil {
		return "", err
	}

	// 高德需要 GCJ-02；库内存的是 WGS-84，必须先转换，否则地名偏移数百米。
	gLng, gLat := WGS84ToGCJ02(lng, lat)

	base := strings.TrimRight(g.BaseURL, "/")
	if base == "" {
		base = amapDefaultBaseURL
	}
	u, err := url.Parse(base + amapRegeoPath)
	if err != nil {
		return "", fmt.Errorf("geo: 高德地址非法: %w", err)
	}
	q := u.Query()
	q.Set("key", apiKey)
	q.Set("location", formatLocation(gLng, gLat)) // 高德要求 "lng,lat"
	q.Set("extensions", "base")
	q.Set("output", "JSON")

	// 安全密钥（jscode）非空时附加 sig 数字签名；Key 未绑定安全密钥时不带 sig 也合法。
	secret := strings.TrimSpace(g.Secret)
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("AMAP_SECRET"))
	}
	if secret != "" {
		q.Set("sig", amapSig(q, secret))
	}
	u.RawQuery = q.Encode()

	cli := g.Client
	if cli == nil {
		cli = &http.Client{Timeout: amapDefaultTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := cli.Do(req)
	if err != nil {
		// 超时/网络错误：url.Error 可被 errors.Is(ctx.Err()) 判定。
		return "", fmt.Errorf("geo: 高德请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geo: 高德 HTTP 状态码 %d", resp.StatusCode)
	}

	var out amapRegeoResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("geo: 高德响应解析失败: %w", err)
	}
	if out.Status != "1" {
		if isAmapQuotaError(out.InfoCode, out.Info) {
			return "", fmt.Errorf("%w: %s", ErrRateLimited, out.Info)
		}
		return "", fmt.Errorf("geo: 高德逆地理失败: %s (infocode=%s)", out.Info, out.InfoCode)
	}
	addr := strings.TrimSpace(out.Regeocode.FormattedAddress)
	if addr == "" {
		return "", ErrEmptyAddress
	}
	return addr, nil
}

// amapSig 高德数字签名：除 sig 外的全部请求参数按 key 字典序排序，
// 以 "key=value&..." 拼接后在末尾追加安全密钥，取 md5 十六进制（小写）。
// 注意拼接使用原始值，不做 URL 编码（高德签名规范）。
func amapSig(q url.Values, secret string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(q.Get(k))
	}
	sb.WriteString(secret)
	sum := md5.Sum([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// formatLocation 高德 location 参数："lng,lat"，最多 6 位小数。
func formatLocation(lng, lat float64) string {
	return strconv.FormatFloat(lng, 'f', 6, 64) + "," + strconv.FormatFloat(lat, 'f', 6, 64)
}

// isAmapQuotaError 判定高德配额/QPS 类错误码（10021 QPS 超限、10003 日访问量超限等）。
func isAmapQuotaError(infoCode, info string) bool {
	switch strings.TrimSpace(infoCode) {
	case "10021", "10003", "10022", "10023", "10029":
		return true
	}
	upper := strings.ToUpper(strings.TrimSpace(info))
	return strings.Contains(upper, "QPS") || strings.Contains(upper, "EXCEED") ||
		strings.Contains(upper, "QUOTA") || strings.Contains(upper, "LIMIT")
}

// throttle 简单 QPS 限流（最小间隔法）。
func (g *AmapGeocoder) throttle(ctx context.Context) error {
	g.once.Do(func() {
		qps := g.QPS
		if qps == 0 {
			qps = amapDefaultQPS
		}
		if qps > 0 {
			g.limiter = newQPSLimiter(qps)
		}
	})
	if g.limiter == nil {
		return nil
	}
	return g.limiter.Wait(ctx)
}

// loadCache 读 geo_cache；Pool 为 nil 或任何异常都视为未命中（缓存失败不影响主流程）。
func (g *AmapGeocoder) loadCache(ctx context.Context, key string) (string, bool) {
	if g.Pool == nil {
		return "", false
	}
	var raw []byte
	err := g.Pool.QueryRow(ctx,
		`SELECT result FROM geo_cache WHERE key = $1 AND (expires_at IS NULL OR expires_at > now())`,
		key).Scan(&raw)
	if err != nil {
		return "", false
	}
	var v cachedAddress
	if json.Unmarshal(raw, &v) != nil || v.Address == "" {
		return "", false
	}
	return v.Address, true
}

// saveCache 回填 geo_cache（provider='amap'）；写入失败忽略。
func (g *AmapGeocoder) saveCache(ctx context.Context, key, address string) {
	if g.Pool == nil {
		return
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = amapDefaultTTL
	}
	raw, _ := json.Marshal(cachedAddress{Address: address})
	_, _ = g.Pool.Exec(ctx, `
		INSERT INTO geo_cache (key, provider, result, expires_at)
		VALUES ($1, 'amap', $2, now() + $3::interval)
		ON CONFLICT (key) DO UPDATE SET result = EXCLUDED.result, expires_at = EXCLUDED.expires_at`,
		key, raw, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
}

// qpsLimiter 最小间隔限流器（不支持突发，足够单 worker 逆地理使用）。
type qpsLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	next     time.Time
}

func newQPSLimiter(qps float64) *qpsLimiter {
	return &qpsLimiter{interval: time.Duration(float64(time.Second) / qps)}
}

// Wait 阻塞至下一个可用时间槽；ctx 取消/超时时返回 ctx.Err()。
func (l *qpsLimiter) Wait(ctx context.Context) error {
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
