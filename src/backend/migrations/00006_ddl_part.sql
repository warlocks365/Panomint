-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE TYPE album_type AS ENUM ('manual', 'smart', 'shared', 'favorites');



CREATE TABLE albums (

    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name      VARCHAR(255) NOT NULL,

    description TEXT,                             -- 相册描述/备注

    type      album_type NOT NULL DEFAULT 'manual',

    space     media_space NOT NULL DEFAULT 'personal',

    owner_id  UUID NOT NULL REFERENCES users(id),

    query     JSONB,                           -- 智能相册条件（人物/标签/日期/地点...）

    cover_media_id UUID REFERENCES media(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_albums_owner ON albums(owner_id, space);



CREATE TABLE album_items (

    album_id  UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,

    media_id  UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,

    sort_key  INTEGER DEFAULT 0,

    PRIMARY KEY (album_id, media_id)

);



-- 相册评论（共享相册成员协作）

CREATE TABLE album_comments (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    album_id    UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,

    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    content     TEXT NOT NULL,

    parent_id   UUID REFERENCES album_comments(id) ON DELETE CASCADE,  -- 支持回复

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_album_comments_album ON album_comments(album_id, created_at DESC);



-- 共享空间（双空间模型；成员与权限由本系统独立管理，不依赖 DSM）

CREATE TABLE shared_space (

    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name      VARCHAR(255) NOT NULL,

    owner_id  UUID NOT NULL REFERENCES users(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE TABLE shared_space_members (

    space_id  UUID NOT NULL REFERENCES shared_space(id) ON DELETE CASCADE,

    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    role      VARCHAR(32) NOT NULL DEFAULT 'member', -- owner|manager|member|viewer

    PRIMARY KEY (space_id, user_id)

);



-- 文件夹视图（保留 NAS 目录结构）

CREATE TABLE folders (

    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    path      TEXT NOT NULL UNIQUE,

    name      VARCHAR(512),

    parent_id UUID REFERENCES folders(id) ON DELETE CASCADE

);

CREATE TABLE folder_media (

    folder_id UUID NOT NULL REFERENCES folders(id) ON DELETE CASCADE,

    media_id  UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,

    PRIMARY KEY (folder_id, media_id)

);

CREATE INDEX idx_folders_parent ON folders(parent_id);
-- +goose StatementEnd
