package folders

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"panoalbum/internal/httperr"
)

// Job000069 目录管理端点。设计红线（v2.0 §三-3）：
// media.folder_path 仍是事实源；管理操作=受权限管控的 folder_path 批量组织变更
//（与 WebDAV MOVE 同语义）+ folders 注册表维护空目录存在性与目录级授权。

// grant 目录授权元素（grants JSONB 数组元素）。
type grant struct {
	UserID string `json:"user_id"`
	Read   bool   `json:"read"`
	Write  bool   `json:"write"`
}

// normalizePath 目录路径规范化与合法性校验。
// 合法：非空、不以 / 开头结尾、无空段、无 . / .. 段、单段 ≤120 字符、总 ≤500。
func normalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, "/")
	if p == "" {
		return "", errString("目录路径不能为空")
	}
	if len(p) > 500 {
		return "", errString("目录路径过长（≤500 字符）")
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", errString("目录路径含非法段（空段/./..）")
		}
		if len(seg) > 120 {
			return "", errString("单级目录名过长（≤120 字符）")
		}
	}
	return p, nil
}

type errString string

func (e errString) Error() string { return string(e) }

// renameDestErr 目录移动目标的合法性（Job000100）：to 不得落在 from 的子树内
// （from==to 由调用点另行拦截；这里只挡 from/... 前缀——段边界比较，a/bc 不算 a/b 的子目录）。
func renameDestErr(from, to string) error {
	if strings.HasPrefix(to, from+"/") {
		return errString("不能将目录移入自身或其子目录")
	}
	return nil
}

// prefixCond folder_path 前缀条件（精确或含全部子目录），参数化。
// 占位符由调用方编号（本函数不绑参，返回的 cond 内占位符用 %d 由调用方填）。
func prefixCond(alias, path string) string {
	fp := alias + ".folder_path"
	if alias == "" {
		fp = "folder_path"
	}
	// 注意 path 是绑定参数（$n），|| '/%' 拼字面量——path 值里的 % 由 normalizePath
	// 放行（目录名允许 %），LIKE 通配语义会在端点层用 replace 转义后绑定（见 bindPathLike）。
	return "(" + fp + " = $%d OR " + fp + " LIKE $%d)"
}

