// Package httpsconfig HTTPS/证书系统级配置（Job000125）。
//
// 存储 = 单行表 system_https_config（singleton，迁移 00042），与 system_transcode_config 同构：
// 读侧无行 = 默认值；写侧 ON CONFLICT (singleton) 原子 upsert + COALESCE 部分更新。
//
// 职责边界（关键设计决定，见设计文档 §4.2）：
//
//   - force_https 由 **应用层中间件**热生效（301 X-Forwarded-Proto=http 的请求）——
//     TLS 终止在反代（Caddy），应用层只依据代理透传的协议头做跳转，无需触碰反代配置；
//   - 证书文件由**反代消费**：本包只负责接收上传（校验 PEM 配对 + 解析到期日）、
//     落盘到共享卷（HTTPS_CERT_DIR）、把路径与到期日记入配置行。
//     「让反代改用新证书」是显式运维动作（更新 TLS_CERT/TLS_KEY + 重启 caddy），
//     不做运行时热切换——自动化重启入口容器的风险大于收益。
package httpsconfig

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Defaults 无配置行时的缺省值：与迁移 00042 的 DDL DEFAULT 逐字一致。
// force_https=false = 历史行为（Caddyfile.tls 的 auto_https disable_redirects 形态）。
const (
	DefaultForceHTTPS   = false
	DefaultCertPath     = ""
	DefaultCertKeyPath  = ""
	CertFileName        = "fullchain.crt"
	CertKeyFileName     = "private.pem"
	ApplyHint           = "将 Caddyfile.tls 的 TLS_CERT/TLS_KEY 指向本目录下的 " + CertFileName + "/" + CertKeyFileName + "，然后 docker compose restart caddy（入口短暂中断）"
)

// Config GET/PUT /admin/https/config 的响应视图。
// CertDir/RequestProto 由 Handler 注入（env 与当前请求），非库字段。
type Config struct {
	ForceHTTPS   bool       `json:"force_https"`
	CertPath     string     `json:"cert_path"`
	CertKeyPath  string     `json:"cert_key_path"`
	CertNotAfter *time.Time `json:"cert_not_after,omitempty"`
	UpdatedAt    *time.Time `json:"updated_at,omitempty"`
	CertDir      string     `json:"cert_dir"`
	RequestProto string     `json:"request_proto"`
}

// ErrValidation 校验失败哨兵（与 transcode.ErrValidation 同型的本地哨兵：跨包不共享
// 错误实例，判型各自 isValidationErr，避免包间耦合）。
var ErrValidation = errors.New("httpsconfig validation")

// Update 部分更新字段集：nil = 不动。
type Update struct {
	ForceHTTPS   *bool
	CertPath     *string
	CertKeyPath  *string
	CertNotAfter *time.Time
}

// Get 读配置；无行返回默认值，查询出错向上抛（Handler 映射 500）。
func Get(ctx context.Context, pool *pgxpool.Pool) (*Config, error) {
	out := &Config{
		ForceHTTPS:  DefaultForceHTTPS,
		CertPath:    DefaultCertPath,
		CertKeyPath: DefaultCertKeyPath,
	}
	var force *bool
	var cp, ckp *string
	var notAfter, updated *time.Time
	err := pool.QueryRow(ctx,
		`SELECT force_https, cert_path, cert_key_path, cert_not_after, updated_at
		 FROM system_https_config WHERE singleton = TRUE`).
		Scan(&force, &cp, &ckp, &notAfter, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	if force != nil {
		out.ForceHTTPS = *force
	}
	if cp != nil {
		out.CertPath = *cp
	}
	if ckp != nil {
		out.CertKeyPath = *ckp
	}
	out.CertNotAfter = notAfter
	out.UpdatedAt = updated
	return out, nil
}

// Put 部分更新：至少一个字段非 nil（全 nil = 调用方缺陷，ErrValidation）。
// 显式 ::type casts 与 transcode 包同因：nil 指针以 unknown 进 COALESCE 会被解析成 text（42804）。
func Put(ctx context.Context, pool *pgxpool.Pool, u Update) error {
	if u.ForceHTTPS == nil && u.CertPath == nil && u.CertKeyPath == nil && u.CertNotAfter == nil {
		return fmt.Errorf("%w: 至少需提供一个待更新字段", ErrValidation)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO system_https_config
			(singleton, force_https, cert_path, cert_key_path, cert_not_after, updated_at)
		VALUES (TRUE,
			COALESCE($1::boolean, $5::boolean),
			COALESCE($2::text, $6::text),
			COALESCE($3::text, $7::text),
			COALESCE($4::timestamptz, $8::timestamptz),
			now())
		ON CONFLICT (singleton) DO UPDATE SET
			force_https   = COALESCE($1, system_https_config.force_https),
			cert_path     = COALESCE($2, system_https_config.cert_path),
			cert_key_path = COALESCE($3, system_https_config.cert_key_path),
			cert_not_after = COALESCE($4, system_https_config.cert_not_after),
			updated_at    = now()`,
		u.ForceHTTPS, u.CertPath, u.CertKeyPath, u.CertNotAfter,
		DefaultForceHTTPS, DefaultCertPath, DefaultCertKeyPath, nil)
	return err
}

// IsValidationErr 判断是否为配置校验类错误（Handler 映射 400）。
func IsValidationErr(err error) bool { return errors.Is(err, ErrValidation) }
