-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE TYPE share_kind AS ENUM ('album', 'media');



CREATE TABLE share_links (

    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    token           VARCHAR(64) NOT NULL UNIQUE,

    kind            share_kind NOT NULL,

    target_id       UUID NOT NULL,             -- album_id 或 media_id

    owner_id        UUID NOT NULL REFERENCES users(id),

    title           VARCHAR(255),

    expire_at       TIMESTAMPTZ,

    password_hash   VARCHAR(255),

    allow_download  BOOLEAN NOT NULL DEFAULT false,

    is_wechat       BOOLEAN NOT NULL DEFAULT false, -- 微信 H5 链接（非整文件）

    max_views       INTEGER,

    access_count    INTEGER NOT NULL DEFAULT 0,    -- 累计访问次数

    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_share_token ON share_links(token);

CREATE INDEX idx_share_expire ON share_links(expire_at);



CREATE TABLE share_access_log (

    id         BIGSERIAL PRIMARY KEY,

    share_id   UUID NOT NULL REFERENCES share_links(id) ON DELETE CASCADE,

    ip         VARCHAR(64),

    user_agent TEXT,

    at         TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_sharelog_share ON share_access_log(share_id);



-- 回忆影片（可选模块）

CREATE TABLE memories (

    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    title            VARCHAR(255),

    criteria         JSONB,                    -- 主题选取条件

    video_asset_path TEXT,

    owner_id         UUID NOT NULL REFERENCES users(id),

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 后台任务（索引/转码）

CREATE TABLE index_jobs (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    kind        VARCHAR(32) NOT NULL,          -- full|incremental

    user_id     UUID REFERENCES users(id),     -- 触发者

    status      VARCHAR(16) NOT NULL DEFAULT 'pending', -- pending|running|done|failed

    total       INTEGER DEFAULT 0,

    processed   INTEGER DEFAULT 0,

    started_at  TIMESTAMPTZ,

    finished_at TIMESTAMPTZ,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE TABLE transcode_jobs (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    media_id    UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,

    node_id     UUID REFERENCES compute_nodes(id),  -- 处理该任务的算力节点

    kind        VARCHAR(32) NOT NULL,          -- thumbnail|hls|memories

    status      VARCHAR(16) NOT NULL DEFAULT 'pending',

    profile     VARCHAR(32),                   -- 1080p|2k|4k

    result_path TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_transcode_media ON transcode_jobs(media_id, status);
-- +goose StatementEnd
