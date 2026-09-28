-- Job000133：任务系统增强——扫描目录（断点续跑定位）、结果统计三列（详情展示）。

-- +goose Up
ALTER TABLE index_jobs ADD COLUMN IF NOT EXISTS dir TEXT NOT NULL DEFAULT '';
ALTER TABLE index_jobs ADD COLUMN IF NOT EXISTS result_inserted INTEGER;
ALTER TABLE index_jobs ADD COLUMN IF NOT EXISTS result_duplicate INTEGER;
ALTER TABLE index_jobs ADD COLUMN IF NOT EXISTS result_failed INTEGER;

-- +goose Down
ALTER TABLE index_jobs DROP COLUMN IF EXISTS dir;
ALTER TABLE index_jobs DROP COLUMN IF EXISTS result_inserted;
ALTER TABLE index_jobs DROP COLUMN IF EXISTS result_duplicate;
ALTER TABLE index_jobs DROP COLUMN IF EXISTS result_failed;
