-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
-- 算力节点（本地 GPU / 云 GPU / 本地网络第三方 GPU 主机）

CREATE TABLE compute_nodes (

    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name           VARCHAR(128) NOT NULL,

    kind           VARCHAR(16) NOT NULL,         -- local_gpu|cloud_gpu|lan_agent

    host           VARCHAR(256),                 -- IP/域名（lan_agent 用内网地址）

    agent_token    VARCHAR(256),                 -- agent 长连接鉴权

    codecs         VARCHAR(64) NOT NULL DEFAULT 'h264',  -- 支持编码: h264|hevc|av1

    has_nvenc      BOOLEAN NOT NULL DEFAULT false,

    vram_mb        INTEGER,

    concurrency    INTEGER NOT NULL DEFAULT 1,

    status         VARCHAR(16) NOT NULL DEFAULT 'offline',  -- online|busy|offline

    last_heartbeat TIMESTAMPTZ,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_nodes_status ON compute_nodes(status);



-- 带宽配置（手动指定 + 自测结果，用于 ABR 档位推荐）

CREATE TABLE bandwidth_profiles (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    scope       VARCHAR(16) NOT NULL DEFAULT 'global',  -- global|share_token

    ref_id      UUID,                                  -- scope=share_token 时填 share_links.id

    up_kbps     INTEGER,                               -- 上行（手动或自测）

    down_kbps   INTEGER,                               -- 下行（手动或自测）

    source      VARCHAR(16) NOT NULL DEFAULT 'manual', -- manual|self_test

    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 带宽自测记录

CREATE TABLE bandwidth_tests (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    profile_id  UUID REFERENCES bandwidth_profiles(id) ON DELETE CASCADE,

    up_kbps     INTEGER,

    down_kbps   INTEGER,

    latency_ms  INTEGER,

    measured_at TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_bwtest_profile ON bandwidth_tests(profile_id, measured_at);
-- +goose StatementEnd
