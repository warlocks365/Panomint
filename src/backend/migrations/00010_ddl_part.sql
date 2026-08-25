-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trigger_set_updated_at()

RETURNS TRIGGER AS $$

BEGIN

    NEW.updated_at = now();

    RETURN NEW;

END;

$$ LANGUAGE plpgsql;



-- 为所有含 updated_at 的表创建触发器

CREATE TRIGGER set_updated_at_users BEFORE UPDATE ON users

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_media BEFORE UPDATE ON media

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_albums BEFORE UPDATE ON albums

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_ui_prefs BEFORE UPDATE ON user_ui_prefs

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_map_config BEFORE UPDATE ON system_map_config

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_compute_nodes BEFORE UPDATE ON compute_nodes

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

CREATE TRIGGER set_updated_at_bandwidth BEFORE UPDATE ON bandwidth_profiles

    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
-- +goose StatementEnd
