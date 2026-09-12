-- Job000011 人物识别（人脸聚类）：向量维度调整 + HNSW 索引 + 扫描标记
--
-- 背景：人脸特征采用 OpenCV Zoo 的 SFace（Apache-2.0），输出 **128 维**，
-- 而 DDL v1.1 中 faces.embedding 为 VECTOR(512)、索引为 ivfflat。
-- faces 表当前 0 行，故改列无数据迁移成本（仅需重建索引）。
--
-- 另需 media.faces_scanned_at：人脸走「清扫式」增量（facesgen -mode watch），
-- 需要标记「已扫描」——**一张脸都没检出的媒体也必须标记**，否则会被每轮重复扫描。

-- +goose Up
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_faces_embedding;
ALTER TABLE faces ALTER COLUMN embedding TYPE vector(128);
-- HNSW：增量插入无需「训练」，比 ivfflat 更适合持续追加的人脸库
CREATE INDEX IF NOT EXISTS idx_faces_embedding ON faces USING hnsw (embedding vector_cosine_ops);
ALTER TABLE media ADD COLUMN IF NOT EXISTS faces_scanned_at TIMESTAMPTZ;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE media DROP COLUMN IF EXISTS faces_scanned_at;
DROP INDEX IF EXISTS idx_faces_embedding;
ALTER TABLE faces ALTER COLUMN embedding TYPE vector(512);
CREATE INDEX IF NOT EXISTS idx_faces_embedding ON faces USING ivfflat (embedding vector_cosine_ops) WITH (lists = 50);
-- +goose StatementEnd
