-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE EXTENSION IF NOT EXISTS vector;          -- pgvector

CREATE EXTENSION IF NOT EXISTS pg_trgm;         -- 模糊搜索

CREATE EXTENSION IF NOT EXISTS postgis;         -- 空间索引/地理查询（地图模式 bbox/距离/聚合）
-- +goose StatementEnd
