package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"panoalbum/internal/httperr"
	"panoalbum/internal/mediascope"
)

// jsonMarshal/jsonUnmarshal 包内别名（测试可替换钉死行为，且集中 import 点）。
var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

// conn 非敏感连接配置（按类型校验必填键）。
type conn struct {
	URL    string `json:"url"`    // webdav
	Host   string `json:"host"`   // smb/nfs
	Share  string `json:"share"`  // smb
	Export string `json:"export"` // nfs
	Port   int    `json:"port,omitempty"`
}

// Mount 挂载记录（API 输出形态，creds_enc 永不出端点）。
// LandingDir=导入落点（Job000118）：媒体库根下语义目录，创建后不可改。
type Mount struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Conn       conn   `json:"conn"`
	HasCreds   bool   `json:"has_creds"`
	OwnerID    string `json:"owner_id"`
	Visibility string `json:"visibility"`
	Status     string `json:"status"`
	LastError  string `json:"last_error,omitempty"`
	MountPath  string `json:"mount_path,omitempty"`
	LandingDir string `json:"landing_dir"`
}

// validTypes/visibilities 白名单（与 CHECK 约束同口径，前置 400 友好报错）。
var validTypes = map[string]bool{"webdav": true, "smb": true, "nfs": true}
var validVis = map[string]bool{"personal": true, "shared": true}

// normalizeConn 按类型校验连接配置必填键，返回规范化 conn。
func normalizeConn(t string, c conn) (conn, error) {
	switch t {
	case "webdav":
		if !strings.HasPrefix(c.URL, "http://") && !strings.HasPrefix(c.URL, "https://") {
			return conn{}, fmt.Errorf("webdav 需合法 url（http/https）")
		}
		if len(c.URL) > 1024 {
			return conn{}, fmt.Errorf("url 过长（≤1024）")
		}
	case "smb":
		if c.Host == "" || c.Share == "" {
			return conn{}, fmt.Errorf("smb 需 host 与 share")
		}
	case "nfs":
		if c.Host == "" || c.Export == "" {
			return conn{}, fmt.Errorf("nfs 需 host 与 export")
		}
	}
	return c, nil
}

// credsForEncrypt 提取请求凭据（user+pass 至少一项才有效；仅 pass=允许如 token 挂载）。
func credsForEncrypt(user, pass, domain string) *Creds {
	if user == "" && pass == "" && domain == "" {
		return nil
	}
	return &Creds{User: user, Pass: pass, Domain: domain}
}

// Handler 挂载端点。MediaRoot=媒体库根（Job000118：创建挂载时在其中建 landing_dir
// 语义目录并注册 folder_dirs——空目录在「文件夹」页签立即可见、可被扫描）。
type Handler struct {
	Pool      *pgxpool.Pool
	MediaRoot string
}

// normalizeLandingDir 校验并规范化导入落点（相对媒体库根的语义目录）：
//   - 去首尾斜杠、按 / 切段；空段（连续斜杠）/ 段为 "."、".." → 拒（路径穿越）
//   - 每段 slug 口径：^[a-z0-9][a-z0-9-]{0,63}$（字母或数字开头，小写字母/数字/连字符）
//   - 保留前缀黑名单：首段 "_imports"（旧技术前缀，防新旧语义混用）、任意段 "@eadir"
//     （Synology 元数据目录，大小写不敏感比对）
func normalizeLandingDir(s string) (string, error) {
	s = strings.Trim(s, "/")
	if s == "" {
		return "", fmt.Errorf("导入落点不能为空")
	}
	segs := strings.Split(s, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return "", fmt.Errorf("落点含非法路径段 %q（拒绝空段/点号/穿越）", seg)
		}
		if strings.EqualFold(seg, "@eadir") {
			return "", fmt.Errorf("落点段 %q 为系统保留目录（Synology 元数据）", seg)
		}
		if !landingDirSegRe.MatchString(seg) {
			return "", fmt.Errorf("落点段 %q 须为字母/数字开头的小写字母·数字·连字符（≤64 位）", seg)
		}
	}
	if segs[0] == "_imports" {
		return "", fmt.Errorf("落点前缀 _imports 为系统保留（旧导入目录），请改用 imports/<名称> 语义命名")
	}
	return strings.Join(segs, "/"), nil
}

