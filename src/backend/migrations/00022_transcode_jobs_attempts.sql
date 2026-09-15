-- T6.2：给转码任务加「尝试次数」计数，为节点侧的回收重派设上限。
--
-- 背景：上一轮给算力节点加了「僵死任务回收」（store.go 的 reclaimJobsSQL）——把心跳静默超时、
-- 或被管理员强制下线节点名下的 running 任务放回 pending，交给别的节点重跑。
-- 但回收**没有次数上限**：一个反复"领了任务就死"的节点（或在两台机器间反复横跳的节点）
-- 会让同一条任务被无限重派，每次白烧一遍算力，而在管理端看起来永远是"在处理中"。
--
-- 本迁移只加一列计数：
--   attempts 在**认领时**自增（pollJobsSQL），回收时**刻意不重置** —— 它记的是
--   "这条任务被领走过几次"，而不是"当前这一轮跑了多久"。于是上限的语义是
--   "最多被几个节点尝试过"，与"任务本身有多长"解耦（长转码不会被误判成重试过多）。
--
-- 达上限的任务由 exhaustJobsSQL 判 failed（在回收之后跑，见 Store.ReclaimStale）：
--   任务仍是 pending 且无人持有（node_id IS NULL）却已 attempts >= 上限 → 它再也不可能被认领
--   （认领条件含 attempts < 上限），不判失败就会永远卡在 pending，管理端看不出任何异常。
--
-- ⚠️ 与 transcodectl 的关系：控制端的 cmd/transcodectl 走 Valkey 队列、不经过认领语句，
--   因此**不增加 attempts**，本上限对它不生效（它的重试由队列退避负责，属既有行为）。
--   本列只服务于"节点认领 → 僵死回收 → 重派"这条链路。
--
-- 影响范围：仅对 transcode_jobs 增 1 列（NOT NULL DEFAULT 0）。存量行自动为 0，
--   于是历史任务在上限为 3 时仍有 3 次机会，不会被立刻判失败。
--   不改任何既有列/类型/索引，不删行。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE transcode_jobs ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;
COMMENT ON COLUMN transcode_jobs.attempts IS '被认领的次数（节点侧认领时自增，僵死回收时不重置）。达到上限后任务判 failed，见 internal/compute 的 exhaustJobsSQL。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE transcode_jobs DROP COLUMN IF EXISTS attempts;
-- +goose StatementEnd
