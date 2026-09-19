-- embedding 扫描标记 —— 失败收敛（审查 P1-01）。
--
-- 背景：embedding 是四条清扫管线里唯一没有扫描标记列的。对照：
--   media.faces_scanned_at（00014）/ tags_scanned_at（00017）/ phash_scanned_at（00024）
-- 三者都明确解决过「坏行永久占位」问题——缩略图损坏/格式不支持的媒体，
-- 若无标记，会被每轮清扫永久重试并刷失败日志，待办 >200 时还长期占据
-- ListPending 的 LIMIT 窗口。embed 是漏网的那个，本迁移补齐。
--
-- 语义沿用 phash（00024）的约定：embed_scanned_at 非空 = 「已尝试过」。
-- 确定性失败（缩略图解码失败）由 embedgen sweepOnce 回填本标记，不再重试；
-- 需要重算时用 -force（ListPending 忽略标记全量重算）。
-- 注意：与 phash 不同，embedding 写入走 UPDATE media SET embedding=...，
-- 已成功行 embedding IS NOT NULL 天然不再进入待扫，故成功路径**不必**回填本列
-- （本列只为「试过但失败」收敛存在；这与 phash 的 Save 同时写两列略有差异，
--  但 ListPending 以 embedding IS NULL AND embed_scanned_at IS NULL 为待办条件，
--  两类行的归宿都闭合）。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE media ADD COLUMN IF NOT EXISTS embed_scanned_at TIMESTAMPTZ;
COMMENT ON COLUMN media.embed_scanned_at IS 'embedding 扫描标记（非空=已尝试；缩略图无法解码等确定性失败不再重试，-force 可重算）。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE media DROP COLUMN IF EXISTS embed_scanned_at;
-- +goose StatementEnd
