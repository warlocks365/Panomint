-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
INSERT INTO roles (name, description) VALUES

  ('owner',  '所有者，全部权限'),

  ('admin',  '管理员，用户/系统配置'),

  ('member', '普通成员，个人空间+共享空间贡献'),

  ('viewer', '访客/只读');



INSERT INTO role_permissions (role_id, perm)

  SELECT id, 'media:read' FROM roles WHERE name='viewer';

INSERT INTO role_permissions (role_id, perm)

  SELECT id, v FROM roles r, unnest(ARRAY[

    'media:read','media:write','album:read','album:write','share:create'

  ]) v WHERE r.name='member';

INSERT INTO role_permissions (role_id, perm)

  SELECT id, v FROM roles r, unnest(ARRAY[

    'media:*','album:*','share:*','space:*','admin:users','admin:system'

  ]) v WHERE r.name IN ('owner','admin');
-- +goose StatementEnd
