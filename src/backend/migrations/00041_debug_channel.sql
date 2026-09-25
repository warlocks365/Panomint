-- Job000121（2026-09-26）：转码远程调试通道——一次性、短寿命、全审计的远程排障通道。
--
--   · 全局单通道：单行表（id BOOLEAN PK 惯用法，system_transcode_config 迁移 00038 同款）；
--   · 双段凭据：channel_id（24 hex，不敏感，出现在 URL/日志/审计）+ access_key（43 字符
--     base64url，敏感，**库中只存 sha256 摘要**，明文仅在 enable/rotate 响应中出现一次）；
--   · 四路失效：TTL 到期（reaper/握手/心跳复核）/ 手动关闭 / rotate 踢连（4004）/
--     连续认证失败锁定（Valkey 侧计数，与本表无关）；
--   · 设计方案见 文档/转码远程调试功能设计方案_v1.0.md §10.1（文中编号 00038，实施期顺延为 00041）。
--
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS debug_channel (
    id               BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),  -- 单行表惯用法
    enabled          BOOLEAN NOT NULL DEFAULT false,
    channel_id       TEXT NOT NULL UNIQUE,
    key_digest       TEXT NOT NULL UNIQUE,        -- sha256(access_key) hex；明文永不入库
    created_by       UUID NOT NULL REFERENCES users(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL,
    last_connect_at  TIMESTAMPTZ,
    last_connect_ip  TEXT
);
COMMENT ON TABLE debug_channel IS '远程调试通道（全局单行，Job000121）；凭证明文仅在 enable/rotate 响应中出现一次，库中只存 sha256 摘要。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS debug_channel;
-- +goose StatementEnd
