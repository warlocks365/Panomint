-- T6.2 算力节点 agent：补齐节点表缺失的令牌哈希与状态列。
--
-- 背景（实测结论，勿删）：
--   compute_nodes 自 00007 建表以来**从未被任何 Go 代码读写过**（transcode_jobs.node_id 恒为 NULL，
--   「存储与算力分离」此前只是 DDL 里的一张空表），三个缺陷因此长期被掩盖，本迁移一并修掉：
--
--   1) 令牌明文列 agent_token（VARCHAR(256)）。
--      本交付项改为只存 sha256 哈希（agent_token_hash），校验时「先对来访明文求哈希、再按哈希查表」，
--      库中不存在可直接冒用的凭据。明文列**保留不删**（避免改动既有 DDL 结构），
--      但其存量值一律清空，且 Go 代码只读不写、不读不写。
--
--   2) 缺 updated_at 列，却挂着 set_updated_at_compute_nodes 触发器（00010 建立）。
--      触发器函数体为 NEW.updated_at = now()，而该表建表语句里没有 updated_at 列
--      （文档/数据库DDL_v1.1.md 第 545–559 行同样缺列，属上游 DDL 自身缺陷）。
--      后果是**任何** UPDATE compute_nodes 都会报：
--          ERROR: record "new" has no field "updated_at"
--      （已在测试库用事务内 INSERT + UPDATE 实测复现）。而心跳续期、状态上下线、令牌轮换
--      全部是 UPDATE —— 不补此列，T6.2 根本无法工作。这也解释了该表为何长期零引用。
--
--   3) 令牌轮换/过期无处落库。TDD §6.1 要求「agent_token 轮换（管理员触发）」，
--      增 agent_token_expires_at 承载有效期；NULL = 永不过期（当前默认，§6.1 未要求必过期）。
--      过期判定在**查询侧**做（internal/compute/token.go 的 TokenValid），不引入定时任务。
--
-- 影响范围：仅对 compute_nodes 增 3 列 + 建 1 个索引 + 清空明文令牌列；
--   该表当前 0 行；不修改任何既有列类型、不删除任何列、不删除任何行、不影响其它表。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS agent_token_hash VARCHAR(64);
ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS agent_token_expires_at TIMESTAMPTZ;
ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

COMMENT ON COLUMN compute_nodes.agent_token IS
  '已废弃：agent 接入令牌不再明文入库，改用 agent_token_hash（sha256 hex）。Go 代码不读不写此列。';
COMMENT ON COLUMN compute_nodes.agent_token_hash IS
  'agent 接入令牌的 sha256 hex（不存明文）；NULL = 该节点未启用 agent（local_gpu/cloud_gpu 登记时不发令牌）。';
COMMENT ON COLUMN compute_nodes.agent_token_expires_at IS
  'agent 令牌过期时刻；NULL = 永不过期。过期判定在查询侧完成（internal/compute/token.go TokenValid）。';
COMMENT ON COLUMN compute_nodes.updated_at IS
  '行更新时间，由触发器 set_updated_at_compute_nodes 自动维护（00010 建立该触发器时遗漏了本列，00018 补上）。';

-- 清空可能残留的明文令牌（当前表内 0 行，此处为幂等兜底）。
UPDATE compute_nodes SET agent_token = NULL WHERE agent_token IS NOT NULL;

-- 令牌校验按哈希定位，必须有索引（AgentAuth 中间件在每次心跳/拉取时都会走这里）。
CREATE INDEX IF NOT EXISTS idx_nodes_agent_token_hash ON compute_nodes(agent_token_hash);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_nodes_agent_token_hash;
ALTER TABLE compute_nodes DROP COLUMN IF EXISTS updated_at;
ALTER TABLE compute_nodes DROP COLUMN IF EXISTS agent_token_expires_at;
ALTER TABLE compute_nodes DROP COLUMN IF EXISTS agent_token_hash;
-- +goose StatementEnd
