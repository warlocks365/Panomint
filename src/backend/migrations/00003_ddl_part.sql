-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
-- 用户级 UI 偏好（地图模式布局）

CREATE TABLE user_ui_prefs (

    user_id             UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,

    map_slider_pos      VARCHAR(8)  NOT NULL DEFAULT 'bottom',  -- top|bottom

    map_filter_side     VARCHAR(8)  NOT NULL DEFAULT 'left',     -- left|right

    map_default_provider VARCHAR(16) NOT NULL DEFAULT 'auto',    -- auto|amap|osm

    map_default_zoom    INTEGER,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 系统级地图配置（管理员可配；高德 API key 等敏感信息加密存储）

CREATE TABLE system_map_config (

    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    china_provider      VARCHAR(16) NOT NULL DEFAULT 'amap',  -- amap|null

    china_api_key_enc   TEXT,                                -- 加密存储的高德 key

    china_tile_url      TEXT,                               -- 高德瓦片模板(可选自定义)

    intl_provider       VARCHAR(16) NOT NULL DEFAULT 'osm',  -- osm|maptiler

    intl_tile_url       TEXT,                               -- 国际瓦片/样式 URL

    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 地理编码缓存（避免重复调用外部 API）

CREATE TABLE geo_cache (

    key          VARCHAR(128) PRIMARY KEY,   -- "rev:lat,lon" 或 "fwd:query"

    provider     VARCHAR(16),

    result       JSONB NOT NULL,

    expires_at   TIMESTAMPTZ

);

CREATE INDEX idx_geocache_exp ON geo_cache(expires_at);
-- +goose StatementEnd
