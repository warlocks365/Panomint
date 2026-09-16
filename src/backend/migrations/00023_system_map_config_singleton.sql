-- system_map_config 是**单行**系统配置（DDL §2.5 与 internal/geo/mapconfig.go 都按单行使用），
-- 但表上只有 PRIMARY KEY (id)，**没有任何约束保证"只有一行"**：
--
--   · 读侧用 `ORDER BY updated_at DESC LIMIT 1` —— 这是"最近改的那条生效"的**隐式约定**；
--     一旦出现两行，"哪条生效"就取决于 updated_at 的精度与写入顺序，极难排查。
--   · 写侧此前无人实现（实测 0 行）；本轮新增 `PUT /admin/map-config`，
--     必须让"单行"成为**数据库级事实**，否则并发两次 PUT 可能留下两行。
--
-- 修法：加一个恒为 true 的 singleton 列并建唯一索引 —— 单行成为硬约束，
-- 写侧即可用 `INSERT ... ON CONFLICT (singleton) DO UPDATE` 做原子 upsert
-- （不必再"先查再改"，那在并发下会竞态）。
--
-- 存量归一：本表自建表起无任何写入代码，但仍先删掉多余行再建唯一索引
-- （唯一索引建在重复数据上会直接失败）。
-- 影响范围：仅 system_map_config 增 1 列 + 1 个唯一索引；不改既有列/类型，不删任何有效数据。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE system_map_config ADD COLUMN IF NOT EXISTS singleton BOOLEAN NOT NULL DEFAULT true;
COMMENT ON COLUMN system_map_config.singleton IS '恒为 true，仅用于配合唯一索引把本表钉成单行配置（见迁移 00023）。';

-- 保留最近更新的一行，其余删除（空表时是无操作）
DELETE FROM system_map_config
WHERE id NOT IN (SELECT id FROM system_map_config ORDER BY updated_at DESC, id LIMIT 1);

CREATE UNIQUE INDEX IF NOT EXISTS uq_system_map_config_singleton ON system_map_config(singleton);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS uq_system_map_config_singleton;
ALTER TABLE system_map_config DROP COLUMN IF EXISTS singleton;
-- +goose StatementEnd
