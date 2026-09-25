package auth

// 扫描根目录分配（Job000123）：
//
//	PUT /admin/users/:id/scan-root   管理员为普通账号分配/取消扫描根目录（admin:users）
//	GET /user/scan-root              任意登录用户查自己的分配（authed，成员扫描面板前置）
//
// 语义（迁移 00039）：users.scan_root
//   - NULL           = 未分配 → 成员侧 POST /scan、GET /fs/tree、GET /jobs/:id 一律
//     403 SCAN_ROOT_REQUIRED（fail-closed）；
//   - ""（空串）     = 已分配，根目录 = MEDIA_ROOT 本身；
//   - "photos/trip"  = 已分配，根目录 = MEDIA_ROOT/photos/trip。
//
// 写入前校验（管理端是唯一写入路径，全部在此把关）：
//   1. dirscope.Resolve 钳在 MEDIA_ROOT 内（路径穿越 400）；
//   2. os.Stat 必须是**真实存在的物理目录**（需求明文：必须是真实物理目录而非虚拟目录）；
//   3. dirscope.HasReserved 命中 _imports 保留段一律 400（任意层级）。
// 存储值统一为 dirscope.RelDisplay 的归一化斜杠路径，避免 "photos/../photos" 这类
// 等价异形混进库。

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/dirscope"
	"panoalbum/internal/httperr"
)

// scanRootBody PUT /admin/users/:id/scan-root 请求体。
type scanRootBody struct {
	// ScanRoot 目标扫描根：nil（JSON null 或字段缺失）= 取消分配；"" = MEDIA_ROOT 本身；
	// 其余 = MEDIA_ROOT 下的相对目录。
	ScanRoot *string `json:"scan_root"`
}

// PutUserScanRoot PUT /admin/users/:id/scan-root（需 admin:users 权限）。
//
//	200 {user}          更新后的用户（含 scan_root）
//	400 INVALID_INPUT   请求体非 JSON / 目录越界 / 非真实目录 / 命中保留段
//	401 UNAUTHORIZED    缺调用者身份
//	404 USER_NOT_FOUND  目标用户不存在
//	500 INTERNAL        写入失败
//
// 审计：admin.user.update，detail.scan_root = 归一化路径（取消分配为 null）。
// 注意本端点**不受**自锁/最后 owner 守卫约束——它不改角色/状态，把自己设个扫描根
// 无害（管理端扫描入口本就不看 scan_root）。
func (h *Handler) PutUserScanRoot(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	// 用 RawMessage 先取一层：区分「字段缺失」（400，防误清空）与「显式 null」
	//（取消分配）。若直接绑 *string，这两种情况会被压成同一个 nil，误发 {}
	// 就把别人的分配抹掉了。
	var raw map[string]json.RawMessage
	if err := c.ShouldBindJSON(&raw); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	field, present := raw["scan_root"]
	if !present {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", "请求体必须包含 scan_root 字段（null = 取消分配）")
		return
	}
	var in scanRootBody
	if string(field) != "null" {
		if err := json.Unmarshal(field, &in.ScanRoot); err != nil {
			errResp(c, http.StatusBadRequest, "INVALID_INPUT", "scan_root 必须是字符串或 null")
			return
		}
	}

	target, err := h.Store.GetUser(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}

	// 归一化 + 三层校验（nil = 取消分配，跳过路径校验）。错误全是本端固定中文文案。
	var storeVal *string
	if in.ScanRoot != nil {
		v, err := normalizeScanRoot(h.MediaRoot, *in.ScanRoot)
		if err != nil {
			errResp(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
			return
		}
		storeVal = &v
	}

	u, err := h.Store.SetUserScanRoot(ctx, id, storeVal)
	if errors.Is(err, ErrUserNotFound) {
		errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		return
	}
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "更新扫描根目录失败")
		return
	}

	detail := map[string]any{"scan_root": storeValOrNull(storeVal)}
	h.record(c, audit.ActionUserUpdate, audit.TargetUser, target.ID, detail)

	c.JSON(http.StatusOK, gin.H{"user": u})
}

// normalizeScanRoot 归一化 + 校验 scan_root 输入（纯文件系统探测，不碰 DB）。
//
// raw 取值："" / "." → 媒体根本身（返回 ""）；其余 → 解析为 MEDIA_ROOT 内真实目录
// 的归一化展示路径（斜杠相对路径）。校验链：
//  1. dirscope.Resolve 钳在 MEDIA_ROOT 内（路径穿越 → dirscope.ErrEscapesScope）；
//  2. os.Stat 必须是**真实存在的物理目录**（需求明文：真实物理目录而非虚拟目录）；
//  3. dirscope.HasReserved 命中 _imports 保留段一律拒绝（任意层级）。
//
// 返回的错误全部是本端固定中文文案（可直接 400 回显，errEchoRegistry 已登记），
// 不回显任何 os/dirscope 底层原文。抽成独立函数是为了能穷举单测（守卫类逻辑）。
func normalizeScanRoot(mediaRoot, raw string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "." {
		v = ""
	}
	if v == "" {
		return "", nil
	}
	abs, err := dirscope.Resolve(mediaRoot, v)
	if err != nil {
		// ErrEscapesScope 原样上抛（固定中文文案，登记在案）；其余底层错误收敛为固定文案。
		if errors.Is(err, dirscope.ErrEscapesScope) {
			return "", err
		}
		return "", errors.New("目录路径无效")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", errors.New("扫描根目录必须是真实存在的物理目录")
	}
	if !info.IsDir() {
		return "", errors.New("扫描根目录必须是目录而非文件")
	}
	display := dirscope.RelDisplay(mediaRoot, abs)
	if dirscope.HasReserved(display) {
		return "", errors.New("扫描根目录不能位于系统保留区（_imports）")
	}
	return display, nil
}

// storeValOrNull 审计 detail 用：nil → nil（JSON null），非 nil → 解引用值。
// 键名 scan_root 不含敏感子串，不会被 RedactDetail 剔除。
func storeValOrNull(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// GetMyScanRoot GET /user/scan-root（任意登录用户）→ 自己的扫描根分配。
//
//	200 {assigned, scan_root}   scan_root：null=未分配；""=整个媒体根；其余=相对路径
//	401 UNAUTHORIZED            缺调用者身份
//	500 INTERNAL                查询失败
//
// 成员扫描面板挂载前先打这个端点：assigned=false 直接展示「联系管理员」引导，
// 不发扫描请求。owner/admin 走 /admin/scan 不受 scan_root 限制，本端点对他们是纯信息。
func (h *Handler) GetMyScanRoot(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		errResp(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少调用者身份")
		return
	}
	scanRoot, err := h.Store.ScanRootOf(c.Request.Context(), userID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "INTERNAL", "查询扫描根目录失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"assigned":  scanRoot != nil,
		"scan_root": scanRoot,
	})
}
