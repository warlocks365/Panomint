-- Job000101 个人空间按相册分组视图：视图模式偏好持久化（沿用 /user/ui-prefs 整体替换契约）。
-- false=时间轴平铺（默认，存量行为不变）；true=按相册分组。

-- +goose Up
ALTER TABLE user_ui_prefs
    ADD COLUMN IF NOT EXISTS spaces_group_by_album BOOLEAN NOT NULL DEFAULT FALSE;

-- +goose Down
ALTER TABLE user_ui_prefs
    DROP COLUMN IF EXISTS spaces_group_by_album;
