---
title: 数据库 DDL (v1.1)
---

<Heading id="kS2zoaxutQyh4ka1rcjqRD" level="1">
  全景相册系统 · 数据库 DDL（PostgreSQL 16 + pgvector）
</Heading>

<BlockQuote id="YBY2dWvv8JHdoyaAEBjRTu">
  <Paragraph id="h1xDfTPcRBfvJ5axQSqe5g">
    版本：v1.1 ｜ 日期：2026-08-24\
    配套：PRD v3.1 / TDD v1.1\
    引擎：PostgreSQL 16 + PostGIS 3.4 + pgvector 0.7\
    设计要点：独立后台账户（不对接 DSM）；媒体/向量/关系分离；面向时间轴与向量检索优化；软删回收站；EXIF/视频元数据完整落库
  </Paragraph>
</BlockQuote>

<Divider id="wCzVkZ748vEQzSTt5VJzMa" />

<Heading id="gn9xwTJItRmKz6DqkJt2dB" level="2">
  1. 初始化
</Heading>

<Code id="laKnu8VPqatNmR0lbHSEKu" language="sql">
  ```sql
  CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
  CREATE EXTENSION IF NOT EXISTS vector;          -- pgvector
  CREATE EXTENSION IF NOT EXISTS pg_trgm;         -- 模糊搜索
  CREATE EXTENSION IF NOT EXISTS postgis;         -- 空间索引/地理查询（地图模式 bbox/距离/聚合）
  ```
</Code>

<BlockQuote id="GMMCXazkzu8qaSQwP1cbMF">
  <Paragraph id="rfi2aaeJE8anZqptTdwmpl">
    <Mark bold>PostGIS 说明</Mark>：地图模式需要 `bbox` 空间查询、距离计算、地理聚合，这些功能依赖 PostGIS。`POINT` 是 PG 内置类型但无法做 `ST_Intersects`/`ST_DWithin` 等地理函数，故采用 `geometry(Point, 4326)` 类型（WGS-84 坐标系）。高德底图需做 WGS-84↔GCJ-02 转换在应用层处理（不落库 GCJ-02）。
  </Paragraph>
</BlockQuote>

<Paragraph id="zOwtvyr9iENyqNriIB4jlR">

</Paragraph>

<Divider id="F2QCGqwb3UserBgP6JtBEb" />

<Heading id="X2XyJHBWkH3QxIwltY5d3r" level="2">
  2. 账户与权限（独立后台，不对接 DSM）
</Heading>

<Code id="KWmou7e6Y5SUDawHJ1v2FZ" language="sql">
  ```sql
  -- 角色
  CREATE TABLE roles (
      id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
      name        VARCHAR(64) NOT NULL UNIQUE,
      description TEXT,
      created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- 角色权限（细粒度 RBAC）
  CREATE TABLE role_permissions (
      role_id    UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
      perm       VARCHAR(128) NOT NULL,           -- 例如 media:read, album:write, admin:users
      PRIMARY KEY (role_id, perm)
  );

  -- 用户（自建账户，不与 DSM 关联）
  CREATE TABLE users (
      id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
      email              VARCHAR(255) NOT NULL UNIQUE,
      display_name       VARCHAR(128),
      password_hash      VARCHAR(255) NOT NULL,    -- bcrypt
      role_id            UUID NOT NULL REFERENCES roles(id),
      mfa_secret         VARCHAR(255),            -- TOTP 密钥（启用 2FA 后）
      mfa_enabled        BOOLEAN NOT NULL DEFAULT false,
      app_password_hash  VARCHAR(255),            -- API 专用应用密码
      status             VARCHAR(16) NOT NULL DEFAULT 'active', -- active|disabled|locked
      last_session       TIMESTAMPTZ,
      created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- 会话（可监控/吊销）
  CREATE TABLE sessions (
      id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
      user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
      refresh_token_hash VARCHAR(255) NOT NULL,
      ip          VARCHAR(64),
      user_agent  TEXT,
      expires_at  TIMESTAMPTZ NOT NULL,
      revoked     BOOLEAN NOT NULL DEFAULT false,
      created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- 审计日志
  CREATE TABLE audit_log (
      id         BIGSERIAL PRIMARY KEY,
      user_id    UUID REFERENCES users(id),
      action     VARCHAR(128) NOT NULL,           -- login, share_create, media_delete...
      detail     JSONB,
      ip         VARCHAR(64),
      at         TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  CREATE INDEX idx_audit_at ON audit_log(at DESC);
  ```
