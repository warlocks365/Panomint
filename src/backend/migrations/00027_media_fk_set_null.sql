-- 放宽指向 media(id) 的四处外键：NO ACTION → ON DELETE SET NULL（P1-02）
--
-- **为什么必须改**：media.Purge（回收站彻底清除）是整行 DELETE，但
--   · albums.cover_media_id       （00005_ddl_part.sql:14）
--   · people.cover_media_id       （00006_ddl_part.sql:24）
--   · media.duplicate_of          （00004_ddl_part.sql:52，去重原件引用）
--   · media.live_photo_pair_id    （00004_ddl_part.sql:100，Live Photo 配对）
-- 四处外键都没有 ON DELETE 规则（默认 NO ACTION）。只要待 purge 的媒体是任一
-- 相册封面、人物封面、去重原件或 Live Photo 配对目标，DELETE 即以 23503 失败 ——
-- 回收站出现「删不掉」的媒体，触发路径非常日常（相册设封面 → 删照片 → 清空回收站）。
--
-- **改法**：四处一律 `ON DELETE SET NULL`。封面/配对/原件引用在语义上都允许悬空为 NULL
-- （相册/人物回落为无封面，Live Photo 与去重副本回落为无配对/无原件），四列本就可空，
-- 不需要额外改列定义。
--
-- **与应用层的分工**：internal/media/write.go 的 Purge 在 DELETE 前仍在**同一事务**里
-- 显式 UPDATE 置 NULL，原因有二：① 解除的引用计数要写进审计 detail（DB 级联拿不到
-- 计数）；② 对本迁移尚未应用的部署兜底。本迁移是第二道防线，二者不冲突。
--
-- 只改四处约束的删除规则，不动索引、不动其它外键（audit_log 的同类问题见 00025）。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_cover_media_id_fkey;
ALTER TABLE albums ADD CONSTRAINT albums_cover_media_id_fkey
    FOREIGN KEY (cover_media_id) REFERENCES media(id) ON DELETE SET NULL;

ALTER TABLE people DROP CONSTRAINT IF EXISTS people_cover_media_id_fkey;
ALTER TABLE people ADD CONSTRAINT people_cover_media_id_fkey
    FOREIGN KEY (cover_media_id) REFERENCES media(id) ON DELETE SET NULL;

ALTER TABLE media DROP CONSTRAINT IF EXISTS media_duplicate_of_fkey;
ALTER TABLE media ADD CONSTRAINT media_duplicate_of_fkey
    FOREIGN KEY (duplicate_of) REFERENCES media(id) ON DELETE SET NULL;

ALTER TABLE media DROP CONSTRAINT IF EXISTS media_live_photo_pair_id_fkey;
ALTER TABLE media ADD CONSTRAINT media_live_photo_pair_id_fkey
    FOREIGN KEY (live_photo_pair_id) REFERENCES media(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 还原为默认的 NO ACTION。⚠️ 若此时已存在上述任一列为 NULL 之外的悬挂中间态
-- （例如还原窗口内有媒体被 purge 而引用列尚未置 NULL），本 Down 可能失败 ——
-- 这是刻意的：还原前应先确认这些引用如何处置，而不是让迁移静默连带删除。
ALTER TABLE albums DROP CONSTRAINT IF EXISTS albums_cover_media_id_fkey;
ALTER TABLE albums ADD CONSTRAINT albums_cover_media_id_fkey
    FOREIGN KEY (cover_media_id) REFERENCES media(id);

ALTER TABLE people DROP CONSTRAINT IF EXISTS people_cover_media_id_fkey;
ALTER TABLE people ADD CONSTRAINT people_cover_media_id_fkey
    FOREIGN KEY (cover_media_id) REFERENCES media(id);

ALTER TABLE media DROP CONSTRAINT IF EXISTS media_duplicate_of_fkey;
ALTER TABLE media ADD CONSTRAINT media_duplicate_of_fkey
    FOREIGN KEY (duplicate_of) REFERENCES media(id);

ALTER TABLE media DROP CONSTRAINT IF EXISTS media_live_photo_pair_id_fkey;
ALTER TABLE media ADD CONSTRAINT media_live_photo_pair_id_fkey
    FOREIGN KEY (live_photo_pair_id) REFERENCES media(id);
-- +goose StatementEnd
