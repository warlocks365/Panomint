-- Job000103 存储位置管理：命名物理存储根（位置名=容器内挂载目录名，Docker 映射名约束 slug 化，
-- 防路径穿越/非法挂载名）。media.location_id 可空 FK：NULL=默认存储根（存量行为不变），
-- 删除位置时媒体回默认（ON DELETE SET NULL，沿 00027 FK 口径）。

-- +goose Up
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

COMMENT ON TABLE storage_locations IS '命名物理存储根（Job000103）：name=容器内挂载目录名（slug 约束=Docker 映射名约束），media.location_id NULL=默认存储根';

-- +goose Down
ALTER TABLE media DROP COLUMN IF EXISTS location_id;
DROP TABLE IF EXISTS storage_locations;
