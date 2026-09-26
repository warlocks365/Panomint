package httpsconfig

// Handler GET/PUT /admin/https/config + POST /admin/https/cert（admin:system，路由层校验）。
//
// 上传链路（UploadCert）的安全顺序是刻意设计的：
//  1. 先读内存（multipart 大小上限由 gin 引擎级 MaxMultipartMemory + 服务端再读全量限时）；
//  2. **先校验后落盘**：tls.X509KeyPair 同时验证 PEM 语法与证书-私钥配对，配不上一个字节都不写；
//  3. 文件名服务端定死（CertFileName/CertKeyFileName）——客户端提供的任何文件名都不参与拼路径，
//     从根上消灭「上传证书」变成「上传任意文件」的路径注入面；
//  4. 落盘成功才写库（cert_path/not_after）；写库失败时文件已存在但不生效（反代未指向），
//     响应仍报错，用户重传即自愈。

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
)

// Handler HTTPS 配置处理器。
type Handler struct {
	Pool    *pgxpool.Pool
	Audit   *audit.Recorder // 可为 nil（测试跳过）
	CertDir string          // 证书落盘目录（env HTTPS_CERT_DIR；空 = 上传端点返回 503）
}

// GetConfig GET /admin/https/config：配置视图 + 当前请求协议 + 证书目录。
func (h *Handler) GetConfig(c *gin.Context) {
	cfg, err := Get(c.Request.Context(), h.Pool)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	fillRuntime(c, h, cfg)
	c.JSON(http.StatusOK, cfg)
}

// httpsUpdateReq PUT /admin/https/config 的请求体（Job000128 扩展端口字段后
// 不再用 map[string]bool —— map 值类型装不下 int）。
type httpsUpdateReq struct {
	ForceHTTPS *bool `json:"force_https"`
	HTTPPort   *int  `json:"http_port"`
	HTTPSPort  *int  `json:"https_port"`
}

// PutConfig PUT /admin/https/config：部分更新（缺失字段不改）。
func (h *Handler) PutConfig(c *gin.Context) {
	var raw httpsUpdateReq
	if err := c.ShouldBindJSON(&raw); err != nil {
		httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST",
			`请求体需为 JSON 对象（{"force_https":true,"http_port":80,"https_port":443}，均可省略）`, err)
		return
	}
	u := Update{}
	if raw.ForceHTTPS != nil {
		u.ForceHTTPS = raw.ForceHTTPS
	}
	if raw.HTTPPort != nil {
		u.HTTPPort = raw.HTTPPort
	}
	if raw.HTTPSPort != nil {
		u.HTTPSPort = raw.HTTPSPort
	}
	// 端口相等校验必须**连同库中现值**判（只传一个端口时另一个取现值）：
	// HTTP 与 HTTPS 端口相同的配置没有意义，且会制造"跳转到自己"的迷惑行为。
	if u.HTTPPort != nil || u.HTTPSPort != nil {
		cur, err := Get(c.Request.Context(), h.Pool)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "读取现配置失败", err)
			return
		}
		hp, sp := cur.HTTPPort, cur.HTTPSPort
		if u.HTTPPort != nil {
			hp = *u.HTTPPort
		}
		if u.HTTPSPort != nil {
			sp = *u.HTTPSPort
		}
		if hp == sp {
			httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST",
				"HTTP 与 HTTPS 端口不能相同", nil)
			return
		}
	}
	if err := Put(c.Request.Context(), h.Pool, u); err != nil {
		if IsValidationErr(err) {
			httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST", err.Error(), err)
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "保存失败", err)
		return
	}
	// 配置写成功 → 失效中间件的 60s 共享缓存，变更立即生效（见 InvalidateForceCache）。
	InvalidateForceCache()
	detail := map[string]any{}
	if raw.ForceHTTPS != nil {
		detail["force_https"] = *raw.ForceHTTPS
	}
	if raw.HTTPPort != nil {
		detail["http_port"] = *raw.HTTPPort
	}
	if raw.HTTPSPort != nil {
		detail["https_port"] = *raw.HTTPSPort
	}
	h.record(c, detail)
	cfg, err := Get(c.Request.Context(), h.Pool)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	fillRuntime(c, h, cfg)
	c.JSON(http.StatusOK, cfg)
}

