package geo

// 系统地图配置的管理端读写（契约 §7 `GET/PUT /admin/map-config`，需 `admin:system`）。
//
// 读侧早已存在（mapconfig.go 的 MapConfigStore.Load，供 /map/search 与 /map/providers 选源），
// 本文件补的是**管理端视图与写入**。三处刻意设计：
//
//  1. **绝不返回密钥**。`china_api_key_enc` 是密文列，任何情况下都不回显；
//     只回一个可用性状态（ok/absent/undecryptable/load_failed）——管理界面需要知道的是
//     "中国地名源现在能不能用"，而不是密钥本身。
//
//  2. **PUT 不接受密钥**（详见 PutSystemMapConfig 的说明）：本仓库没有密钥管理设施，
//     把明文写进名为 `_enc` 的列是**说谎**，比不实现更糟。当前高德 Key 由 `AMAP_KEY`
//     环境变量提供（已在生产路径验证过），管理界面只需显示其状态。
//
//  3. **provider 走白名单**。这两个值直接决定底图与地名源走哪条分支，
//     存进拼错的值不会立刻报错，只会让地图"打不开"——故障现场离根因几百行远。

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidConfig 地图配置入参非法（handler 据此返回 400）。
var ErrInvalidConfig = errors.New("地图配置非法")

// 支持的 provider 取值。新增一家需要同时改代码里的选源分支，故这里是白名单而非自由文本。
var (
	validChinaProviders = []string{"amap"}
	validIntlProviders  = []string{"osm", "maptiler"}
)

