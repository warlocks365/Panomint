-- Job000143：媒体元数据可编辑 —— 拆分「拍摄地」与「详细地址」两列。
--
-- 背景（v1.9.2 全量审查后新增需求）：用户要在照片/视频/360照片/360视频的详细信息里
--   编辑 GPS、拍摄地、详细地址、拍摄时间，**只改相册系统数据，不动原媒体文件**。
--
-- 为什么需要迁移（本仓既有字段语义不匹配）：
--   media.place 自建表起（00004）装的是高德逆地理的 `formatted_address`，内容形如
--   「北京市东城区景山前街 4 号」—— 是**详细地址**，不是短地名。这导致：
--     1. 「拍摄地」（如「景山前街」）与「详细地址」两个概念挤在一列，
--        无法按粒度筛选（地图页 place 搜索）或展示；
--     2. VARCHAR(128) 对长地址偏紧。
--
-- 本迁移的处置：
--   1. 新增 address 列承载「详细地址」；
--   2. place 语义收敛为「短地名」，放宽到 VARCHAR(256)（短地名用不到 128，
--      放宽是为兼容历史脏数据与手工输入的长地名）；
--   3. 启发式搬迁：把现有 place 中的**详细地址**内容复制到 address 并清空 place，
--      短地名的存量数据**保留在 place 不动**（见下方判定条件）。
--
-- ⚠️ 搬迁是「复制」而非「移动」：address 拿到内容，place 置 NULL 待用户重填短地名。
--    这样即使启发式判定有误，原值仍在 address 里可找回，**无数据丢失**。
--
-- 回滚（Down 段）只删 address 列；place 的原值可从 address 回填：
--   UPDATE media SET place = address WHERE address IS NOT NULL;
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 搬迁条件基于内容特征，重复执行结果一致。

-- +goose Up
-- +goose StatementBegin

-- 1.1 新增 address 列（详细地址）
ALTER TABLE media ADD COLUMN IF NOT EXISTS address TEXT;

-- 1.2 place 放宽到 256（原 128 对长地址偏紧）
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'media' AND column_name = 'place'
          AND character_maximum_length = 128
    ) THEN
        ALTER TABLE media ALTER COLUMN place TYPE VARCHAR(256);
    END IF;
END
$$;

-- 1.3 启发式搬迁：只把「明显是详细地址」的内容复制到 address。
--
-- 判定依据（任一命中即视为详细地址）：
--   · 含道路细节词：路/号/街/巷/弄/胡同/大街/大道/里/村/镇
--   · 含行政区划后缀且总长偏长：如「…朝阳区」（≤24 字的短行政区名不算）
--   · 总长超过 24 字（短地名几乎不会这么长）
--
-- 目的是**不误搬**已经正确的短地名（如「故宫博物院」「西湖」）。
UPDATE media
SET address = place
WHERE place IS NOT NULL
  AND btrim(place) <> ''
  AND address IS NULL
  AND (
        place LIKE '%路%' OR place LIKE '%号%' OR place LIKE '%街%'
     OR place LIKE '%巷%' OR place LIKE '%弄%' OR place LIKE '%胡同%'
     OR place LIKE '%大道%' OR place LIKE '%里%'  OR place LIKE '%村%'
     OR place LIKE '%镇%'  OR place LIKE '%路%'
     OR length(place) > 24
      );

-- 1.4 已搬迁的 place 清空，避免同一份内容两列重复。
--    条件用「address = place」而非「address IS NOT NULL」：只清我们**确实**搬过去的那些，
--    用户若此前手工写过 address，不受影响。
UPDATE media
SET place = NULL
WHERE address IS NOT NULL
  AND place IS NOT NULL
  AND btrim(place) = btrim(address);

-- 1.6 文档化列语义（与 media 上其它 COMMENT 风格一致）
COMMENT ON COLUMN media.address IS
  '详细地址（用户可编辑，Job000143）：由地图搜索候选带入或手工填写。仅相册系统数据，不写回原媒体文件。';
COMMENT ON COLUMN media.place IS
  '拍摄地短地名（用户可编辑，Job000143）：反向地理编码的 formatted_address 已迁至 address。';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 🔴 回滚**必须先回填 place 再删列**，否则 Up 的 1.4 已经把 place 置 NULL，
-- 那些行的详细地址会随 address 列一起消失 —— **不可逆的数据丢失**。
--
-- 守卫 `place IS NULL` 解决「怕覆盖用户已编辑的 place」这个原本的顾虑：
-- 只补 NULL 行，用户改过的非空 place 一律不碰。
UPDATE media SET place = address WHERE place IS NULL AND address IS NOT NULL;
ALTER TABLE media DROP COLUMN IF EXISTS address;
-- +goose StatementEnd