// landingDirSegRe 落点逐段 slug（与 LocationNameRe 同族、放宽到数字开头+64 位）。
var landingDirSegRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// slugifyName 名称 → slug（默认落点 imports/<slug> 用）：小写，非字母数字压缩为连字符。
func slugifyName(s string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// mountRowScan 行扫描复用。
func mountFromRow(id, name, typ string, connRaw []byte, credsEnc string, ownerID, vis, status, lastErr, mountPath, landingDir *string) (*Mount, error) {
	m := &Mount{ID: id, Name: name, Type: typ}
	if err := jsonUnmarshal(connRaw, &m.Conn); err != nil {
		return nil, err
	}
	m.HasCreds = credsEnc != ""
	if ownerID != nil {
		m.OwnerID = *ownerID
	}
	if vis != nil {
		m.Visibility = *vis
	}
	if status != nil {
		m.Status = *status
	}
	if lastErr != nil {
		m.LastError = *lastErr
	}
	if mountPath != nil {
		m.MountPath = *mountPath
	}
	if landingDir != nil {
		m.LandingDir = *landingDir
	}
	return m, nil
}

const mountCols = `id::text, name, type, conn::text, creds_enc, owner_id::text, visibility, status, last_error, mount_path, landing_dir`

// requirePrivileged 写操作仅 owner/admin（挂载=系统级存储能力）。
func requirePrivileged(c *gin.Context) bool {
	if !mediascope.RolePrivileged(c.GetString("role")) {
		httperr.Abort(c, http.StatusForbidden, "FORBIDDEN", "仅系统管理员可管理挂载")
		return false
	}
	return true
}

// Create POST /storage/mounts {name,type,conn,creds?,landing_dir?} → 201。
// landing_dir 缺省 = imports/<name-slug>（Job000118 裁决③）；创建后不可改。
// 落点生效三步（本函数）：校验 → MkdirAll(MEDIA_ROOT/landing_dir) → 同事务写
// storage_mounts + 注册 folder_dirs（存在即目录：空挂载目录在「文件夹」页签立即可见）。
// 幂等：folder_dirs 注册 ON CONFLICT DO NOTHING（同名目录已存在时保留原行）。
func (h *Handler) Create(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	var req struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Conn       conn   `json:"conn"`
		CUser      string `json:"creds_user"`
		CPass      string `json:"creds_pass"`
		CDomain    string `json:"creds_domain"`
		LandingDir string `json:"landing_dir"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 128 {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "name 必填且 ≤128 字符")
		return
	}
	if !validTypes[req.Type] {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "type 仅支持 webdav|smb|nfs")
		return
	}
	nc, err := normalizeConn(req.Type, req.Conn)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_CONN", err.Error())
		return
	}
	// 落点：缺省 imports/<slug>（裁决③），显式给则 normalize 校验。
	landingDir := strings.TrimSpace(req.LandingDir)
	if landingDir == "" {
		slug := slugifyName(req.Name)
		if slug == "" {
			slug = "import"
		}
		landingDir = "imports/" + slug
	}
	landingDir, err = normalizeLandingDir(landingDir)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_LANDING_DIR", err.Error())
		return
	}
	enc, err := EncryptCreds(credsForEncrypt(req.CUser, req.CPass, req.CDomain))
	if err != nil {
		if errors.Is(err, ErrNoCipherKey) {
			httperr.Abort(c, http.StatusBadRequest, "CIPHER_KEY_MISSING", "服务端未配置 STORAGE_CIPHER_KEY，无法存储凭据")
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	connJSON, _ := jsonMarshal(nc)
	uid := c.GetString("user_id")
	// 先建语义目录（库外副作用先行；后续库失败仅留无害空目录——未注册 folder_dirs
	// 时不在文件夹树出现，不污染用户视图）。
	if err := os.MkdirAll(filepath.Join(h.MediaRoot, landingDir), 0o755); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建落点目录失败", err)
		return
	}
	// 同事务：storage_mounts 行 + folder_dirs 注册（一致性：注册失败则整创建回滚）。
	ctx := c.Request.Context()
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx,
		`INSERT INTO storage_mounts (name, type, conn, creds_enc, owner_id, landing_dir)
		 VALUES ($1,$2,$3,$4,$5,$6) RETURNING id::text`,
		req.Name, req.Type, string(connJSON), enc, uid, landingDir).Scan(&id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO folder_dirs (path, owner_id) VALUES ($1,$2) ON CONFLICT (path) DO NOTHING`,
		landingDir, uid); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "注册目录失败", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "landing_dir": landingDir})
}

