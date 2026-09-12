-- Phase 4 标签：AI 自动打标 + 逐图人工确认
-- 背景：tags/media_tags 已存在（00005），但 media_tags 仅 (media_id,tag_id)，
--       无法表达「这张图上的这个 AI 标签是否已被人工确认」「置信度」「来源」。
-- 决策（用户拍板，双写）：
--   · tags.confirmed 保留为**粗粒度审阅标记**（该标签名是否被人工审阅过）；
--   · media_tags 新增**逐图态** confirmed/confidence/origin，表达关联级事实。
-- 兼容：ADD COLUMN ... NOT NULL DEFAULT true → 既有（手动）关联自动视为已确认。
-- +goose Up
-- +goose StatementBegin
ALTER TABLE media_tags
    ADD COLUMN IF NOT EXISTS confirmed  BOOLEAN NOT NULL DEFAULT true, -- 该关联是否已人工确认
    ADD COLUMN IF NOT EXISTS confidence REAL,                          -- AI 置信度/相似度（手动为 NULL）
    ADD COLUMN IF NOT EXISTS origin     VARCHAR(16);                   -- user|ai|heuristic（历史行为 NULL）

-- (media_id) 已由主键 (media_id,tag_id) 前缀覆盖，(tag_id) 已有 idx_mediatags_tag → 无需重建。
-- 待确认 AI 标签的清扫/预览查询走部分索引，避免全表扫描。
CREATE INDEX IF NOT EXISTS idx_mediatags_pending_ai
    ON media_tags (media_id) WHERE origin = 'ai' AND confirmed = false;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_mediatags_pending_ai;
ALTER TABLE media_tags
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS confidence,
    DROP COLUMN IF EXISTS confirmed;
-- +goose StatementEnd
