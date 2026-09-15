-- T6.2 算力节点 agent 缺陷修复：引入「状态锁」，区分「初始 offline」与「管理员强制 offline」。
--
-- 缺陷（已在测试服实测复现）：
--   新登记的节点永远无法变为 online。三条原因叠加：
--     1) Register 不写 status，落库取 DDL 默认值 'offline'；
--     2) EffectiveStatus 把「存储态 offline」当作最高优先级，即便心跳新鲜也判 offline；
--     3) heartbeatSQL 只续 last_heartbeat，**不更新 status** —— status 永远停在 'offline'。
--   问题本质：「初始 offline」与「管理员强制下线」共用了同一个值，无法区分。
--
-- 修法：新增 status_locked 作为状态锁。
--   status_locked = false（默认）→ status 由心跳自治：心跳把未锁定的 status 置 online/busy；
--   status_locked = true          → 管理员显式置过状态，心跳不得改写（「强制上/下线」语义保留）。
--   管理端显式传 status='online' 时解除锁定（status_locked=false），交还给心跳。
--
-- 影响范围：仅对 compute_nodes 增 1 列（NOT NULL DEFAULT false）。存量行自动为 false = 未锁定，
--   语义与「心跳可自治」一致，不会把既有节点误判为被强制下线；不修改任何既有列与类型、不删列、不删行。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE compute_nodes ADD COLUMN IF NOT EXISTS status_locked BOOLEAN NOT NULL DEFAULT false;
COMMENT ON COLUMN compute_nodes.status_locked IS '管理员显式置过状态时为 true（强制上下线）；false 时心跳可自由改写 status。仅新增列，不改既有列与类型。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE compute_nodes DROP COLUMN IF EXISTS status_locked;
-- +goose StatementEnd
