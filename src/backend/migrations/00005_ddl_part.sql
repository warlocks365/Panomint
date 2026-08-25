-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE TABLE people (

    id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name      VARCHAR(128),

    hidden    BOOLEAN NOT NULL DEFAULT false,

    is_pet    BOOLEAN NOT NULL DEFAULT false,

    cover_media_id UUID REFERENCES media(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_people_name ON people(name);



CREATE TABLE faces (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    media_id    UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,

    person_id   UUID REFERENCES people(id) ON DELETE SET NULL,

    cluster_id  VARCHAR(64),                   -- 聚类临时 ID

    bbox        BOX,                           -- 人脸框

    is_pet      BOOLEAN NOT NULL DEFAULT false,

    confidence  REAL,

    embedding   VECTOR(512),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_faces_person ON faces(person_id);

CREATE INDEX idx_faces_cluster ON faces(cluster_id);

CREATE INDEX idx_faces_media ON faces(media_id);

CREATE INDEX idx_faces_embedding ON faces USING ivfflat (embedding vector_cosine_ops) WITH (lists = 50);



CREATE TABLE tags (

    id       UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name     VARCHAR(128) NOT NULL,

    kind     VARCHAR(16) NOT NULL DEFAULT 'user', -- user | ai

    confirmed BOOLEAN NOT NULL DEFAULT true,

    color    VARCHAR(7),                           -- 标签颜色 #RRGGBB

    UNIQUE (name, kind)

);

CREATE TABLE media_tags (

    media_id UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,

    tag_id   UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,

    PRIMARY KEY (media_id, tag_id)

);

CREATE INDEX idx_mediatags_tag ON media_tags(tag_id);
-- +goose StatementEnd