// SystemMapConfigView 管理端视图（**不含任何密钥或密文**）。
type SystemMapConfigView struct {
	ChinaProvider string `json:"china_provider"`
	ChinaTileURL  string `json:"china_tile_url,omitempty"`
	IntlProvider  string `json:"intl_provider"`
	IntlTileURL   string `json:"intl_tile_url,omitempty"`

	// ChinaAPIKeyState 高德 Key 可用性：ok|absent|undecryptable|load_failed（见 mapconfig.go 常量）。
	ChinaAPIKeyState string `json:"china_api_key_state"`
	// ChinaAPIKeySource 密钥来源：env|db|none —— 让运维一眼看出"改 .env 还是改这里"。
	ChinaAPIKeySource string `json:"china_api_key_source"`

	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// SystemMapConfigInput PUT 的入参（部分更新：nil = 不改）。
type SystemMapConfigInput struct {
	ChinaProvider *string `json:"china_provider"`
	ChinaTileURL  *string `json:"china_tile_url"`
	IntlProvider  *string `json:"intl_provider"`
	IntlTileURL   *string `json:"intl_tile_url"`
}

// Empty 是否没有任何改动。
func (in SystemMapConfigInput) Empty() bool {
	return in.ChinaProvider == nil && in.ChinaTileURL == nil &&
		in.IntlProvider == nil && in.IntlTileURL == nil
}

// NormalizeSystemMapConfig 校验并归一化（纯函数，便于穷举单测）。
// tile URL 允许空串（= 清空、改用内置底图），非空时必须是 http(s)。
func NormalizeSystemMapConfig(in SystemMapConfigInput) (SystemMapConfigInput, error) {
	var out SystemMapConfigInput
	if in.ChinaProvider != nil {
		v := strings.ToLower(strings.TrimSpace(*in.ChinaProvider))
		if !contains(validChinaProviders, v) {
			return out, fmt.Errorf("china_provider 仅支持 %s", strings.Join(validChinaProviders, "|"))
		}
		out.ChinaProvider = &v
	}
	if in.IntlProvider != nil {
		v := strings.ToLower(strings.TrimSpace(*in.IntlProvider))
		if !contains(validIntlProviders, v) {
			return out, fmt.Errorf("intl_provider 仅支持 %s", strings.Join(validIntlProviders, "|"))
		}
		out.IntlProvider = &v
	}
	for src, dst := range map[*string]**string{in.ChinaTileURL: &out.ChinaTileURL, in.IntlTileURL: &out.IntlTileURL} {
		if src == nil {
			continue
		}
		raw := strings.TrimSpace(*src)
		if raw != "" {
			u, err := url.Parse(raw)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return out, errors.New("tile_url 必须是以 http:// 或 https:// 开头的完整地址（空串表示清空）")
			}
		}
		v := raw
		*dst = &v
	}
	return out, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// GetSystemMapConfig 读取管理端视图（表为空时返回内置默认值 + absent 状态）。
func (s *MediaStore) GetSystemMapConfig(ctx context.Context, keyState, keySource string) (*SystemMapConfigView, error) {
	out := &SystemMapConfigView{
		ChinaProvider:     defaultChinaProvider,
		IntlProvider:      defaultIntlProvider,
		ChinaAPIKeyState:  keyState,
		ChinaAPIKeySource: keySource,
	}
	var chinaProv, intlProv, chinaURL, intlURL *string
	var updated *time.Time
	err := s.Pool.QueryRow(ctx, `
		SELECT china_provider, intl_provider, china_tile_url, intl_tile_url, updated_at
		FROM system_map_config ORDER BY updated_at DESC, id LIMIT 1`).
		Scan(&chinaProv, &intlProv, &chinaURL, &intlURL, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if v := strings.TrimSpace(derefString(chinaProv)); v != "" {
		out.ChinaProvider = v
	}
	if v := strings.TrimSpace(derefString(intlProv)); v != "" {
		out.IntlProvider = v
	}
	out.ChinaTileURL = strings.TrimSpace(derefString(chinaURL))
	out.IntlTileURL = strings.TrimSpace(derefString(intlURL))
	out.UpdatedAt = updated
	return out, nil
}

// PutSystemMapConfig 原子 upsert 系统地图配置，返回更新后的视图。
//
// ⚠️ **刻意不接受 `china_api_key`**。列名 `china_api_key_enc` 声明的是"密文"，
// 而本仓库没有密钥管理设施（KEK 放哪里？怎么轮换？）。两条路都不该悄悄走：
//
//	· 把明文写进 `_enc` 列 = 列名说谎，且密钥以明文躺在数据库与备份里；
//	· 自造一套"用某个已存在的密钥派生 KEK"的加密 = 一个需要单独评审的安全设计
//	  （且会让"轮换 JWT_SECRET 就打不开地图配置"这类耦合变成隐形陷阱）。
//
// 因此本端点只写**非密钥**字段；高德 Key 继续由 `AMAP_KEY` 环境变量提供
// （已在生产路径验证可用），管理界面通过 `china_api_key_state` 显示其可用性。
// 若后续要支持"界面上填 Key"，需先定密钥管理方案 —— 已登记在待解决问题登记簿。
//
// 单行保证由迁移 00023 的唯一索引承担，故这里可以直接 ON CONFLICT 做原子 upsert。
func (s *MediaStore) PutSystemMapConfig(ctx context.Context, in SystemMapConfigInput, keyState, keySource string) (*SystemMapConfigView, error) {
	norm, err := NormalizeSystemMapConfig(in)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}
	if norm.Empty() {
		return nil, fmt.Errorf("%w: 没有任何待更新字段", ErrInvalidConfig)
	}

	// 用 COALESCE($n, 现列) 实现部分更新：未提供的字段保持原值。
	// 首次写入时现列为 NULL，用 DDL 默认值兜底（与读侧的默认值一致）。
	_, err = s.Pool.Exec(ctx, `
		INSERT INTO system_map_config (china_provider, intl_provider, china_tile_url, intl_tile_url, updated_at)
		VALUES (COALESCE($1, $5), COALESCE($2, $6), $3, $4, now())
		ON CONFLICT (singleton) DO UPDATE SET
			china_provider = COALESCE($1, system_map_config.china_provider),
			intl_provider  = COALESCE($2, system_map_config.intl_provider),
			china_tile_url = COALESCE($3, system_map_config.china_tile_url),
			intl_tile_url  = COALESCE($4, system_map_config.intl_tile_url),
			updated_at     = now()`,
		norm.ChinaProvider, norm.IntlProvider, norm.ChinaTileURL, norm.IntlTileURL,
		defaultChinaProvider, defaultIntlProvider)
	if err != nil {
		return nil, err
	}
	return s.GetSystemMapConfig(ctx, keyState, keySource)
}
