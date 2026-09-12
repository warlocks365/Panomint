-- 查看器基本编辑（非破坏式 sidecar）
-- 用途：POST /media/:id/rotate 与 PATCH /media/:id 持久化旋转/裁剪参数；
--       媒体详情返回；原文件保持不变，呈现端按参数渲染（不改写原文件）。
-- 结构：{"rotate": 0|90|180|270, "crop": {"x":0..1,"y":0..1,"w":0..1,"h":0..1} | null}
-- +goose Up
-- +goose StatementBegin
ALTER TABLE media ADD COLUMN IF NOT EXISTS edits JSONB;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE media DROP COLUMN IF EXISTS edits;
-- +goose StatementEnd