// Create POST /folders {path} → 201。注册空目录（folders 行）。
func (h *Handler) Create(c *gin.Context) {
	var req struct {
		Path string `json:"path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	p, err := normalizePath(req.Path)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	uid := c.GetString("user_id")
	// 同路径已注册（任何人）→ 同形失败，不泄露存在性。
	tag, err := h.Pool.Exec(c.Request.Context(),
		`INSERT INTO folder_dirs (path, owner_id) VALUES ($1, $2) ON CONFLICT (path) DO NOTHING`, p, uid)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "CREATE_FAILED", "创建目录失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusConflict, "ALREADY_EXISTS", "目录已存在")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"path": p})
}

// Rename PATCH /folders/rename {from, to} → 204。
// 改名/移动 = folder_path 前缀批量改写（媒体 + 注册表子目录跟随），单事务。
// 权限：调用者必须是 from 目录属主（folders 行 owner，或无注册行时该前缀媒体的 owner）。
func (h *Handler) Rename(c *gin.Context) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	from, err := normalizePath(req.From)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	to, err := normalizePath(req.To)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	if from == to {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "新旧路径相同")
		return
	}
	// Job000100：目录移入自身/子目录守卫——前缀改写会把目标解析到源内部，
	// 产生 a → a/b 的自引用目录环（且注册表/media 双重改写后路径不可回退），必须明确拒绝。
	if err := renameDestErr(from, to); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	uid := c.GetString("user_id")
	ctx := c.Request.Context()

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	defer tx.Rollback(ctx)

	// 归属校验：from 已注册 → 行 owner 必须是调用者；未注册 → 前缀下不得有他人媒体。
	ok, err := h.ownFolder(ctx, tx, from, uid)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	if !ok {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "目录不存在")
		return
	}
	// 目标已注册（任何人）→ 冲突。
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM folder_dirs WHERE path = $1)`, to).Scan(&exists); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	if exists {
		httperr.Abort(c, http.StatusConflict, "ALREADY_EXISTS", "目标目录已存在")
		return
	}

	// LIKE 通配转义：from 作字面前缀绑定，'%'/'_' 转义 + ESCAPE。
	likeFrom := escapeLike(from)
	// 媒体前缀改写：from/x → to/x。substring 从 len(from)+1 取尾部（前导 / 或空）。
	// 起点是 Go 侧算出的**整数常量**（非用户输入），直接内联进 SQL 文本安全。
	// 只动调用者自己空间（owner_id=uid）的媒体——目录属主语义 = 自己的库。
	mtag, err := tx.Exec(ctx,
		fmt.Sprintf(`UPDATE media SET folder_path = $1 || substring(folder_path from %[1]d), updated_at = now()
		 WHERE owner_id = $2 AND deleted_at IS NULL
		   AND (folder_path = $3 OR folder_path LIKE $4 ESCAPE '\')`,
			len(from)+1),
		to, uid, from, likeFrom+"/%")
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	// 注册表前缀跟随（子目录也改名）。
	ftag, err := tx.Exec(ctx,
		fmt.Sprintf(`UPDATE folder_dirs SET path = $1 || substring(path from %[1]d)
		 WHERE owner_id = $2 AND (path = $3 OR path LIKE $4 ESCAPE '\')`,
			len(from)+1),
		to, uid, from, likeFrom+"/%")
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "RENAME_FAILED", "重命名失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"moved_media": mtag.RowsAffected(), "moved_folders": ftag.RowsAffected()})
}

// Delete DELETE /folders?path= → 204。软删其下全部（含子目录）未删媒体（回收站语义）
// + 删除注册行（含子目录注册）。归属校验同 Rename。
func (h *Handler) Delete(c *gin.Context) {
	p, err := normalizePath(c.Query("path"))
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	uid := c.GetString("user_id")
	ctx := c.Request.Context()

	tx, err := h.Pool.Begin(ctx)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	defer tx.Rollback(ctx)

	ok, err := h.ownFolder(ctx, tx, p, uid)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	if !ok {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "目录不存在")
		return
	}
	likeP := escapeLike(p)
	// 批量软删（与 media.SoftDelete 同语义：deleted_at + updated_at）。
	mtag, err := tx.Exec(ctx,
		`UPDATE media SET deleted_at = now(), updated_at = now()
		 WHERE owner_id = $1 AND deleted_at IS NULL
		   AND (folder_path = $2 OR folder_path LIKE $3 ESCAPE '\')`,
		uid, p, likeP+"/%")
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	ftag, err := tx.Exec(ctx,
		`DELETE FROM folder_dirs WHERE owner_id = $1 AND (path = $2 OR path LIKE $3 ESCAPE '\')`,
		uid, p, likeP+"/%")
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "DELETE_FAILED", "删除失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted_media": mtag.RowsAffected(), "deleted_folders": ftag.RowsAffected()})
}

