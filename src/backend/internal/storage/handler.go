package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// Handler 挂载端点。
type Handler struct {
	Pool *pgxpool.Pool
}

// mountRowScan 行扫描复用。
func mountFromRow(id, name, typ string, connRaw []byte, credsEnc string, ownerID, vis, status, lastErr, mountPath *string) (*Mount, error) {
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
	return m, nil
}

const mountCols = `id::text, name, type, conn::text, creds_enc, owner_id::text, visibility, status, last_error, mount_path`

// requirePrivileged 写操作仅 owner/admin（挂载=系统级存储能力）。
func requirePrivileged(c *gin.Context) bool {
	if !mediascope.RolePrivileged(c.GetString("role")) {
		httperr.Abort(c, http.StatusForbidden, "FORBIDDEN", "仅系统管理员可管理挂载")
		return false
	}
	return true
}

// Create POST /storage/mounts {name,type,conn,creds?} → 201。
func (h *Handler) Create(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	var req struct {
		Name    string `json:"name"`
		Type    string `json:"type"`
		Conn    conn   `json:"conn"`
		CUser   string `json:"creds_user"`
		CPass   string `json:"creds_pass"`
		CDomain string `json:"creds_domain"`
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
	var id string
	err = h.Pool.QueryRow(c.Request.Context(),
		`INSERT INTO storage_mounts (name, type, conn, creds_enc, owner_id)
		 VALUES ($1,$2,$3,$4,$5) RETURNING id::text`,
		req.Name, req.Type, string(connJSON), enc, uid).Scan(&id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建失败", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
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
		var ownerID, vis, status, lastErr, mp *string
		if err := rows.Scan(&id, &name, &typ, &connRaw, &credsEnc, &ownerID, &vis, &status, &lastErr, &mp); err != nil {
			httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
			return
		}
		m, err := mountFromRow(id, name, typ, connRaw, credsEnc, ownerID, vis, status, lastErr, mp)
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
func (h *Handler) Delete(c *gin.Context) {
	if !requirePrivileged(c) {
		return
	}
	id := c.Param("id")
	if !h.ownMount(c, id) {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
		return
	}
	tag, err := h.Pool.Exec(c.Request.Context(), `DELETE FROM storage_mounts WHERE id=$1`, id)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "挂载不存在")
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
