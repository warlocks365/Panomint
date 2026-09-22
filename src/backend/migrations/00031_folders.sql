-- Job000069 F3 目录管理：虚拟目录注册表。
-- 设计红线：media.folder_path 仍是媒体归属事实源；本表只补两件事——
-- ① 空目录的存在性（folder_path 派生模型天然表达不了空目录）；
-- ② 目录级授权（可见/可写，元素形态 [{"user_id":"uuid","read":true,"write":true}]）。
CREATE TABLE IF NOT EXISTS folders (
    path       TEXT PRIMARY KEY,
    owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    grants     JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_folders_owner ON folders(owner_id);

COMMENT ON TABLE folders IS '虚拟目录注册表：存在即目录（含空目录）；media.folder_path 仍是事实源，本表只补空目录存在性与目录级授权（Job000069）';
COMMENT ON COLUMN folders.grants IS '目录级授权：[{"user_id":"uuid","read":true,"write":true}]；read 收敛 mediascope 可见性臂，write 在端点层校验';