// List GET /storage/mounts → 挂载列表。owner/admin 见全量，其余仅自己的。
func (h *Handler) List(c *gin.Context) {
	uid := c.GetString("user_id")
	where := ""
	args := []any{}
	if !mediascope.RolePrivileged(c.GetString("role")) {
		where = " WHERE owner_id = $1"
		args = append(args, uid)
	}
	rows, err := h.Pool.Query(c.Request.Context(),
		`SELECT `+mountCols+` FROM storage_mounts`+where+` ORDER BY created_at DESC`, args...)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	defer rows.Close()
	out := []Mount{}
	for rows.Next() {
		var id, name, typ, credsEnc string
		var connRaw []byte
		var ownerID, vis, status, lastErr, mp, ld *string
		if err := rows.Scan(&id, &name, &typ, &connRaw, &credsEnc, &ownerID, &vis, &status, &lastErr, &mp, &ld); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		m, err := mountFromRow(id, name, typ, connRaw, credsEnc, ownerID, vis, status, lastErr, mp, ld)
		if err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		out = append(out, *m)
	}
	c.JSON(http.StatusOK, gin.H{"mounts": out})
}

// ownMount 归属校验：owner/admin 全通；其余须 owner_id=调用者。无权/不存在同形 404。
func (h *Handler) ownMount(c *gin.Context, id string) bool {
	uid := c.GetString("user_id")
	if mediascope.RolePrivileged(c.GetString("role")) {
		return true
	}
	var exists bool
	if err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT EXISTS(SELECT 1 FROM storage_mounts WHERE id=$1 AND owner_id=$2)`, id, uid).Scan(&exists); err != nil || !exists {
		return false
	}
	return true
}

// Patch PATCH /storage/mounts/:id {name?,conn?,creds_*,visibility?,status?} → 204。
// 凭据更新：creds_pass 为 "********"（掩码）表示不改动（前端回显惯例）。
const credMask = "********"

func (h *Handler) Patch(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	if !h.ownMount(c, id) {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	var req struct {
		Name       *string `json:"name"`
		Conn       *conn   `json:"conn"`
		CUser      *string `json:"creds_user"`
		CPass      *string `json:"creds_pass"`
		CDomain    *string `json:"creds_domain"`
		Visibility *string `json:"visibility"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	ctx := c.Request.Context()
	// 先取现行行（type 决定 conn 校验口径；旧 creds 用于掩码合并）。
	var typ string
	var oldEnc string
	var oldC conn
	var connRaw []byte
	if err := h.Pool.QueryRow(ctx,
		`SELECT type, creds_enc, conn::text FROM storage_mounts WHERE id=$1`, id).Scan(&typ, &oldEnc, &connRaw); err != nil {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	_ = jsonUnmarshal(connRaw, &oldC)

	if req.Conn != nil {
		nc, err := normalizeConn(typ, *req.Conn)
		if err != nil {
			httperr.Abort(c, http.StatusBadRequest, "BAD_CONN", err.Error())
			return
		}
		b, _ := jsonMarshal(nc)
		if _, err := h.Pool.Exec(ctx, `UPDATE storage_mounts SET conn=$2, updated_at=now() WHERE id=$1`, id, string(b)); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
	}
	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" || len(n) > 128 {
			httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "name 必填且 ≤128 字符")
			return
		}
		if _, err := h.Pool.Exec(ctx, `UPDATE storage_mounts SET name=$2, updated_at=now() WHERE id=$1`, id, n); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
	}
	if req.Visibility != nil {
		if !validVis[*req.Visibility] {
			httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "visibility 仅支持 personal|shared")
			return
		}
		if _, err := h.Pool.Exec(ctx, `UPDATE storage_mounts SET visibility=$2, updated_at=now() WHERE id=$1`, id, *req.Visibility); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
			return
		}
	}
	// 凭据更新：掩码=不改动；全空=清除凭据；否则重加密覆盖。
	if req.CPass != nil && *req.CPass != credMask {
		user, pass, domain := "", "", ""
		if req.CUser != nil {
			user = *req.CUser
		}
		pass = *req.CPass
		if req.CDomain != nil {
			domain = *req.CDomain
		}
		if user == "" && pass == "" && domain == "" {
			if _, err := h.Pool.Exec(ctx, `UPDATE storage_mounts SET creds_enc='', updated_at=now() WHERE id=$1`, id); err != nil {
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
				return
			}
		} else {
			enc, err := EncryptCreds(&Creds{User: user, Pass: pass, Domain: domain})
			if err != nil {
				if errors.Is(err, ErrNoCipherKey) {
					httperr.Abort(c, http.StatusBadRequest, "CIPHER_KEY_MISSING", "服务端未配置 STORAGE_CIPHER_KEY，无法存储凭据")
					return
				}
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
				return
			}
			if _, err := h.Pool.Exec(ctx, `UPDATE storage_mounts SET creds_enc=$2, updated_at=now() WHERE id=$1`, id, enc); err != nil {
				httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
				return
			}
		}
	}
	c.Status(http.StatusNoContent)
}

