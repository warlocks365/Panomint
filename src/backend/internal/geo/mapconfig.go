package geo

// system_map_config 读取（契约 §7 GET /map/search 的选源依据、GET /map/providers 的配置来源）。
//
// 设计要点：
//   - 该表为**单行**系统配置（DDL §2.5），允许空表：空表/查询失败一律按内置默认值工作，
//     绝不因配置缺失让端点报错（默认 china=amap / intl=osm）。
//   - china_api_key_enc 按 DDL 语义为**加密存储**。仓库当前没有任何 AES/密钥管理设施，
//     因此解密以函数注入（Decrypt）而非在本包自造密码学：
//       · 注入 Decrypt → 按密文解密，失败则视为「不可用」并降级；
//       · 未注入 Decrypt → 按明文处理（兼容运营直接写入明文 key 的部署），
//         若该值是密文，高德会返回 INVALID_USER_KEY，编排层随即降级（不会 500）。
//   - 明文密钥只在本进程内存中出现，绝不经 /map/providers 或日志输出。

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 内置默认底图供应商（与 DDL DEFAULT 一致）。
const (
	defaultChinaProvider = "amap"
	defaultIntlProvider  = "osm"
)

// 高德 Key 可用性状态（用于降级原因归因与日志观测）。
const (
	// ChinaKeyOK 密钥可用（来自 system_map_config 解密值或环境变量）。
	ChinaKeyOK = "ok"
	// ChinaKeyAbsent 未配置密钥（表为空或 china_api_key_enc 为 NULL/空）。
	ChinaKeyAbsent = "absent"
	// ChinaKeyUndecryptable 配置了密文但解密失败（Decrypt 返回错误或空值）。
	ChinaKeyUndecryptable = "undecryptable"
	// ChinaKeyLoadFailed 读取配置失败（DB 异常），按默认值继续。
	ChinaKeyLoadFailed = "load_failed"
)

// MapConfig 地图系统配置（不含明文密钥导出路径）。
type MapConfig struct {
	ChinaProvider string // 中国底图/地名源：amap|null
	IntlProvider  string // 国际底图/地名源：osm|maptiler
	// ChinaAPIKey 解密后的高德 Key；空表示当前不可用。
	ChinaAPIKey string
	// ChinaKeyState 见 ChinaKeyOK/Absent/Undecryptable/LoadFailed。
	ChinaKeyState string
}

// MapConfigStore 读取 system_map_config。Pool 为 nil 时只返回默认值（便于单测）。
type MapConfigStore struct {
	Pool *pgxpool.Pool
	// Decrypt 可选：解密 china_api_key_enc。nil 表示按明文处理（见文件头说明）。
	Decrypt func(enc string) (string, error)
}

// Load 读取系统地图配置；表为空、无行或查询失败时返回内置默认值，不返回错误。
func (s *MapConfigStore) Load(ctx context.Context) MapConfig {
	out := MapConfig{
		ChinaProvider: defaultChinaProvider,
		IntlProvider:  defaultIntlProvider,
		ChinaKeyState: ChinaKeyAbsent,
	}
	if s == nil || s.Pool == nil {
		return out
	}
	var chinaProv, intlProv, enc *string
	err := s.Pool.QueryRow(ctx, `
		SELECT china_provider, intl_provider, china_api_key_enc
		FROM system_map_config
		ORDER BY updated_at DESC
		LIMIT 1`).Scan(&chinaProv, &intlProv, &enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return out
	}
	if err != nil {
		out.ChinaKeyState = ChinaKeyLoadFailed
		return out
	}
	if v := strings.TrimSpace(derefString(chinaProv)); v != "" {
		out.ChinaProvider = v
	}
	if v := strings.TrimSpace(derefString(intlProv)); v != "" {
		out.IntlProvider = v
	}
	raw := strings.TrimSpace(derefString(enc))
	if raw == "" {
		return out
	}
	out.ChinaAPIKey, out.ChinaKeyState = resolveEncryptedKey(raw, s.Decrypt)
	return out
}

// resolveEncryptedKey 由密文列得到明文 Key 与可用性状态（纯函数，便于单测）：
//   - 注入 decrypt → 解密成功返回 (plain, ok)；失败或空值返回 ("", undecryptable)；
//   - 未注入 decrypt → 按明文处理返回 (raw, ok)（见文件头说明）。
func resolveEncryptedKey(raw string, decrypt func(string) (string, error)) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ChinaKeyAbsent
	}
	if decrypt == nil {
		return raw, ChinaKeyOK
	}
	plain, err := decrypt(raw)
	plain = strings.TrimSpace(plain)
	if err != nil || plain == "" {
		return "", ChinaKeyUndecryptable
	}
	return plain, ChinaKeyOK
}

// derefString 安全解引用可空字符串列。
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
