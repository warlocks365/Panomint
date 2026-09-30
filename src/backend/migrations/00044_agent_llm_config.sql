-- Job000140 Phase 2：Agent 语义接口 LLM 上游配置（单行表惯用法，同 00038/00041）。
--
--   · baseURL/model 为非敏感运维配置；api_key **列级加密**（AES-256-GCM，密钥来自
--     env STORAGE_CIPHER_KEY，与 storage 挂载凭据同体系）——明文只存在于保存请求的
--     内存与后端代理转发瞬间，绝不入库明文、绝不通过 API 回显（只回 has_key+尾 4 位）；
--   · 未配置密钥环境（STORAGE_CIPHER_KEY 缺失）时保存带 key 的配置 FailClosed（400）；
--   · 单行表：全局一份上游配置（admin:system 管理）。
--
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS agent_llm_config (
    id           BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    base_url     TEXT NOT NULL DEFAULT '',
    model        TEXT NOT NULL DEFAULT '',
    api_key_enc  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT false,
    updated_by   UUID REFERENCES users(id),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE agent_llm_config IS 'Agent 语义接口 LLM 上游配置（全局单行，Job000140）；api_key 列级加密，明文永不入库、永不回显。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS agent_llm_config;
-- +goose StatementEnd
