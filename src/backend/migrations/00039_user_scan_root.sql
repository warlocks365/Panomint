-- Job000123（用户裁决 2026-09-25）：管理员可为每个普通账号分配**扫描根目录**——
-- 该账号扫描导入/目录树浏览/任务查询一律被钳制在 scan_root 之内（fail-closed），
-- 未分配（NULL）的账号没有任何扫描权限（403 SCAN_ROOT_REQUIRED）。
--
-- 取值语义：
--   NULL           = 未分配 → 成员侧扫描入口一律 403（区别于「根目录就是媒体根」）
--   ''（空串）     = 已分配，根目录 = MEDIA_ROOT 本身
--   'photos/trip'  = 已分配，根目录 = MEDIA_ROOT/photos/trip（相对斜杠路径，存储前 Clean）
--
-- 为什么是 VARCHAR 而非外键/枚举：根目录是文件系统路径，由 admin 端点写入前经
-- dirscope 解析 + os.Stat 真实目录校验（含 _imports 保留段黑名单），DB 层只做承载。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE users ADD COLUMN IF NOT EXISTS scan_root VARCHAR(512);
COMMENT ON COLUMN users.scan_root IS '扫描根目录（Job000123）：相对 MEDIA_ROOT 的斜杠路径；NULL=未分配（成员无扫描权限），空串=整个媒体根';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE users DROP COLUMN IF EXISTS scan_root;
-- +goose StatementEnd
