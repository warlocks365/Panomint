-- T6.2 收尾：删除 compute_nodes 上的废弃明文令牌列 agent_token，偿还安全欠债。
--
-- 背景（承接 00018）：
--   00018 把 agent 接入令牌改为只存 sha256（agent_token_hash）+ 有效期
--   （agent_token_expires_at），并把明文列 agent_token 的存量值一律清空；
--   当时为了「不改既有 DDL 结构」选择**保留列不删**——现在把这条尾巴收掉。
--
-- 为什么现在删是安全的：
--   1) 存量值已在 00018 清空（`UPDATE ... SET agent_token = NULL`），库中不存在可用明文；
--   2) 全仓 Go 代码只读不写、不读不写：insertNodeSQL / nodeCols / 各 UPDATE 均不含该列
--      （由 store_test.go 的 TestInsertSQLMustNotStorePlaintextToken /
--        TestAllNodeSQLMustNotStorePlaintextToken / TestNodeColsHasNoPlaintextToken 钉死）；
--      令牌校验走「先对来访明文求 sha256 → 再按哈希查表」（internal/compute/token.go）。
--   3) 该列是**永久负债**：即便当前值为 NULL，只要这一列还在，任何一行 compute_nodes 的
--      审计副本、pg_dump 备份、只读从库快照里都可能残留一份**可直接冒用**的凭据；
--      而哈希列（agent_token_hash）被拖走也无法反推出明文，风险等级完全不同。
--      把列本身删掉，才是从根上消除这类泄漏面，而不是依赖"记得清空"。
--
-- ⚠️ 数据不可逆：本迁移对**数据**是不可逆的（列一旦删除，列上曾被写入的明文无法恢复）；
--   但对**结构**可逆——Down 段会重建该列（无值）。这正是"Down 只恢复 schema、不回填数据"的原因。
--
-- 影响范围：仅对 compute_nodes 删 1 列；不修改任何其它列/类型/索引，不删行。
--   Down 重建的列可空、无默认、无约束，不会让任何未来的 INSERT 失败。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE compute_nodes DROP COLUMN IF EXISTS agent_token;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 仅恢复结构，**不、也无法**恢复数据：被删列的明文值无法从任何地方还原，
-- 这里只重建一个恒为 NULL 的空列（可空 VARCHAR(255)，无 NOT NULL、无默认、无约束，
-- 因此不会阻断任何后续 INSERT）。若真需要凭据，应由管理端轮换生成新令牌（写 agent_token_hash）。
ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS agent_token VARCHAR(255);
-- +goose StatementEnd
