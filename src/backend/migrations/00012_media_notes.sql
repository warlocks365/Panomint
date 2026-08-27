-- Job000005 体验需求：媒体备注（notes）
-- 用途：PATCH /media/:id 用户自定义备注文本；媒体详情返回
-- +goose Up
-- +goose StatementBegin
ALTER TABLE media ADD COLUMN IF NOT EXISTS notes TEXT;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE media DROP COLUMN IF EXISTS notes;
-- +goose StatementEnd