</Code>

<Divider id="mZeQ2bJHzxNWsBKydnFdkR" />

<Heading id="vIop3vbK9Uk5B3kcewV71t" level="2">
  2.5 用户 UI 偏好与地图配置
</Heading>

<BlockQuote id="r5p3CjRMKX2wpGZTNYAozM">
  <Paragraph id="3hXeLTFcPgGp675ugGTMUO">
    对应 PRD §6.6 地图模式（滑块位置/筛选栏侧可配置、中外国界可配）。
  </Paragraph>
</BlockQuote>

<Code id="yh9MLdzBGG3Pu1wWTkvzvK" language="sql">
  ```sql
  -- 用户级 UI 偏好（地图模式布局）
  CREATE TABLE user_ui_prefs (
      user_id             UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
      map_slider_pos      VARCHAR(8)  NOT NULL DEFAULT 'bottom',  -- top|bottom
      map_filter_side     VARCHAR(8)  NOT NULL DEFAULT 'left',     -- left|right
      map_default_provider VARCHAR(16) NOT NULL DEFAULT 'auto',    -- auto|amap|osm
      map_default_zoom    INTEGER,
      created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
      updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- 系统级地图配置（管理员可配；高德 API key 等敏感信息加密存储）
  CREATE TABLE system_map_config (
      id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
      china_provider      VARCHAR(16) NOT NULL DEFAULT 'amap',  -- amap|null
      china_api_key_enc   TEXT,                                -- 加密存储的高德 key
      china_tile_url      TEXT,                               -- 高德瓦片模板(可选自定义)
      intl_provider       VARCHAR(16) NOT NULL DEFAULT 'osm',  -- osm|maptiler
      intl_tile_url       TEXT,                               -- 国际瓦片/样式 URL
      updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
  );

  -- 地理编码缓存（避免重复调用外部 API）
  CREATE TABLE geo_cache (
      key          VARCHAR(128) PRIMARY KEY,   -- "rev:lat,lon" 或 "fwd:query"
      provider     VARCHAR(16),
      result       JSONB NOT NULL,
      expires_at   TIMESTAMPTZ
  );
  CREATE INDEX idx_geocache_exp ON geo_cache(expires_at);
  ```
</Code>

<Divider id="FQlXPtDJXx8WWHCTUl718q" />

<Heading id="RKOKSbfJaaw15iwFYz2i3b" level="2">
  3. 媒体核心
</Heading>

<Code id="0UQiVSsN9q3gaf0Oooxhsf" language="sql">
  ````sql
  ```sql
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
        hash          VARCHAR(64),                 -- sha256 内容哈希去重（64 hex；pHash 相似图检测属 P1 另设）
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
      ```
  ````
</Code>

<BlockQuote id="TzqXSSYXB4niWnG0rlyKUr">
  media 表没有 space\_id 列：空间归属由 media.space（media\_space 枚举 personal|shared）表达；共享媒体的可见性按 shared\_space\_members 的成员关系取并集（任一共享空间成员即可见），不依赖 media 上的空间外键。
</BlockQuote>

<Divider id="DSKeRw7l7tnE6AfntXCJ0S" />

<Heading id="5ktLpuZaF5nGtlar4GxfOI" level="2">
  4. 人脸 / 人物 / 标签
</Heading>

<Code id="EUS7b8HzKSj1Kb6ufjISLj" language="sql">
  ```sql
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
      embedding   VECTOR(128),
      created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
  );
  CREATE INDEX idx_faces_person ON faces(person_id);
  CREATE INDEX idx_faces_cluster ON faces(cluster_id);
  CREATE INDEX idx_faces_media ON faces(media_id);
  CREATE INDEX idx_faces_embedding ON faces USING hnsw (embedding vector_cosine_ops);

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
  ```
</Code>

<Divider id="YdPz3flAbP1lXd41SjdstN" />

<Heading id="fFi3FfwMVy0dVfShfGyk0Q" level="2">
  5. 相册 / 共享空间 / 文件夹
