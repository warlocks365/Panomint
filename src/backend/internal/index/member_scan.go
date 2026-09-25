package index

// 成员端扫描导入（Job000123）：POST /scan、GET /fs/tree、GET /jobs/:id。
//
// 背景：管理端扫描（POST /admin/scan，Job000113）一直只有 admin:system 能用——
// 普通成员想把 NAS 挂载目录里已有的照片导入自己的库，没有任何入口。本组端点把
// 扫描能力开放给普通账号，但**边界不再是整个 MEDIA_ROOT**，而是管理员在
// users.scan_root 里分配的目录（迁移 00039）：
//
//   - scan_root = NULL        → 未分配：三个端点一律 403 SCAN_ROOT_REQUIRED（fail-closed，
//     「没有根目录」与「根目录就是媒体根」必须区分开，不能默认放开）；
//   - scan_root = ''          → 已分配，根目录 = MEDIA_ROOT 本身；
//   - scan_root = 'photos/x'  → 已分配，根目录 = MEDIA_ROOT/photos/x。
//
// 钳制链路：请求 dir 先经 dirscope.Resolve 钳在 scan_root 之内，再算它相对 MEDIA_ROOT
// 的展示路径，命中保留段 _imports（任意层级）一律 400——挂载导入落点是 storagectl
// 的系统保留区，成员绝不可见/不可扫。
//
// 路由（cmd/api/main.go，nginx 第 2 组纯 API 前缀已含 scan|fs|jobs）：
//
//	authed.POST("/scan", permWrite, indexH.ScanMine)
//	authed.GET("/fs/tree", permRead, indexH.ListDirTreeMine)
//	authed.GET("/jobs/:id", permRead, indexH.MyJobStatus)
//
// 入库归属**真实调用者**（ScanAsync 的 ownerID = JWT user_id），与管理端语义一致。

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"

	"panoalbum/internal/dirscope"
	"panoalbum/internal/httperr"
)

// ErrScanRootRequired 调用者未被分配扫描根目录（users.scan_root IS NULL）。
// 固定中文文案，errEchoRegistry 已登记（MustFix=false）。
var ErrScanRootRequired = errors.New("管理员尚未为你分配扫描根目录，请联系管理员设置后再试")

