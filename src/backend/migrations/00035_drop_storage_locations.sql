-- Job000118 S1 存储位置功能移除：media.location_id 自 00034 落地后写入侧零消费
--（index/upload/batch 三条 INSERT 均不含该列）、读取侧仅 locations.go 自身两条 COUNT、
-- 线上 storage_locations 表 0 行 / media.location_id 非 NULL 0 行（2026-09-25 实测）——
-- 属从未启用的命名标签，整功能移除（后端 handler+路由+前端面板+契约 §18）。
-- 影响：无存量数据（预检 count=0）；down 段完整回放 00034 结构。

-- +goose Up
DROP INDEX IF EXISTS idx_media_location_id;
ALTER TABLE media DROP COLUMN IF EXISTS location_id;
DROP TABLE IF EXISTS storage_locations;

-- +goose Down
CREATE TABLE storage_locations (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT storage_locations_name_key UNIQUE (name),
    CONSTRAINT storage_locations_name_slug CHECK (name ~ '^[a-z][a-z0-9-]{0,31}$')
);
CREATE INDEX IF NOT EXISTS idx_storage_locations_name ON storage_locations(name);
ALTER TABLE media
    ADD COLUMN IF NOT EXISTS location_id UUID REFERENCES storage_locations(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_media_location_id ON media(location_id);
