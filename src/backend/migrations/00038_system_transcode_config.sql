-- Job000120-r2（用户裁决 2026-09-25）：「自动 HLS 转码」从用户级偏好（user_ui_prefs.auto_transcode）
-- 上收为**系统级开关**（管理后台「转码」页签维护，全站生效）：
--
--   · 新建 system_transcode_config 单行表（singleton 主键 = 单行硬约束，
--     system_map_config 迁移 00023 同款先例），ON CONFLICT (singleton) 原子 upsert；
--   · DROP user_ui_prefs.auto_transcode——用户级取值作废（该语义上线数日、从未进正式
--     版本，无存量迁移价值），系统级默认 true = 存量行为不变。
--
-- 读侧语义（internal/transcode/sysconfig.go）：无行 = 默认开启；查询出错 fail-closed 由调用方 500。

-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS system_transcode_config (
    singleton      BOOLEAN PRIMARY KEY DEFAULT true CHECK (singleton),
    auto_transcode BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE system_transcode_config IS '转码系统级开关（单行）；auto_transcode=false 时 API 发起的转码 409（Job000120-r2，管理后台维护）。';

ALTER TABLE user_ui_prefs DROP COLUMN IF EXISTS auto_transcode;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_ui_prefs ADD COLUMN IF NOT EXISTS auto_transcode BOOLEAN NOT NULL DEFAULT TRUE;
DROP TABLE IF EXISTS system_transcode_config;
-- +goose StatementEnd
