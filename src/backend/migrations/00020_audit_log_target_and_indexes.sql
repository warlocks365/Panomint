-- Phase 5（精选第一项）：让审计日志真正可用 —— 补列 + 补查询索引。
--
-- 背景：audit_log 自 00002 建表以来**只有 DDL，没有任何写入代码，也没有查询端点**
--   （实测：库中 0 行；全仓 grep 无 audit_log 引用）。本次为它实现写入与查询，
--   但实测表结构不足以支撑「可过滤、可追责」的审计查询：
--     缺 target_type / target_id —— 无法回答"这条记录动的是哪个对象"，
--       而这是审计最常见的问句（"某用户被谁改过什么"）；
--     缺 user_agent —— TDD §8.2 的追溯口径要求 UA 可查（sessions 表早已有该列）；
--     只有 idx_audit_at(at DESC) —— 按 action / actor / target 过滤会全表扫。
--
-- ⚠️ 实测确认：库里 audit_log 的实际列是 (id, user_id, action, detail, ip, at)，
--   与 docker/db/init/01-schema.sql、migrations/00002_ddl_part.sql、
--   文档/数据库DDL_v1.1.md §2.4 **三处一致**（本表未出现历史上那类文档/库不一致）。
--   列名 user_id 保留不改（语义是"触发者"，Go 侧对外叫 actor_user_id）。
--
-- 本迁移**只新增列与索引**：不改任何既有列的类型、不重命名、不删列、不删行。
--   audit_log 是只增不改的账本 —— 动既有结构等于改写历史，这是审计表的大忌。
-- 三列全部可空（NULL 表示"没采到"），且都带 DEFAULT 语义上的"无值"，
--   故对既有行（当前 0 行，但生产可能有）无需回填，也不会因 NOT NULL 阻断写入。
--   审计写入本身是"尽力而为"的：任何一次写入失败都会被业务层忽略，
--   所以这里绝不能再引入一个能让 INSERT 失败的新约束。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_type VARCHAR(64);
COMMENT ON COLUMN audit_log.target_type IS '被操作对象的类型（user/share/media/role/setting/compute_node/audit_log ...）。NULL 表示无特定对象。归一化与校验见 internal/audit.NormalizeTargetType。';

ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS target_id VARCHAR(128);
COMMENT ON COLUMN audit_log.target_id IS '被操作对象的标识（本项目多为 UUID，故按 128 留余量）。不变量：非空时 target_type 必须非空，由 internal/audit.Entry.Normalize 强制。';

ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS user_agent TEXT;
COMMENT ON COLUMN audit_log.user_agent IS '触发者 UA（TDD §8.2 追溯口径）。完全由客户端控制，属不可信输入；写入侧已剔除控制字符并截断至 512 字符。';

-- 查询索引：管理端固定 `ORDER BY at DESC, id DESC`，三类过滤各配一条复合索引，
-- 让 "过滤 + 排序 + LIMIT" 走同一条索引。索引把 at DESC 写在后面，是因为
-- 过滤列等值匹配的基数更高（action/actor/target 通常远少于总行数）。
CREATE INDEX IF NOT EXISTS idx_audit_action_at ON audit_log(action, at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_actor_at ON audit_log(user_id, at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_target ON audit_log(target_type, target_id, at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_audit_target;
DROP INDEX IF EXISTS idx_audit_actor_at;
DROP INDEX IF EXISTS idx_audit_action_at;

ALTER TABLE audit_log DROP COLUMN IF EXISTS user_agent;
ALTER TABLE audit_log DROP COLUMN IF EXISTS target_id;
ALTER TABLE audit_log DROP COLUMN IF EXISTS target_type;
-- +goose StatementEnd
