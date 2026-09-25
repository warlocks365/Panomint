-- Job000120 用户级「自动 HLS 转码」开关（设置页「播放与转码」卡）。
-- true=保持现状（播放器可发起 HLS 转码）；false=CreateJob 409 拒绝 + 播放器直接播原始文件。
-- NOT NULL DEFAULT true：存量用户行为不变；PUT 缺省=保留现值（keep-on-absent，见 uiprefs.go）。

-- +goose Up
ALTER TABLE user_ui_prefs
    ADD COLUMN IF NOT EXISTS auto_transcode BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE user_ui_prefs
    DROP COLUMN IF EXISTS auto_transcode;
