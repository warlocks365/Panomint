-- Job000132：索引进度实时化——正在处理的文件名（前端「N/M · 正在处理 xxx」展示）
-- 与孤儿任务回收（api 启动时 running 必为中断残留，见 main.go 启动钩子）。

-- +goose Up
ALTER TABLE index_jobs ADD COLUMN IF NOT EXISTS current_file TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE index_jobs DROP COLUMN IF EXISTS current_file;
