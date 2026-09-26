-- Job000125（2026-09-26）：管理后台「HLS 流媒体设置 + HTTPS/证书设置」持久化。
--
--   · system_transcode_config 增三列（singleton 单行表扩展，迁移 00040 同款）：
--       - hls_seg_seconds  分片时长（秒），DEFAULT 4 = 既有 DefaultSegSeconds，存量行为不变；
--       - hls_cache_profile 缓存策略档位（no_cache|balanced|aggressive），DEFAULT balanced = 现状头；
--       - stream_base_url   流媒体外部地址前缀（空 = 站内相对路径，现状）；
--   · 新表 system_https_config（singleton，与 system_transcode_config 同构）：
--       - force_https 强制 HTTPS（HTTP 301 跳转，应用层中间件热生效）；
--       - cert_path/cert_key_path 证书落卷路径（记录用，TLS 终止在反代）；
--       - cert_not_after 证书到期日（上传时从 x509 自动解析）；
--
-- 校验（范围/枚举/URL 形态）在应用层 sysconfig.go/httpsconfig，DDL 只兜底 NOT NULL + DEFAULT。

-- +goose Up
-- +goose StatementBegin
ALTER TABLE system_transcode_config
    ADD COLUMN IF NOT EXISTS hls_seg_seconds INT NOT NULL DEFAULT 4,
    ADD COLUMN IF NOT EXISTS hls_cache_profile TEXT NOT NULL DEFAULT 'balanced',
    ADD COLUMN IF NOT EXISTS stream_base_url TEXT NOT NULL DEFAULT '';
COMMENT ON COLUMN system_transcode_config.hls_seg_seconds IS 'HLS 分片时长秒数（Job000125）：仅影响新转码任务，存量分片不变；2-20 由应用层校验。';
COMMENT ON COLUMN system_transcode_config.hls_cache_profile IS 'HLS 缓存策略档位（Job000125）：no_cache=调试全不缓存；balanced=m3u8 no-cache+ts 一年（默认，= 历史行为）；aggressive=m3u8 短缓存+ts 一年。';
COMMENT ON COLUMN system_transcode_config.stream_base_url IS 'HLS 流媒体地址前缀（Job000125）：空=站内相对路径；须 http(s):// 开头（应用层校验）。播放端拼接 master.m3u8 用。';

CREATE TABLE IF NOT EXISTS system_https_config (
    singleton      BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    force_https    BOOLEAN NOT NULL DEFAULT FALSE,
    cert_path      TEXT NOT NULL DEFAULT '',
    cert_key_path  TEXT NOT NULL DEFAULT '',
    cert_not_after TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
COMMENT ON TABLE system_https_config IS 'HTTPS/证书系统级配置（Job000125）：singleton 单行；force_https 由应用中间件热生效；证书文件由反代消费（本表只记录路径与到期日）。';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS system_https_config;
ALTER TABLE system_transcode_config
    DROP COLUMN IF EXISTS stream_base_url,
    DROP COLUMN IF EXISTS hls_cache_profile,
    DROP COLUMN IF EXISTS hls_seg_seconds;
-- +goose StatementEnd
