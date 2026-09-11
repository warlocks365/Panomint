-- Job000009 地图模式优化：用户地图偏好（图标配置账户级持久化）
-- 用途：GET/PUT /preferences/map 读写；换设备同步
-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS user_preferences (
    user_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    map_icon    JSONB NOT NULL DEFAULT '{}'::jsonb,   -- 地图聚合点图标配置（shape/color/icon/dataURL）
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_preferences;
-- +goose StatementEnd