// MemberDB 成员端点的最小 DB 依赖（*pgxpool.Pool 天然实现；handler 单测用脚本替身）。
// 刻意只暴露 QueryRow：本组端点只需要"按 id 取一列/取一行"。
type MemberDB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// memberJobUUIDRe MyJobStatus 的 :id 格式校验（与 audit.uuidRe 同式；id 进 SQL 带
// ::uuid 转换，格式不对会让 PG 抛类型错误 → 500，把"调用方传错 id"伪装成"服务故障"）。
var memberJobUUIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// memberRoot 取调用者的扫描根：查 users.scan_root → 解析为 MEDIA_ROOT 内的绝对路径。
//
// 返回的 display 是 scan_root 的展示值（'.'=媒体根本身）；调用方把它作为
// runScan/树响应的 root 回显。三个成员端点共用这条前置链，钳制口径天然一致。
func (h *Handler) memberRoot(c *gin.Context) (absRoot, display string, ok bool) {
	userID := c.GetString("user_id")
	if userID == "" {
		httperr.Envelope(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少调用者身份")
		return "", "", false
	}
	var scanRoot *string
	err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT scan_root FROM users WHERE id = $1::uuid`, userID).Scan(&scanRoot)
	if err != nil {
		// 用户行必存在（JWT 中间件已验）；查询失败按 500 处理。
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "查询扫描根目录失败", err)
		return "", "", false
	}
	if scanRoot == nil {
		httperr.Envelope(c, http.StatusForbidden, "SCAN_ROOT_REQUIRED", ErrScanRootRequired.Error())
		return "", "", false
	}
	absRoot, err = dirscope.Resolve(h.MediaRoot, *scanRoot)
	if err != nil {
		// 分配后媒体目录被移动/删除等运营态错位：不是调用方的错，500 + 服务端日志。
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "扫描根目录配置无效", err)
		return "", "", false
	}
	if info, err := os.Stat(absRoot); err != nil || !info.IsDir() {
		httperr.Envelope(c, http.StatusInternalServerError, "INTERNAL", "扫描根目录不存在或不可访问，请联系管理员重新分配")
		return "", "", false
	}
	return absRoot, dirscope.RelDisplay(h.MediaRoot, absRoot), true
}

// resolveMemberDir 把成员请求的 dir（相对其 scan_root）解析为绝对路径，
// 并做保留段黑名单（相对 MEDIA_ROOT 逐段查 _imports）。越界/命中保留段返回
// ( "", false ) 且已写响应。
func (h *Handler) resolveMemberDir(c *gin.Context, absRoot, dir string) (string, bool) {
	absDir, err := dirscope.Resolve(absRoot, dir)
	if errors.Is(err, dirscope.ErrEscapesScope) {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return "", false
	}
	if err != nil {
		httperr.Fail(c, http.StatusBadRequest, "INVALID_INPUT", "目录路径无效", err)
		return "", false
	}
	// Resolve 已保证结果在 MEDIA_ROOT 内，RelDisplay 必成功；命中保留段一律 400——
	// 挂载导入落点是 storagectl 的系统保留区，成员绝不可见/不可扫。
	rel := dirscope.RelDisplay(h.MediaRoot, absDir)
	if dirscope.HasReserved(rel) {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "该目录为系统保留区，不可扫描或浏览")
		return "", false
	}
	return absDir, true
}

// ScanMine POST /scan：成员扫描自己的 scan_root 内的目录，入库归属调用者。
//
//	202 {job_id,status,root,dir}   root=scan_root 展示值；dir=相对 scan_root
//	400 INVALID_INPUT              dir 越界 / 命中保留段 / 目录不存在 / 非目录
//	401 UNAUTHORIZED               缺调用者身份
//	403 SCAN_ROOT_REQUIRED         未分配扫描根目录
//	409 SCAN_RUNNING               已有扫描任务进行中（与管理端共用互斥）
//	500 INTERNAL                   查询 scan_root / 建任务行失败
func (h *Handler) ScanMine(c *gin.Context) {
	var req scanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT",
			"请求体必须是 JSON：{\"dir\":\"相对扫描根的子目录（空=整个扫描根）\"}")
		return
	}
	absRoot, display, ok := h.memberRoot(c)
	if !ok {
		return
	}
	absDir, ok := h.resolveMemberDir(c, absRoot, req.Dir)
	if !ok {
		return
	}
	h.runScan(c, absDir, absRoot, display)
}

// ListDirTreeMine GET /fs/tree?dir=<相对 scan_root>：成员目录树（懒加载单层）。
//
// 与管理端 ListDirTree 同构，差别只在边界与保留段过滤：根 = scan_root；
// 名为 _imports 的子目录不下发（保留区对成员不可见，前端因此永远不会画出它）。
// unreadable/readable 语义与管理端一致（无权限 = 锁定态而非报错）。
func (h *Handler) ListDirTreeMine(c *gin.Context) {
	absRoot, display, ok := h.memberRoot(c)
	if !ok {
		return
	}
	absDir, ok := h.resolveMemberDir(c, absRoot, c.Query("dir"))
	if !ok {
		return
	}
	info, err := os.Stat(absDir)
	if err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "目录不存在或不可访问")
		return
	}
	if !info.IsDir() {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "dir 必须是目录而非文件")
		return
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			c.JSON(http.StatusOK, gin.H{
				"root":       display,
				"dir":        dirscope.RelDisplay(absRoot, absDir),
				"unreadable": true,
				"items":      []treeEntry{},
			})
			return
		}
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "列举目录失败", err)
		return
	}

	parentRel := dirscope.RelDisplay(absRoot, absDir)
	items := make([]treeEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue // 文件不上树；符号链接（含指向目录的）同样被 lstat 语义排除
		}
		if e.Name() == dirscope.ReservedSeg {
			continue // 保留区不下发——树里根本不会出现 _imports
		}
		rel := e.Name()
		if parentRel != "." {
			rel = parentRel + "/" + e.Name()
		}
		items = append(items, treeEntry{
			Name:     e.Name(),
			Rel:      rel,
			Readable: dirReadable(filepath.Join(absDir, e.Name())),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"root":       display,
		"dir":        parentRel,
		"unreadable": false,
		"items":      items,
	})
}

// memberJob 成员任务视图（GET /jobs/:id 响应）。只覆盖 index_jobs——成员触发的
// 扫描任务只会进这张表；transcode_jobs 是系统自动任务，成员无权按 id 窥探。
type memberJob struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	Total      *int       `json:"total"`
	Processed  *int       `json:"processed"`
	Progress   *float64   `json:"progress"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// MyJobStatus GET /jobs/:id：查**自己触发**的扫描任务进度/终态。
//
// 归属过滤写在 SQL 里（user_id = 调用者）：别人的任务 id 查不到 → 404 与
// 「不存在」逐字节同形（无权=不可见，404 不是存在性预言机）。
//
//	200 memberJob
//	400 INVALID_INPUT   id 非 UUID
//	401 UNAUTHORIZED    缺调用者身份
//	403 SCAN_ROOT_REQUIRED  未分配扫描根目录
//	404 JOB_NOT_FOUND   任务不存在或不属于你
func (h *Handler) MyJobStatus(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		httperr.Envelope(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少调用者身份")
		return
	}
	// 未分配扫描根的账号连任务查询也不给（与其无任何扫描入口的口径一致）。
	var scanRoot *string
	err := h.Pool.QueryRow(c.Request.Context(),
		`SELECT scan_root FROM users WHERE id = $1::uuid`, userID).Scan(&scanRoot)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "查询扫描根目录失败", err)
		return
	}
	if scanRoot == nil {
		httperr.Envelope(c, http.StatusForbidden, "SCAN_ROOT_REQUIRED", ErrScanRootRequired.Error())
		return
	}

	id := c.Param("id")
	if !memberJobUUIDRe.MatchString(id) {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "任务 id 必须是 UUID")
		return
	}

	var j memberJob
	var total, processed *int
	err = h.Pool.QueryRow(c.Request.Context(), `
		SELECT id, kind, status, total, processed, started_at, finished_at, created_at
		FROM index_jobs WHERE id = $1::uuid AND user_id = $2::uuid`, id, userID).
		Scan(&j.ID, &j.Kind, &j.Status, &total, &processed,
			&j.StartedAt, &j.FinishedAt, &j.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		httperr.Envelope(c, http.StatusNotFound, "JOB_NOT_FOUND", "任务不存在")
		return
	}
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "查询任务失败", err)
		return
	}
	j.Total = total
	j.Processed = processed
	j.Progress = memberJobProgress(total, processed)
	c.JSON(http.StatusOK, j)
}

// memberJobProgress 进度（纯函数）：total 缺失或为 0 时返回 nil（"未知"≠"0%"）。
func memberJobProgress(total, processed *int) *float64 {
	if total == nil || processed == nil || *total <= 0 {
		return nil
	}
	p := float64(*processed) / float64(*total)
	return &p
}
