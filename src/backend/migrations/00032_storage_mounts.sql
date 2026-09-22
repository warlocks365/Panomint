-- +goose Up
-- Job000070 F4 网络挂载管理：挂载注册表。
-- 凭据红线：creds_enc 恒为密文（AES-256-GCM，密钥=env STORAGE_CIPHER_KEY，不落库不入日志）；
-- 明文只在请求体内短暂存在，加密后立即丢弃。无凭据挂载（guest/nfs）为空串。
CREATE TABLE storage_mounts (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(128) NOT NULL,
    type        VARCHAR(16)  NOT NULL CHECK (type IN ('webdav','smb','nfs')),
    -- 非敏感连接配置 JSON：webdav {url}；smb {host,share,port?}；nfs {host,export}
    conn        JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- 敏感凭据密文 base64(nonce||ciphertext)，形态 {user,pass,domain?}（加密前）
    creds_enc   TEXT NOT NULL DEFAULT '',
    owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- personal=仅属主可浏览内容；shared=全站登录用户可浏览（内容可见性下轮接入 mediascope）
    visibility  VARCHAR(16) NOT NULL DEFAULT 'personal' CHECK (visibility IN ('personal','shared')),
    status      VARCHAR(16) NOT NULL DEFAULT 'unknown' CHECK (status IN ('unknown','online','offline','error')),
    last_error  TEXT,
    -- worker 执行器回填的挂载点（如 /mnt/storage/<id>），仅展示用
    mount_path  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_storage_mounts_owner ON storage_mounts(owner_id);

COMMENT ON TABLE storage_mounts IS '网络挂载注册表（Job000070 F4）：webdav/smb/nfs 挂载的 CRUD 与状态；凭据恒密文，密钥在 env STORAGE_CIPHER_KEY';
COMMENT ON COLUMN storage_mounts.creds_enc IS 'AES-256-GCM 密文 base64(nonce||ct)；明文绝不落库/入日志；空串=无凭据挂载';
