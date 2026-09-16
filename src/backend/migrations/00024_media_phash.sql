-- 感知哈希（pHash）去重 —— 基础建设。
--
-- **为什么 pHash 单独占一列，而不是按 TDD §381 的字面「pHash 入 media.hash」**：
-- media.hash 自迁移 00004 起存的就已经是 **sha256 内容哈希**（由 internal/index/scanner.go
-- 的 HashFile 写入），而且它是既有导入去重链路的**承重墙**——index.go 与 upload.go 都以
-- 「sha256 相同 ⇒ 同一文件、跳过导入」为判据。pHash 的语义与之根本不同：
--   · sha256 回答「字节是否完全相同」
--   · pHash  回答「观感是否相似」（认出重新编码 / 缩放 / 加滤镜过的副本）
-- 两者判据不可互换；若把 pHash 写进 media.hash，既有导入去重会立刻把
-- 「看起来像但其实是不同文件」的图误判为重复而丢弃。
-- 00004 里该列的行内注释误写成 "pHash 去重"，已造成实际误解（见 文档/遗留问题处理方案.md），
-- 故本次一并用 COMMENT 把它改正为可查询的事实。
--
-- 存储形态：64 位指纹用 BIGINT 承载。虽然哈希是无符号 64 位、最高位可能为 1，
-- 但 PostgreSQL 的 BIGINT 只是"按位解释"的容器——写入时用 int64(h) 保证位模式无损
-- （已在测试库实测：0x8000000000000001 往返后位模式完全一致）。
-- 读取侧算汉明距离必须写成 `bit_count((a # b)::bit(64))`：
--   · `#` 是按位异或，BIGINT 原生支持；
--   · 但 bit_count 只接受 **bit** 类型，**不接受 bigint**
--     （实测 `SELECT bit_count(1::bigint # 3::bigint)` 直接报
--      `ERROR: function bit_count(bigint) does not exist (SQLSTATE 42883)`），
--     故必须显式转成定长位串。下一个阶段写分组查询时照此写，别再踩一遍。
-- 刻意 **不用 pgvector**：它没有位运算/汉明距离运算符，且 media.embedding VECTOR(512)
-- 已被语义检索占用，语义完全不同，混在同一列会同时毁掉两边。
--
-- phash_scanned_at 沿用 faces_scanned_at（00014）/ tags_scanned_at（00017）的**扫描标记**约定：
-- 非空 = 「已尝试过」。有了它，「缩略图存在但解不开」的媒体才算有归宿，
-- 否则每轮清扫都会重试同一批永远失败的坏缩略图。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE media ADD COLUMN IF NOT EXISTS phash BIGINT;
COMMENT ON COLUMN media.phash IS '64 位感知哈希（DCT pHash）。位序 bit index = row*8+col、位 0 为最低位，算法与位序定义见 internal/phash。与 media.hash（sha256）语义不同，勿混用。';

ALTER TABLE media ADD COLUMN IF NOT EXISTS phash_scanned_at TIMESTAMPTZ;
COMMENT ON COLUMN media.phash_scanned_at IS '感知哈希扫描标记（非空=已尝试；缩略图缺失或无法解码也不再重试）。';

-- 支撑后续「按汉明距离分组」的查询：只索引真正可用于比较的行。
CREATE INDEX IF NOT EXISTS idx_media_phash ON media(phash) WHERE phash IS NOT NULL AND deleted_at IS NULL;

-- 修正 00004 遗留的误导性（且从未真正生效的）注释。
COMMENT ON COLUMN media.hash IS 'sha256 内容哈希（64 hex）。感知哈希见 media.phash —— 两者语义不同，勿混用。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 还原成「无注释」而非还原成 00004 那句 "pHash 去重"：那只是 CREATE TABLE 的 SQL 行内注释，
-- 从未落成 PostgreSQL 的 COMMENT，即本次迁移之前该列本来就没有 COMMENT。
COMMENT ON COLUMN media.hash IS NULL;

DROP INDEX IF EXISTS idx_media_phash;
ALTER TABLE media DROP COLUMN IF EXISTS phash_scanned_at;
ALTER TABLE media DROP COLUMN IF EXISTS phash;
-- +goose StatementEnd
