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
// 端口默认 80/443（迁移 00043 NOT NULL DEFAULT）：即"标准端口不进 URL"的常规形态。
const (
	DefaultForceHTTPS   = false
	DefaultCertPath     = ""
	DefaultCertKeyPath  = ""
	DefaultHTTPPort     = 80
	DefaultHTTPSPort    = 443
	CertFileName        = "fullchain.crt"
	CertKeyFileName     = "private.pem"
	ApplyHint           = "将 Caddyfile.tls 的 TLS_CERT/TLS_KEY 指向本目录下的 " + CertFileName + "/" + CertKeyFileName + "，然后 docker compose restart caddy（入口短暂中断）"
)

// Config GET/PUT /admin/https/config 的响应视图。
// CertDir/RequestProto 由 Handler 注入（env 与当前请求），非库字段。
type Config struct {
	ForceHTTPS bool `json:"force_https"`
	// HTTPPort / HTTPSPort 访问端口配置（Job000128 点 9）。
	// HTTPSPort 是 **ForceHTTPS 301 目标端口**的真实驱动源（middleware.go）：
	// 非 443 时跳转目标携带 `:port`；HTTPPort 目前是配置记录与 UI 展示
	// （应用/反代的实际监听由部署决定，配置改不了它们的监听）。
	HTTPPort     int        `json:"http_port"`
	HTTPSPort    int        `json:"https_port"`
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
	HTTPPort     *int
	HTTPSPort    *int
	CertPath     *string
	CertKeyPath  *string
	CertNotAfter *time.Time
}

// Get 读配置；无行返回默认值，查询出错向上抛（Handler 映射 500）。
func Get(ctx context.Context, pool *pgxpool.Pool) (*Config, error) {
	out := &Config{
		ForceHTTPS: DefaultForceHTTPS,
		HTTPPort:   DefaultHTTPPort,
		HTTPSPort:  DefaultHTTPSPort,
		CertPath:   DefaultCertPath,
		CertKeyPath: DefaultCertKeyPath,
	}
	var force *bool
	var cp, ckp *string
	var notAfter, updated *time.Time
	var httpPort, httpsPort int
	err := pool.QueryRow(ctx,
		`SELECT force_https, cert_path, cert_key_path, cert_not_after, updated_at, http_port, https_port
		 FROM system_https_config WHERE singleton = TRUE`).
		Scan(&force, &cp, &ckp, &notAfter, &updated, &httpPort, &httpsPort)
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
	// 迁移 00043 后两端口列为 NOT NULL DEFAULT，理论上恒可扫出；
	// 仍按"扫不到就用默认值"兜底（防御半途迁移的库）。
	if httpPort > 0 {
		out.HTTPPort = httpPort
	}
	if httpsPort > 0 {
		out.HTTPSPort = httpsPort
	}
	return out, nil
}

// validatePorts 端口范围校验（与迁移 00043 的 CHECK 一致；相等性在 handler 层连同库值一起判）。
func validatePorts(p int) error {
	if p < 1 || p > 65535 {
		return fmt.Errorf("%w: 端口须在 1–65535 之间（得到 %d）", ErrValidation, p)
	}
	return nil
}

// Put 部分更新：至少一个字段非 nil（全 nil = 调用方缺陷，ErrValidation）。
// 显式 ::type casts 与 transcode 包同因：nil 指针以 unknown 进 COALESCE 会被解析成 text（42804）。
func Put(ctx context.Context, pool *pgxpool.Pool, u Update) error {
	if u.ForceHTTPS == nil && u.CertPath == nil && u.CertKeyPath == nil && u.CertNotAfter == nil &&
		u.HTTPPort == nil && u.HTTPSPort == nil {
		return fmt.Errorf("%w: 至少需提供一个待更新字段", ErrValidation)
	}
	if u.HTTPPort != nil {
		if err := validatePorts(*u.HTTPPort); err != nil {
			return err
		}
	}
	if u.HTTPSPort != nil {
		if err := validatePorts(*u.HTTPSPort); err != nil {
			return err
		}
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO system_https_config
			(singleton, force_https, cert_path, cert_key_path, cert_not_after, http_port, https_port, updated_at)
		VALUES (TRUE,
			COALESCE($1::boolean, $5::boolean),
			COALESCE($2::text, $6::text),
			COALESCE($3::text, $7::text),
			COALESCE($4::timestamptz, $8::timestamptz),
			COALESCE($9::int, $10::int),
			COALESCE($11::int, $12::int),
			now())
		ON CONFLICT (singleton) DO UPDATE SET
			force_https   = COALESCE($1, system_https_config.force_https),
			cert_path     = COALESCE($2, system_https_config.cert_path),
			cert_key_path = COALESCE($3, system_https_config.cert_key_path),
			cert_not_after = COALESCE($4, system_https_config.cert_not_after),
			http_port     = COALESCE($9, system_https_config.http_port),
			https_port    = COALESCE($11, system_https_config.https_port),
			updated_at    = now()`,
		u.ForceHTTPS, u.CertPath, u.CertKeyPath, u.CertNotAfter,
		DefaultForceHTTPS, DefaultCertPath, DefaultCertKeyPath, nil,
		u.HTTPPort, DefaultHTTPPort, u.HTTPSPort, DefaultHTTPSPort)
	return err
}

// IsValidationErr 判断是否为配置校验类错误（Handler 映射 400）。
func IsValidationErr(err error) bool { return errors.Is(err, ErrValidation) }