// Delete DELETE /storage/mounts/:id → 204。worker 侧卸载由状态机轮询处理（下轮）。
// Job000118：随删清理 folder_dirs 注册行（含已导入媒体时 media.folder_path 仍是
// 目录存在事实源，目录在文件夹树中靠媒体行撑着不受影响）；落地目录本体保留
// （用户数据不随挂载删除清除）。
func (h *Handler) Delete(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	if !h.ownMount(c, id) {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	ctx := c.Request.Context()
	// 先取落点（删行后无从得知）。
	var landingDir string
	if err := h.Pool.QueryRow(ctx,
		`SELECT landing_dir FROM storage_mounts WHERE id=$1`, id).Scan(&landingDir); err != nil {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM storage_mounts WHERE id=$1`, id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	if _, err := tx.Exec(ctx, `DELETE FROM folder_dirs WHERE path=$1`, landingDir); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "清理目录注册失败", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Test POST /storage/mounts/:id/test → {ok, checks[]}。
// v1 仅做配置/凭据可解密校验（真实连通性由 worker 执行器挂载时判定，下轮）。
func (h *Handler) Test(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	if !h.ownMount(c, id) {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	var typ, enc string
	var connRaw []byte
	if err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT type, creds_enc, conn::text FROM storage_mounts WHERE id=$1`, id).Scan(&typ, &enc, &connRaw); err != nil {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	checks := []string{}
	var cc conn
	if err := jsonUnmarshal(connRaw, &cc); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "TEST_FAILED", "配置解析失败", err)
		return
	}
	if _, err := normalizeConn(typ, cc); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "checks": append(checks, "连接配置: "+err.Error())})
		return
	}
	checks = append(checks, "连接配置: 合法")
	if enc != "" {
		if _, err := DecryptCreds(enc); err != nil {
			c.JSON(http.StatusOK, gin.H{"ok": false, "checks": append(checks, "凭据解密: 失败（密钥不匹配或密文损坏）")})
			return
		}
		checks = append(checks, "凭据解密: 成功")
	} else {
		checks = append(checks, "凭据: 无（匿名挂载）")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "checks": checks})
}
