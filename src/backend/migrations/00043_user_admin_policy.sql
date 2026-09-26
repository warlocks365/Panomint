-- Job000128（2026-09-26）：管理员用户账号管理 + 注册/登录安全策略 + 网络端口配置。
--
--   · users 增三列：
--       - must_change_password  管理员重置密码后置 true；该用户下次登录成功后必须先改密（前端强跳，改密成功清除）；
--       - failed_login_count    连续登录失败计数（成功登录清零）；
--       - locked_until          失败次数达上限后的临时锁定截止时间（NULL=未锁定）。
--   · 新表 system_account_config（singleton，与 system_https_config 同构）：
--       注册开关 / 邀请注册 / 密码强度策略（长度+四类字符复选）/ 登录失败锁定参数。
--       校验（范围/组合）在应用层 policy.go，DDL 只兜底 NOT NULL + DEFAULT + 范围 CHECK。
--   · 新表 invite_codes：邀请注册码（一次性）——code 唯一、过期时间、被谁使用；
--       created_by 随创建者删除级联清理；used_by SET NULL 保留使用痕迹。
--   · system_https_config 增两列：
--       - http_port   HTTP 访问端口（展示与文档语义；默认 80）；
--       - https_port  HTTPS 访问端口 = 强制跳转的目标端口（默认 443；非 443 时 301 Location 携带 :port）。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS failed_login_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS locked_until TIMESTAMPTZ;
COMMENT ON COLUMN users.must_change_password IS '下次登录必须改密（Job000128）：管理员重置密码时自动置位，用户改密成功后清除。';
COMMENT ON COLUMN users.failed_login_count IS '连续登录失败次数（Job000128）：成功登录清零；达上限置 locked_until。';
COMMENT ON COLUMN users.locked_until IS '登录临时锁定截止时间（Job000128）：NULL=未锁定；期间登录一律拒绝（ACCOUNT_LOCKED）。';

CREATE TABLE IF NOT EXISTS system_account_config (
    singleton           BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    allow_registration  BOOLEAN NOT NULL DEFAULT FALSE,
    invite_required     BOOLEAN NOT NULL DEFAULT FALSE,
    pwd_min_length      INT NOT NULL DEFAULT 8 CHECK (pwd_min_length BETWEEN 8 AND 64),
    pwd_require_upper   BOOLEAN NOT NULL DEFAULT FALSE,
    pwd_require_lower   BOOLEAN NOT NULL DEFAULT FALSE,
    pwd_require_digit   BOOLEAN NOT NULL DEFAULT FALSE,
    pwd_require_special BOOLEAN NOT NULL DEFAULT FALSE,
    login_max_attempts  INT NOT NULL DEFAULT 5 CHECK (login_max_attempts BETWEEN 1 AND 50),
    login_lock_minutes  INT NOT NULL DEFAULT 15 CHECK (login_lock_minutes BETWEEN 1 AND 1440),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO system_account_config (singleton) VALUES (TRUE) ON CONFLICT DO NOTHING;
COMMENT ON TABLE system_account_config IS '账号策略系统级配置（Job000128）：singleton 单行；注册开关默认关闭、邀请注册可选、密码强度策略与登录失败锁定参数；校验在应用层 policy.go。';

CREATE TABLE IF NOT EXISTS invite_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code       TEXT NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_invite_codes_code ON invite_codes(code);
COMMENT ON TABLE invite_codes IS '邀请注册码（Job000128）：一次性使用；注册开关+邀请模式开启时 /auth/register 必须携带有效码。';

ALTER TABLE system_https_config
    ADD COLUMN IF NOT EXISTS http_port INT NOT NULL DEFAULT 80 CHECK (http_port BETWEEN 1 AND 65535),
    ADD COLUMN IF NOT EXISTS https_port INT NOT NULL DEFAULT 443 CHECK (https_port BETWEEN 1 AND 65535);
COMMENT ON COLUMN system_https_config.http_port IS 'HTTP 访问端口（Job000128）：默认 80；用于网络页展示与跳转判断语义。';
COMMENT ON COLUMN system_https_config.https_port IS 'HTTPS 访问端口（Job000128）：默认 443；force_https 的 301 Location 在非 443 时携带该端口。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE system_https_config
    DROP COLUMN IF EXISTS https_port,
    DROP COLUMN IF EXISTS http_port;
DROP TABLE IF EXISTS invite_codes;
DROP TABLE IF EXISTS system_account_config;
ALTER TABLE users
    DROP COLUMN IF EXISTS locked_until,
    DROP COLUMN IF EXISTS failed_login_count,
    DROP COLUMN IF EXISTS must_change_password;
-- +goose StatementEnd