</Heading>

<Code id="vL6vWOdDnbIUa8PZVsU5qw" language="sql">
  ```sql
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
  ```
</Code>

<BlockQuote id="2Oefz8XnKZ78mIgtlOvfv1">
  收藏模型澄清：不存在 media.is\_favorite / albums.kind='favorites' 之类的字段。「收藏」= album\_type='favorites' 的相册 + album\_items 成员关系，检索/时间轴一律用 EXISTS(SELECT 1 FROM album\_items ai JOIN albums a ON a.id=ai.album\_id WHERE ai.media\_id=m.id AND a.type='favorites') 判定，不使用 kind。
</BlockQuote>

<Divider id="81x3GGGXHa80sdLXoSOVO9" />

<Heading id="e5cgSXKoZ2JzHCdB1RZbzc" level="2">
  6. 分享 / 回忆 / 任务
</Heading>

<Code id="NuFg3y47cx5HZ2LlLiYswD" language="sql">
  ````sql
  ```sql
      CREATE TYPE share_kind AS ENUM ('album', 'media');
      
      CREATE TABLE share_links (
        id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        token           VARCHAR(64) NOT NULL UNIQUE,
        kind            share_kind NOT NULL,
        target_id       UUID NOT NULL,             -- album_id 或 media_id
        owner_id        UUID NOT NULL REFERENCES users(id),
        title           VARCHAR(255),
        expire_at       TIMESTAMPTZ,
        password_hash   VARCHAR(255),
        allow_download  BOOLEAN NOT NULL DEFAULT false,
        is_wechat       BOOLEAN NOT NULL DEFAULT false, -- 微信 H5 链接（非整文件）
        max_views       INTEGER,
        access_count    INTEGER NOT NULL DEFAULT 0,    -- 累计访问次数
        created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      CREATE INDEX idx_share_token ON share_links(token);
      CREATE INDEX idx_share_expire ON share_links(expire_at);
      
      CREATE TABLE share_access_log (
        id         BIGSERIAL PRIMARY KEY,
        share_id   UUID NOT NULL REFERENCES share_links(id) ON DELETE CASCADE,
        ip         VARCHAR(64),
        user_agent TEXT,
        at         TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      CREATE INDEX idx_sharelog_share ON share_access_log(share_id);
      
      -- 回忆影片（可选模块）
      CREATE TABLE memories (
        id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        title            VARCHAR(255),
        criteria         JSONB,                    -- 主题选取条件
        video_asset_path TEXT,
        owner_id         UUID NOT NULL REFERENCES users(id),
        created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      
      -- 后台任务（索引/转码）
      CREATE TABLE index_jobs (
        id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        kind        VARCHAR(32) NOT NULL,          -- full|incremental
        user_id     UUID REFERENCES users(id),     -- 触发者
        status      VARCHAR(16) NOT NULL DEFAULT 'pending', -- pending|running|done|failed
        total       INTEGER DEFAULT 0,
        processed   INTEGER DEFAULT 0,
        started_at  TIMESTAMPTZ,
        finished_at TIMESTAMPTZ,
        created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      ```
  ````
</Code>

<Divider id="V3yM0flVLM0VqWtqGW9hU4" />

<Heading id="slRdg9CTrPyKiB8YIJIT4U" level="2">
  6.5 算力节点与带宽配置（多形态 GPU 接入）
</Heading>

<BlockQuote id="KzRNtTuTKOd4T49uYi7b8L">
  <Paragraph id="1mCeYFCV37bRZXtureczRg">
    对应 TDD §6「算力节点多形态」。算力节点经 agent 注册/心跳上报能力，调度器按能力匹配派发转码/AI 任务。
  </Paragraph>
</BlockQuote>

<Divider id="mj91wcaFur7DqUTL8zteTw" />

<Heading id="dtdUvjl4A26LQEAkSfNWlA" level="2">
  7. 初始数据（建议）
</Heading>