// UploadCert POST /admin/https/cert：multipart 上传证书（cert）与私钥（key）。
// 成功 = 落盘 HTTPS_CERT_DIR + 到期日入库；**不**自动让反代生效（响应带 ApplyHint）。
func (h *Handler) UploadCert(c *gin.Context) {
	if h.CertDir == "" {
		httperr.Fail(c, http.StatusServiceUnavailable, "CERT_DIR_UNSET",
			"HTTPS_CERT_DIR 未配置：请在 compose 为 api 服务挂载证书卷并设置环境变量后再上传", nil)
		return
	}
	certPEM, err := readFormPEM(c, "cert")
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST", err.Error(), err)
		return
	}
	keyPEM, err := readFormPEM(c, "key")
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST", err.Error(), err)
		return
	}
	// 配对 + 语法校验（X509KeyPair 同时做两件事；失败=一个字节都不落盘）
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST", "证书与私钥 PEM 非法或不配对", err)
		return
	}
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "BAD_REQUEST", "证书解析失败", err)
		return
	}

	if err := writeCertFiles(h.CertDir, certPEM, keyPEM); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "WRITE_FAILED", "证书落盘失败", err)
		return
	}
	certPath := filepath.Join(h.CertDir, CertFileName)
	keyPath := filepath.Join(h.CertDir, CertKeyFileName)
	notAfter := leaf.NotAfter
	if err := Put(c.Request.Context(), h.Pool, Update{
		CertPath: &certPath, CertKeyPath: &keyPath, CertNotAfter: &notAfter,
	}); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "证书信息入库失败（文件已落盘，重传可覆盖）", err)
		return
	}
	h.record(c, map[string]any{"cert_not_after": notAfter.Format(time.RFC3339), "cert_dir": h.CertDir})

	cfg, err := Get(c.Request.Context(), h.Pool)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	fillRuntime(c, h, cfg)
	c.JSON(http.StatusOK, gin.H{
		"config":     cfg,
		"apply_hint": ApplyHint,
	})
}

// record 写一条 HTTPS 配置审计（尽力而为；Audit 为 nil 时跳过——与 transcode.Handler.record 同语义）。
func (h *Handler) record(c *gin.Context, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = audit.ActionSettingsPatch
	e.TargetType = audit.TargetSetting
	e.TargetID = "https-config"
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}

// fillRuntime 注入两个运行时字段：证书目录与当前请求协议（UI 展示「当前形态」用）。
func fillRuntime(c *gin.Context, h *Handler, cfg *Config) {
	cfg.CertDir = h.CertDir
	switch p := c.GetHeader("X-Forwarded-Proto"); p {
	case "https", "http":
		cfg.RequestProto = p
	default:
		cfg.RequestProto = ""
	}
}

// writeCertFiles 落盘证书与私钥；目录 0755、证书 0644、私钥 0600（最小权限）。
func writeCertFiles(dir string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, CertFileName), certPEM, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, CertKeyFileName), keyPEM, 0o600)
}

// readFormPEM 读 multipart 文件并做最小 PEM 语法预检（真正的配对校验在 X509KeyPair）。
func readFormPEM(c *gin.Context, field string) ([]byte, error) {
	fh, err := c.FormFile(field)
	if err != nil {
		return nil, errors.New("缺少上传文件字段 " + field)
	}
	f, err := fh.Open()
	if err != nil {
		return nil, errors.New("读取上传文件失败：" + field)
	}
	defer f.Close()
	const maxPEM = 1 << 20 // 1MB：证书链远小于此；超限即视为恶意/错传
	buf := make([]byte, maxPEM+1)
	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return nil, errors.New("上传文件为空：" + field)
	}
	if n > maxPEM {
		return nil, errors.New("上传文件超过 1MB 上限：" + field)
	}
	buf = buf[:n]
	if block, _ := pem.Decode(buf); block == nil {
		return nil, errors.New("上传文件不是合法 PEM：" + field)
	}
	return buf, nil
}