// SetGrants PUT /folders/grants {path, grants:[{user_id,read,write}]} → 204。
// 仅目录属主。整组覆盖（幂等）。元素校验：user_id 形态合法（存在性不校验——
// 用户删除后残留无害，且 JSONB 无 FK）。
func (h *Handler) SetGrants(c *gin.Context) {
	var req struct {
		Path   string  `json:"path"`
		Grants []grant `json:"grants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "请求体格式错误")
		return
	}
	p, err := normalizePath(req.Path)
	if err != nil {
		httperr.Abort(c, http.StatusBadRequest, "BAD_PATH", err.Error())
		return
	}
	if len(req.Grants) > 200 {
		httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "授权数量超限（≤200）")
		return
	}
	for _, g := range req.Grants {
		if !isUUID(g.UserID) {
			httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "grants 含非法 user_id")
			return
		}
		if !g.Read && !g.Write {
			httperr.Abort(c, http.StatusBadRequest, "BAD_REQUEST", "授权项 read/write 至少一项为 true")
			return
		}
	}
	// 去重（同 user 后写覆盖前写）。
	seen := map[string]grant{}
	order := []string{}
	for _, g := range req.Grants {
		if _, ok := seen[g.UserID]; !ok {
			order = append(order, g.UserID)
		}
		seen[g.UserID] = g
	}
	out := make([]grant, 0, len(order))
	for _, id := range order {
		out = append(out, seen[id])
	}
	b, err := json.Marshal(out)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	uid := c.GetString("user_id")
	tag, err := h.Pool.Exec(c.Request.Context(),
		`UPDATE folder_dirs SET grants = $1 WHERE path = $2 AND owner_id = $3`, string(b), p, uid)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}
	if tag.RowsAffected() == 0 {
		httperr.Abort(c, http.StatusNotFound, "NOT_FOUND", "目录不存在")
		return
	}
	c.Status(http.StatusNoContent)
}

// ownFolder 归属校验：调用者是否为该目录属主。
// 注册行存在 → 行 owner=uid；未注册 → 前缀下不存在他人媒体（即纯个人目录）。
// 无任何痕迹（无注册行且无媒体）→ false（不存在，同形 404）。
func (h *Handler) ownFolder(ctx context.Context, tx pgx.Tx, path, uid string) (bool, error) {
	var ownerID string
	err := tx.QueryRow(ctx, `SELECT owner_id::text FROM folder_dirs WHERE path = $1`, path).Scan(&ownerID)
	if err == nil {
		return ownerID == uid, nil
	}
	if err != pgx.ErrNoRows {
		return false, err
	}
	// 未注册：前缀下媒体必须存在且全部属调用者。
	var n int
	var others int
	likeP := escapeLike(path)
	if err := tx.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE owner_id <> $1)
		 FROM media WHERE deleted_at IS NULL AND (folder_path = $2 OR folder_path LIKE $3 ESCAPE '\')`,
		uid, path, likeP+"/%").Scan(&n, &others); err != nil {
		return false, err
	}
	return n > 0 && others == 0, nil
}

// escapeLike LIKE 模式转义（%/_ → \% /\_）。
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// CanWrite 目录写权限判定（write 授权的消费点，供 upload/batch move 挂钩）：
// 无注册行 → true（自然派生目录 = 自由空间）；owner → true；
// grants 含调用者 write=true → true；否则 false。
// 根目录（path 空）恒 true。目录不存在（无注册行且无媒体痕迹）亦 true——
// 与读侧"注册行存在才可见"语义互补：写检查只管"已声明权限的目录"。
func CanWrite(ctx context.Context, q Querier, path, uid string) (bool, error) {
	if path == "" {
		return true, nil
	}
	var ownerID string
	var gtext string
	err := q.QueryRow(ctx, `SELECT owner_id::text, grants::text FROM folder_dirs WHERE path = $1`, path).Scan(&ownerID, &gtext)
	if err == pgx.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if ownerID == uid {
		return true, nil
	}
	var gs []grant
	if gtext != "" && gtext != "[]" {
		_ = json.Unmarshal([]byte(gtext), &gs)
	}
	for _, g := range gs {
		if g.UserID == uid && g.Write {
			return true, nil
		}
	}
	return false, nil
}

// Querier 最小查询接口（*pgxpool.Pool 与 pgx.Tx 均满足）。
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// isUUID 标准 36 位 UUID 形态（与 albums 包同实现，独立副本避免跨包依赖）。
func isUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i, ch := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f' || ch >= 'A' && ch <= 'F') {
			return false
		}
	}
	return true
}