<Code id="YFTsatlktoPMMlhBg7XZRe" language="sql">
  ```sql
  INSERT INTO roles (name, description) VALUES
    ('owner',  '所有者，全部权限'),
    ('admin',  '管理员，用户/系统配置'),
    ('member', '普通成员，个人空间+共享空间贡献'),
    ('viewer', '访客/只读');

  INSERT INTO role_permissions (role_id, perm)
    SELECT id, 'media:read' FROM roles WHERE name='viewer';
  INSERT INTO role_permissions (role_id, perm)
    SELECT id, v FROM roles r, unnest(ARRAY[
      'media:read','media:write','album:read','album:write','share:create'
    ]) v WHERE r.name='member';
  INSERT INTO role_permissions (role_id, perm)
    SELECT id, v FROM roles r, unnest(ARRAY[
      'media:*','album:*','share:*','space:*','admin:users','admin:system'
    ]) v WHERE r.name IN ('owner','admin');
  ```
</Code>

<Divider id="rFfikOojTHLg2924udBVLi" />

<Heading id="lYFyk8WSVNGnhTO3W6SCvB" level="2">
  7.5 updated\_at 自动更新触发器
</Heading>

<BlockQuote id="93uXEvFxtNkuycPh3789Yn">
  <Paragraph id="hd3cflryqM9WCxUInbn4zP">
    所有含 `updated_at` 字段的表在 UPDATE 时自动刷新时间戳。
  </Paragraph>
</BlockQuote>

<Code id="CWLSaeBgElFNsim18nx7Da" language="sql">
  ```sql
  CREATE OR REPLACE FUNCTION trigger_set_updated_at()
  RETURNS TRIGGER AS $$
  BEGIN
      NEW.updated_at = now();
      RETURN NEW;
  END;
  $$ LANGUAGE plpgsql;

  -- 为所有含 updated_at 的表创建触发器
  CREATE TRIGGER set_updated_at_users BEFORE UPDATE ON users
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_media BEFORE UPDATE ON media
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_albums BEFORE UPDATE ON albums
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_ui_prefs BEFORE UPDATE ON user_ui_prefs
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_map_config BEFORE UPDATE ON system_map_config
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_compute_nodes BEFORE UPDATE ON compute_nodes
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  CREATE TRIGGER set_updated_at_bandwidth BEFORE UPDATE ON bandwidth_profiles
      FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
  ```
</Code>

<Divider id="pHa7EYG1jadh4JtWvwJoeb" />

<Heading id="GBDO55f3LmNe6opqzXQe5j" level="2">
  8. 维护说明
</Heading>

<BulletedList id="3vsSlgV9jejoZIewCd74Ox">
  向量索引 `ivfflat`：建表并灌入数据后再 `CREATE INDEX`；查询前 `SET ivfflat.probes = 10;`。
- 算力节点表 `compute_nodes`：**必须含 `updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`**。本文件早先的建表块漏了该列，却在触发器段为它建了 `set_updated_at_compute_nodes`；触发器函数体执行 `NEW.updated_at = now()`，缺列会导致**任何 UPDATE 都报 `record "new" has no field "updated_at"`**（已在库上实测复现）。库侧已由迁移 `migrations/00018_compute_node_agent.sql` 补齐；同时该迁移把 agent 令牌改为只存 sha256（`agent_token_hash`）并新增 `agent_token_expires_at`。
- 人脸向量索引 `faces.embedding`：自迁移 `migrations/00014_faces_cluster.sql` 起，`faces.embedding` 已由 `VECTOR(512)` + ivfflat 改为 **`VECTOR(128)` + HNSW**；`media.embedding`（CLIP 语义向量，512 维）**仍用 ivfflat，未受影响**。改用 HNSW 的原因：HNSW 增量插入无需「训练」，在小数据量与高维下召回更稳定，更适合持续追加的人脸库；且 128 维已不再适配原 `ivfflat ... WITH (lists = 50)` 配置。**注意：`SET ivfflat.probes`（以及 ivfflat 的 `lists`）仅对 ivfflat 索引生效，对人脸 HNSW 索引不适用**；HNSW 的检索质量由建索引参数（`m`、`ef_construction`）与查询期 `hnsw.ef_search` 控制。
</BulletedList>

<BulletedList id="yoY5J8plqoHzaSwbIC3QDv">
  <Mark bold>空间索引（PostGIS）</Mark>：`gps` 字段为 `geometry(Point, 4326)`（WGS-84 坐标系）。bbox 查询用 `ST_MakeEnvelope` + `ST_Intersects`；距离查询用 `ST_DWithin`。高德底图显示需在应用层做 WGS-84→GCJ-02 转换（落库保持 WGS-84）。
