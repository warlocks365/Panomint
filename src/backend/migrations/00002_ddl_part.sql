-- 来源: 数据库DDL_v1.1.md（自动提取，勿手改）
-- +goose Up
-- +goose StatementBegin
-- 角色

CREATE TABLE roles (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    name        VARCHAR(64) NOT NULL UNIQUE,

    description TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 角色权限（细粒度 RBAC）

CREATE TABLE role_permissions (

    role_id    UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,

    perm       VARCHAR(128) NOT NULL,           -- 例如 media:read, album:write, admin:users

    PRIMARY KEY (role_id, perm)

);



-- 用户（自建账户，不与 DSM 关联）

CREATE TABLE users (

    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    email              VARCHAR(255) NOT NULL UNIQUE,

    display_name       VARCHAR(128),

    password_hash      VARCHAR(255) NOT NULL,    -- bcrypt

    role_id            UUID NOT NULL REFERENCES roles(id),

    mfa_secret         VARCHAR(255),            -- TOTP 密钥（启用 2FA 后）

    mfa_enabled        BOOLEAN NOT NULL DEFAULT false,

    app_password_hash  VARCHAR(255),            -- API 专用应用密码

    status             VARCHAR(16) NOT NULL DEFAULT 'active', -- active|disabled|locked

    last_session       TIMESTAMPTZ,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),

    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 会话（可监控/吊销）

CREATE TABLE sessions (

    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),

    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,

    refresh_token_hash VARCHAR(255) NOT NULL,

    ip          VARCHAR(64),

    user_agent  TEXT,

    expires_at  TIMESTAMPTZ NOT NULL,

    revoked     BOOLEAN NOT NULL DEFAULT false,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()

);



-- 审计日志

CREATE TABLE audit_log (

    id         BIGSERIAL PRIMARY KEY,

    user_id    UUID REFERENCES users(id),

    action     VARCHAR(128) NOT NULL,           -- login, share_create, media_delete...

    detail     JSONB,

    ip         VARCHAR(64),

    at         TIMESTAMPTZ NOT NULL DEFAULT now()

);

CREATE INDEX idx_audit_at ON audit_log(at DESC);
-- +goose StatementEnd
