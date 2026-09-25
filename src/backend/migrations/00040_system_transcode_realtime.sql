-- Job000124（2026-09-25）：管理后台「转码」页签新增**播放时自动转码（实时转码）**开关。
--
--   · system_transcode_config 增列 realtime_transcode（singleton 单行表上扩展，迁移 00038 同款）；
--   · 与既有 auto_transcode（总闸门）是 **AND 关系**：有效自动触发 = auto_transcode AND realtime_transcode；
--     auto_transcode 的 409/fail-closed/CLI 例外语义全部不动；
--   · DEFAULT FALSE：存量部署升级后行为逐字节不变（仍手动发起转码）。
--
-- 设计方案见 文档/设计方案_播放实时转码_Job000124.md §1.3/§1.5。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE system_transcode_config
    ADD COLUMN IF NOT EXISTS realtime_transcode BOOLEAN NOT NULL DEFAULT FALSE;
COMMENT ON COLUMN system_transcode_config.realtime_transcode IS '播放时自动转码开关（Job000124）：true=播放无 HLS 视频自动发起转码；与 auto_transcode 为 AND 关系；默认 false 存量行为不变。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE system_transcode_config DROP COLUMN IF EXISTS realtime_transcode;
-- +goose StatementEnd
