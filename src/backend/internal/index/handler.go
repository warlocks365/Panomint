package index

// 管理端扫描导入（Job000113 / R1-b）：POST /admin/scan。
//
// 存在的理由：NAS bind mount 场景下，用户把照片放进挂载目录后系统"不识别"——
// 因为此前唯一入库路径是 worker 镜像内的 indexctl CLI，api 既没有扫描入口、启动
// 也不扫 MEDIA_ROOT（诊断见 文档/Docker挂载问题诊断报告_v1.0.md）。本端点让管理员
// 在界面上直接触发扫描；入库媒体归属**真实调用者**（彻底告别种子 owner 语义）。
//
// 路由由 cmd/api/main.go 统一接线：
//
//	authed.POST("/admin/scan", auth.RequirePerm(authStore, "admin:system"), indexH.Scan)
//
// 任务进度/终态不在本包另起查询——前端直接轮询既有 GET /admin/jobs/:id（admin:system）。
//
// ⚠️ nginx 前缀同步：/admin 已在 docker/web/Dockerfile 的「纯 API 组」正则内，无需改 nginx。

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/dirscope"
	"panoalbum/internal/httperr"
)

// asyncScanner ScanAsync 的最小抽象：*Indexer 天然实现；抽成接口只为 handler 单测能替身。
type asyncScanner interface {
	ScanAsync(ctx context.Context, root, ownerID string, onDone func(error)) (string, error)
}

// Handler POST /admin/scan 管理端扫描导入；成员端三端点（Job000123）同结构体，见 member_scan.go。
type Handler struct {
	Indexer   asyncScanner
	MediaRoot string

	// Pool 成员端（POST /scan、GET /fs/tree、GET /jobs/:id）读 users.scan_root /
	// index_jobs 归属用；管理端两端点纯文件系统操作，不需要它。
	Pool MemberDB

	// mu/running：进程内并发互斥——同一时刻只允许一个进行中的扫描任务，重复触发返回 409。
	// 标准部署为单 api 实例，进程内互斥即正确；多副本时各副本各自互斥（可接受：
	// 各自任务行都在 index_jobs 可见，最坏是并发扫两遍，hash 幂等去重不会脏库）。
	// 管理端与成员端**共用**这把锁：扫描是整机资源（CPU/IO）密集动作，单实例全局一件。
	mu      sync.Mutex
	running bool
}

// scanRequest POST /admin/scan 请求体。
type scanRequest struct {
	// Dir 待扫描目录：相对 MEDIA_ROOT（如 "photos/trip"）；空串或 "." 表示整个 MEDIA_ROOT。
	// 也接受绝对路径，但解析后必须仍位于 MEDIA_ROOT 之内（防路径穿越）。
	Dir string `json:"dir"`
}

// ErrDirEscapesRoot 待扫描目录越出 MEDIA_ROOT（路径穿越）的本端哨兵。
// handler 只对**这个哨兵**回显 err.Error()（固定中文文案，见 errEchoRegistry 登记）；
// filepath.Abs/Rel 的底层错误走 httperr.Fail 固定文案，绝不回显。
//
// 实现即 dirscope.ErrEscapesScope（Job000123 起钳制真源收敛到 internal/dirscope，
// 成员端与管理端共用同一哨兵，errors.Is 两种写法等价）。
var ErrDirEscapesRoot = dirscope.ErrEscapesScope

// resolveScanDir 把调用方给的 dir 解析为 MEDIA_ROOT 内的绝对路径。
// 空 / "." / 空白均表示 MEDIA_ROOT 本身；越界返回 ErrDirEscapesRoot。
// 钳制真源在 dirscope.Resolve，本函数仅是它在 MEDIA_ROOT 场景下的薄封装。
func resolveScanDir(mediaRoot, dir string) (string, error) {
	return dirscope.Resolve(mediaRoot, dir)
}

// relToRoot 返回 absDir 相对 MEDIA_ROOT 的斜杠展示路径（供响应回显；根本身返回 "."）。
func relToRoot(mediaRoot, absDir string) string {
	return dirscope.RelDisplay(mediaRoot, absDir)
}

// Scan POST /admin/scan：异步扫描 MEDIA_ROOT 内的目录，入库归属调用者。
//
//	202 Accepted {job_id,status,root,dir}   已受理；前端轮询 GET /admin/jobs/:id 看进度/终态
//	400 INVALID_INPUT                       请求体非 JSON / dir 越界 / 目录不存在 / 非目录
//	401 UNAUTHORIZED                        缺调用者身份（正常走 JWT 中间件到不了这里）
//	409 SCAN_RUNNING                        已有扫描任务进行中
//	500 INTERNAL                            建任务行失败
func (h *Handler) Scan(c *gin.Context) {
	var req scanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT",
			"请求体必须是 JSON：{\"dir\":\"相对 MEDIA_ROOT 的子目录（空=整个媒体根）\"}")
		return
	}

	absDir, err := resolveScanDir(h.MediaRoot, req.Dir)
	if errors.Is(err, ErrDirEscapesRoot) {
		// 本端哨兵的固定中文文案，回显安全（errEchoRegistry 已登记）。
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}
	if err != nil {
		// filepath.Abs/Rel 的底层错误（如媒体根路径非法）不回显，只给固定文案。
		httperr.Fail(c, http.StatusBadRequest, "INVALID_INPUT", "目录路径无效", err)
		return
	}
	h.runScan(c, absDir, h.MediaRoot, h.MediaRoot)
}

// runScan 扫描执行体：管理端 Scan 与成员端 ScanMine 解析出各自边界内的 absDir 后共用。
// absBoundary = 计算 dir 展示值所用的边界绝对路径（管理端 = MEDIA_ROOT；成员端 = 其
// scan_root 绝对路径）；rootDisplay = 响应 root 字段的回显值（成员端为其 scan_root
// 相对展示路径，不外泄绝对路径）。
func (h *Handler) runScan(c *gin.Context, absDir, absBoundary, rootDisplay string) {
	info, err := os.Stat(absDir)
	if err != nil {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "目录不存在或不可访问")
		return
	}
	if !info.IsDir() {
		httperr.Envelope(c, http.StatusBadRequest, "INVALID_INPUT", "dir 必须是目录而非文件")
		return
	}

	ownerID := c.GetString("user_id")
	if ownerID == "" {
		httperr.Envelope(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少调用者身份")
		return
	}

	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		httperr.Envelope(c, http.StatusConflict, "SCAN_RUNNING", "已有扫描任务进行中，请等待完成后再触发")
		return
	}
	h.running = true
	h.mu.Unlock()

	jobID, err := h.Indexer.ScanAsync(c.Request.Context(), absDir, ownerID, func(error) { h.clearRunning() })
	if err != nil {
		h.clearRunning()
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "创建扫描任务失败", err)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"job_id": jobID,
		"status": "running",
		"root":   rootDisplay,
		"dir":    dirscope.RelDisplay(absBoundary, absDir),
	})
}

// clearRunning 释放扫描互斥（onDone 回调用）。
func (h *Handler) clearRunning() {
	h.mu.Lock()
	h.running = false
	h.mu.Unlock()
}
