-- Stage 3 结构化搜索（Job000001）：pg_trgm 扩展 + 三元组 GIN 索引
-- 用途：q 关键词（media.filename / media.place / tags.name）与 place 参数的三元组模糊匹配
-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_media_filename_trgm ON media USING GIN (filename gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_media_place_trgm    ON media USING GIN (place gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_tags_name_trgm      ON tags  USING GIN (name gin_trgm_ops);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_media_filename_trgm;
DROP INDEX IF EXISTS idx_media_place_trgm;
DROP INDEX IF EXISTS idx_tags_name_trgm;
-- 扩展保留不删：pg_trgm 可能被其他对象依赖，回滚仅撤索引
-- +goose StatementEnd