</BulletedList>

<BulletedList id="pathW9FPPBFJ9eHBBrnUru">
  `deleted_at` 非空表示在回收站中（软删）；正常查询需 `WHERE deleted_at IS NULL`。
</BulletedList>

<BulletedList id="zvqKzNxxw6bvZIzwJFKcHc">
  大表分区可按 `taken_at` 按月分区（>1 亿行时）。
</BulletedList>

<BulletedList id="9F3a0rqiLCmaQy6ddyOsSE">
  备份：`pg_dump` + 原文件/对象存储快照；与 PRD §11 风险对策一致。
</BulletedList>

<Divider id="uqYvCkQnbY6d2YIXPi70Xl" />

<Paragraph id="B1glDi34wifu3YpFqpDjis">
  <Mark italic>文档结束（DDL v1.1）。接口契约见《API 详细契约.md》。商业化许可证合规矩阵见 PRD §12.2。</Mark>
</Paragraph>

<Code id="JhUbFjNh8T7WdV0FnnMu3N" language="sql">
  ````sql
  ```sql
      -- 算力节点（本地 GPU / 云 GPU / 本地网络第三方 GPU 主机）
      CREATE TABLE compute_nodes (
        id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        name           VARCHAR(128) NOT NULL,
        kind           VARCHAR(16) NOT NULL,         -- local_gpu|cloud_gpu|lan_agent
        host           VARCHAR(256),                 -- IP/域名（lan_agent 用内网地址）
        agent_token    VARCHAR(256),                 -- ⚠️ 已废弃（00018）：令牌改为只存 sha256 于 agent_token_hash，本列不再写入。仅因「不改既有列」保留
        codecs         VARCHAR(64) NOT NULL DEFAULT 'h264',  -- 支持编码: h264|hevc|av1
        has_nvenc      BOOLEAN NOT NULL DEFAULT false,
        vram_mb        INTEGER,
        concurrency    INTEGER NOT NULL DEFAULT 1,
        status         VARCHAR(16) NOT NULL DEFAULT 'offline',  -- online|busy|offline
        last_heartbeat TIMESTAMPTZ,
        created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
        updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()   -- 00018 补：本表挂了 set_updated_at_compute_nodes 触发器，
                                                          -- 触发器函数体赋值 NEW.updated_at，缺此列则任何 UPDATE 报
                                                          -- record "new" has no field "updated_at"（实测复现）
      );
      CREATE INDEX idx_nodes_status ON compute_nodes(status);
      
      -- 带宽配置（手动指定 + 自测结果，用于 ABR 档位推荐）
      CREATE TABLE bandwidth_profiles (
        id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        scope       VARCHAR(16) NOT NULL DEFAULT 'global',  -- global|share_token
        ref_id      UUID,                                  -- scope=share_token 时填 share_links.id
        up_kbps     INTEGER,                               -- 上行（手动或自测）
        down_kbps   INTEGER,                               -- 下行（手动或自测）
        source      VARCHAR(16) NOT NULL DEFAULT 'manual', -- manual|self_test
        updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      
      -- 带宽自测记录
      CREATE TABLE bandwidth_tests (
        id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        profile_id  UUID REFERENCES bandwidth_profiles(id) ON DELETE CASCADE,
        up_kbps     INTEGER,
        down_kbps   INTEGER,
        latency_ms  INTEGER,
        measured_at TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      CREATE INDEX idx_bwtest_profile ON bandwidth_tests(profile_id, measured_at);
      CREATE TABLE transcode_jobs (
        id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
        media_id    UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,
        node_id     UUID REFERENCES compute_nodes(id),  -- 处理该任务的算力节点
        kind        VARCHAR(32) NOT NULL,          -- thumbnail|hls|memories
        status      VARCHAR(16) NOT NULL DEFAULT 'pending',
        profile     VARCHAR(32),                   -- 1080p|2k|4k
        result_path TEXT,
        created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
      );
      CREATE INDEX idx_transcode_media ON transcode_jobs(media_id, status);
      ```
  ````
</Code>
