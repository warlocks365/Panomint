-- +goose Up
-- Job000067（F1 权限体系增强）：权限元数据（中文化/自定义分类）+ 用户级权限临时授予。
-- 关键边界：perm_meta 只改"展示"，enforcement 仍是 role_permissions + 代码白名单（knownPerms 不变）；
-- user_permissions 是**增量授予**（granted=true 生效），不授予=回退角色权限，永不减权。

CREATE TABLE perm_meta (
    perm        VARCHAR(128) PRIMARY KEY,          -- 必须命中 auth.knownPerms 白名单（应用层校验）
    label       VARCHAR(64)  NOT NULL,             -- 中文名（用户可改）
    category    VARCHAR(64)  NOT NULL DEFAULT '其他', -- 自定义分类（用户可建）
    description TEXT,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- 种子：12 个内置权限的默认中文名与分类（用户之后可改）
INSERT INTO perm_meta (perm, label, category, description) VALUES
    ('media:read',  '媒体查看',     '媒体',   '浏览时间轴/相册/地图等全部媒体内容'),
    ('media:write', '媒体管理',     '媒体',   '上传/编辑/删除/移动媒体'),
    ('media:*',     '媒体全权',     '媒体',   '查看+管理的通配组合'),
    ('album:read',  '相册查看',     '相册',   '浏览他人可见相册'),
    ('album:write', '相册管理',     '相册',   '创建/编辑相册与评论'),
    ('album:*',     '相册全权',     '相册',   '查看+管理的通配组合'),
    ('share:create','创建分享',     '分享',   '生成外链分享'),
    ('share:*',     '分享全权',     '分享',   '创建+管理分享的通配组合'),
    ('space:*',     '空间全权',     '空间',   '共享空间全操作'),
    ('admin:users', '用户管理',     '管理',   '管理用户与角色（不能自我提权到未拥有的权限）'),
    ('admin:system','系统管理',     '管理',   '系统级配置（仅 owner/admin 拥有）')
ON CONFLICT (perm) DO NOTHING;

-- 用户级增量授权（临时授予：离职回收/项目制协作）
CREATE TABLE user_permissions (
    user_id   UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    perm      VARCHAR(128) NOT NULL,
    granted   BOOLEAN     NOT NULL DEFAULT true,    -- true=增量授予（false 预留给显式记录）
    granted_by UUID       REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, perm)
);

-- +goose Down

DROP TABLE user_permissions;
DROP TABLE perm_meta;
