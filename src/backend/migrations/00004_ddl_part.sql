-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE TYPE media_type  AS ENUM ('photo', 'video', '360');

CREATE TYPE media_space AS ENUM ('personal', 'shared');



CREATE TABLE media (

    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    type          media_type NOT NULL,

    space         media_space NOT NULL DEFAULT 'personal',

    owner_id      UUID NOT NULL REFERENCES users(id),

    path          TEXT NOT NULL,               -- 对象存储/NAS 内相对路径

    folder_path   TEXT,                        -- 保留原始目录结构（文件夹视图）

    filename      VARCHAR(512),

    taken_at      TIMESTAMPTZ,                 -- 拍摄时间（EXIF）

    imported_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    width         INTEGER,

    height        INTEGER,

    duration      INTEGER,                     -- 视频秒数

    codec         VARCHAR(32),

    container     VARCHAR(16),

    media_subtype TEXT[],                      -- live/portrait/panorama/raw/screenshot/burst...

    is_360        BOOLEAN NOT NULL DEFAULT false,

    projection    VARCHAR(32),                 -- equirectangular

    gps           geometry(Point, 4326),       -- WGS-84 经纬度（PostGIS 空间查询）

    place         VARCHAR(128),                -- 反地理编码地名

    hash          VARCHAR(64),                 -- pHash 去重

    duplicate_of   UUID REFERENCES media(id),  -- 指向保留原件

    thumbnail_sm  TEXT,

    thumbnail_md  TEXT,

    thumbnail_lg  TEXT,

    hls_master    TEXT,                        -- HLS master.m3u8 路径（自适应码率）

    filesize      BIGINT,

    -- 相机/EXIF 元数据

    camera_make   VARCHAR(128),

    camera_model  VARCHAR(128),

    lens_model    VARCHAR(128),

    focal_length  REAL,                        -- 焦距 mm

    aperture      REAL,                        -- 光圈 f 值

    iso           INTEGER,

    shutter_speed VARCHAR(32),                 -- 快门速度（如 "1/200"）

    exposure_bias REAL,                        -- 曝光补偿 EV

    -- 视频技术元数据

    fps           REAL,

    bitrate       INTEGER,                     -- 码率 bps

    hdr           BOOLEAN NOT NULL DEFAULT false,

    color_space   VARCHAR(32),                 -- bt709/bt2020/p3...

    video_preview_at REAL,                    -- 视频缩略图截取时间点（秒）

    -- 用户评价/评级

    rating        SMALLINT NOT NULL DEFAULT 0, -- 0-5 星

    -- 实况照片配对（Apple Live Photo: 照片+短视频配对）

    live_photo_pair_id UUID REFERENCES media(id),

    -- 软删/回收站

    deleted_at    TIMESTAMPTZ,                -- 非空=在回收站中（软删）

    embedding     VECTOR(512),                 -- 语义/聚类向量

    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),

    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()

);



CREATE INDEX idx_media_owner_space ON media(owner_id, space);

CREATE INDEX idx_media_taken ON media(taken_at DESC);

CREATE INDEX idx_media_folder ON media(folder_path);

CREATE INDEX idx_media_type ON media(type);

CREATE INDEX idx_media_hash ON media(hash) WHERE duplicate_of IS NULL;

CREATE INDEX idx_media_gps ON media USING gist (gps) WHERE gps IS NOT NULL;

CREATE INDEX idx_media_deleted ON media(deleted_at) WHERE deleted_at IS NOT NULL;  -- 回收站查询

CREATE INDEX idx_media_rating ON media(rating) WHERE rating > 0;

CREATE INDEX idx_media_live_pair ON media(live_photo_pair_id) WHERE live_photo_pair_id IS NOT NULL;

-- 向量索引（IVFFlat，需先 INSERT 后 CREATE INDEX 或设 probes）

CREATE INDEX idx_media_embedding ON media USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
-- +goose StatementEnd
