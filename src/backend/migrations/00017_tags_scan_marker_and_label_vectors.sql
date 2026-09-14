-- 标签 AI 打标：标签文本向量持久缓存 + 每图「已完成一轮打标」标记
--
-- 缺陷 1（API 启动 ~110s）：internal/tags.NewClassifier 在进程启动时把词表逐标签编码为
--   文本向量，提示词模板从 2 条扩到 5 条后编码次数 114×5=570，CPU 下 API 绑定端口前要等
--   近 2 分钟（期间 nginx 502）。把结果持久化后，二次启动直接载入，编码次数 0。
--   表 tag_label_vectors 只按 cache_key 取全量、不做近邻检索，故**不建向量索引**；
--   主键 (cache_key,label) 已提供以 cache_key 为前导列的 B-tree。
--   cache_key 由「模型族 + 模型目录 + 词表内容哈希 + 提示词模板集合哈希」构成
--   （见 internal/tags/labelcache.go 的 labelCacheKey），任一变化即换 key、自动重算。
--
-- 缺陷 2（tag-worker 每分钟空转）：待打标判据原为「没有 origin='ai' 关联行」，但某图若启发式
--   已占据全部互斥组（如季节），CLIP 建议会被 applier.filterAISuggestions 全部丢弃 →
--   永远不产生 origin='ai' 行 → 每分钟被重选重打，日志持续 "发现 1 条待打标"，永不收敛。
--   改用 media.tags_scanned_at（与 faces_scanned_at 同模式）标记「该图已完成一轮 AI 打标」，
--   无论是否产出建议都置位；判据改为该列为空。
--   手动强制重打标（POST /ai/tags {"scope":"media_id"}）走 ListPendingAI 的 forceMediaID 分支，
--   该分支不带本标记条件，**无视**标记照常重算。

-- +goose Up
-- +goose StatementBegin

-- 1) 标签文本向量持久缓存
CREATE TABLE IF NOT EXISTS tag_label_vectors (
    cache_key  TEXT        NOT NULL,   -- 向量「哪一套」：族+模型目录+词表哈希+模板哈希
    label      TEXT        NOT NULL,   -- 标签名（落库名）
    class      TEXT        NOT NULL DEFAULT '', -- 词表分类（场景|物体|事件），仅展示用
    grp        TEXT        NOT NULL DEFAULT '', -- 互斥组名；空 = 不互斥
    embedding  VECTOR(512) NOT NULL,   -- 文本塔输出，与 media.embedding 同维
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (cache_key, label)
);

-- 2) 每图「已完成一轮 AI 打标」标记
ALTER TABLE media ADD COLUMN IF NOT EXISTS tags_scanned_at TIMESTAMPTZ;

-- 3) 存量回填：把「已经产出过 origin='ai' 关联」的媒体标记为已打标，避免迁移后这批图
--    被整库重打一遍。测试服实测：media 共 93 行，其中 92 行有 origin='ai' 关联 → 回填 92 行。
--    未回填的只是「从未产出过 AI 建议」的少数图（测试服 1 条）：它们会在下一次清扫各被处理
--    一次并落标记，随即收敛——这正是缺陷 2 想达成的效果，故无需也不应回填。
--    影响范围：仅把满足条件的 media.tags_scanned_at 由 NULL 置为 now()，
--    不修改任何其它列、不删除任何行、不影响无 origin='ai' 关联的媒体（保持 NULL = 待打标）。
UPDATE media m
   SET tags_scanned_at = now()
 WHERE m.tags_scanned_at IS NULL
   AND EXISTS (SELECT 1 FROM media_tags mt
                WHERE mt.media_id = m.id AND mt.origin = 'ai');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE media DROP COLUMN IF EXISTS tags_scanned_at;
DROP TABLE IF EXISTS tag_label_vectors;
-- +goose StatementEnd
