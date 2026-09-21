-- +goose Up
-- Job000059/060：地图 UI 偏好新增两列——筛选悬浮层收起状态 + 标记样式（图标/缩略图）。
-- 均带默认值，存量行无需回填；CHECK 约束与 NormalizeUIPrefs 白名单同口径。

ALTER TABLE user_ui_prefs
    ADD COLUMN map_filter_collapsed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN map_marker_mode      VARCHAR(8) NOT NULL DEFAULT 'icon'
        CHECK (map_marker_mode IN ('icon', 'thumb'));

-- +goose Down

ALTER TABLE user_ui_prefs
    DROP COLUMN map_filter_collapsed,
    DROP COLUMN map_marker_mode;
