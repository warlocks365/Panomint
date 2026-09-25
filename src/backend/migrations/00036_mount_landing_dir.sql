-- Job000118 S2 挂载语义目录：storage_mounts.landing_dir = 导入落点（相对媒体库根的
-- 虚拟语义目录）。改造动机：旧硬编码 `_imports/<id8>` 是技术名、空挂载目录在
-- 「文件夹」页签不可见（folder_path GROUP BY 与 folder_dirs 两来源均不沾）。
-- 落点语义化后：创建时 MkdirAll(MEDIA_ROOT/landing_dir) + 注册 folder_dirs——
-- 空目录立即可见（folder_dirs 存在即目录）；扫描天然可及（MEDIA_ROOT 内）。
-- 存量回填 `'_imports/'||left(id::text,8)` = 旧 syncLocalDir/mountKey 行为，已导入
-- 媒体的 folder_path 前缀与新值逐字节一致 → 行为不变、零迁移媒体。
-- 约束：创建后不可改（PATCH 不暴露本列；前端创建后锁定输入）。

-- +goose Up
ALTER TABLE storage_mounts ADD COLUMN IF NOT EXISTS landing_dir TEXT;

UPDATE storage_mounts SET landing_dir = '_imports/' || left(id::text, 8) WHERE landing_dir IS NULL;

ALTER TABLE storage_mounts ALTER COLUMN landing_dir SET NOT NULL;

COMMENT ON COLUMN storage_mounts.landing_dir IS '导入落点=媒体库根下语义目录（Job000118）：相对路径、逐段 slug、创建后不可改；worker 落地目录=MEDIA_ROOT/landing_dir，扫描前缀同值；存量回填 _imports/<id8>=旧行为';

-- +goose Down
ALTER TABLE storage_mounts DROP COLUMN IF EXISTS landing_dir;
