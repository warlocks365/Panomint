package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
	"panoalbum/internal/httperr"
)

// validateChangePassword 自助改密的入参校验（纯函数，便于穷举）。
//
// 只校验"不依赖数据库"的部分：长度下限沿用全局 MinPasswordLen（与 POST /admin/users 一致），
// 且新口令不得与当前口令相同（改了等于没改，还白吊销一轮会话）。
// 旧口令是否**正确**必须查库比对，不在这里判。
func validateChangePassword(current, next string) error {
	if current == "" {
		return errors.New("缺少当前密码")
	}
	if next == "" {
		return errors.New("缺少新密码")
	}
	if len(next) < MinPasswordLen {
		return fmt.Errorf("新密码至少 %d 位", MinPasswordLen)
	}
	if next == current {
		return errors.New("新密码不能与当前密码相同")
	}
	return nil
}

// ChangePassword PUT /user/password —— 用户自助修改自己的口令（Job000034）。
//
// 补的是"用户无法自助改密"这个真实产品缺口：此前唯一途径是 PATCH /admin/users/:id，
// 需要 admin:users 权限 —— 普通用户改不了自己的密码。
//
// 安全要点（每一项都有理由，改动前请重读）：
//   - **必须提供当前密码**：只凭已登录会话即可改密，等于"捡到开着的电脑即可换锁"，
//     旧口令这道验证是改密与"会话被劫持后改密"之间唯一的屏障。
//   - **成功后吊销该用户全部会话**：refresh 不验口令，若不改密后吊销，攻击者此前盗走的
//     refresh token 在改密后**仍然有效**（能不断换出新的 access token）。
//     ⚠️ 这与管理员重置密码（UpdateUser）**有意不同** —— 那边目前不吊销会话，
//     属于另一个待评估项；自助改密必须吊销。
//   - 吊销失败**不阻断**响应（沿用 UpdateUser 的约定）：口令已改成功，让请求"看起来失败"
//     只会误导用户以为密码没改成；失败只记服务端日志。
//   - 全程审计（action=user.password.change）；口令类字段一律不入 detail
//     （键名命中敏感词会被 RedactDetail 整键剔除，且审计表永久保留）。
//
// 前端契约：成功后**登出并引导用新口令重新登录** —— 吊销全部会话后旧 refresh 已失效，
// 留在原页面也只是拖到 access token 过期；立即重登同时验证新口令可用。
func (h *Handler) ChangePassword(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST",
			`请求体需为 {"current_password":"…","new_password":"…"}`)
		return
	}
	if err := validateChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}

	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	if !h.verifyCurrentPassword(c, userID, req.CurrentPassword) {
		return
	}

	newHash, err := HashPassword(req.NewPassword)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "HASH_FAILED", "口令散列失败", err)
		return
	}
	if err := h.Store.SetPassword(ctx, userID, newHash); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}

	if err := h.Store.RevokeAllSessions(ctx, userID); err != nil {
		log.Printf("auth: 自助改密后吊销会话失败（user=%s）: %v", userID, err)
	}

	h.record(c, audit.ActionPasswordChange, audit.TargetUser, userID, map[string]any{})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GenerateAppPassword 生成应用密码明文（Job000098）——服务端随机、只此一次下发。
//
// 格式：`pano-` + 40 位 hex（20 字节 crypto/rand，160 位熵）。前缀让使用者在
// 客户端配置里一眼认出它是什么；hex 字符集对各类 DAV/HTTP 客户端零兼容风险
// （不含 +/=/-/_ 等可能被不同客户端特殊对待的符号）。45 字符远低 bcrypt 72 字节上限。
// 调用方负责：bcrypt 散列入库、明文只在响应里出现一次、绝不落日志/审计。
func GenerateAppPassword() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "pano-" + hex.EncodeToString(b), nil
}

// AppPasswordStatus GET /user/app-password → {set: bool}（Job000098）。
//
// 只回"是否已设置"，**永不回散列**：散列即便不可逆，也是"该账号开了这道凭据"的信号，
// 而设置页只需要状态徽章。明文本来就不可能回（服务端只存散列）。
func (h *Handler) AppPasswordStatus(c *gin.Context) {
	hash, err := h.Store.AppPasswordHash(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"set": hash != ""})
}

// AppPasswordGenerate PUT /user/app-password（Job000098）——生成/轮换应用密码。
//
// 补的是 Job000055 遗留的产品缺口：WebDAV Basic 认证早已支持"邮箱+应用密码"，
// 但此前没有任何端点能设置它——列是 DDL 预留的，消费方（dav.go）也是现成的，
// 唯独"怎么把这个值写进去"缺了。没有它，第三方客户端只能拿**主密码**配 DAV，
// 主密码一泄漏就是全部；应用密码可单独轮换/清除，泄漏面小得多。
//
// 安全要点（每一项都有理由，改动前请重读）：
//   - **必须提供当前密码**（同 ChangePassword 的"捡到开着的电脑即可换锁"防线）：
//     只凭已登录会话即可铸造永久凭据的话，access token 一泄漏，攻击者就能给自己
//     造一把长期钥匙 —— 而应用密码正是为"主密码不能给客户端"这个场景存在的。
//   - **轮换语义**：每次调用都生成新随机串并覆盖旧散列 —— 旧应用密码立即失效，
//     使用该密码的 DAV 客户端必须更新。单列单值（app_password_hash 是单数列）。
//   - **明文只在响应里出现一次**：服务端只存 bcrypt 散列，之后任何接口都取不回明文；
//     因此绝不能进日志/审计 detail（与 MFA secret 同一纪律）。
//   - **不吊销会话**：应用密码不走 /auth/login，不创建任何会话（DAV Basic 逐请求校验），
//     轮换/清除都不影响现有登录态 —— 这与自助改密"必须吊销"**有意不同**。
//   - 审计 action=user.app_password，detail 只记 operation=rotate（键名无敏感子串）。
func (h *Handler) AppPasswordGenerate(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST",
			`请求体需为 {"current_password":"…"}`)
		return
	}
	if req.CurrentPassword == "" {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", "缺少当前密码")
		return
	}

	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	if !h.verifyCurrentPassword(c, userID, req.CurrentPassword) {
		return
	}

	plain, err := GenerateAppPassword()
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "GENERATE_FAILED", "生成应用密码失败", err)
		return
	}
	hash, err := HashPassword(plain)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "HASH_FAILED", "口令散列失败", err)
		return
	}
	if err := h.Store.SetAppPassword(ctx, userID, hash); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}

	h.record(c, audit.ActionAppPassword, audit.TargetUser, userID,
		map[string]any{"operation": "rotate"})
	c.JSON(http.StatusOK, gin.H{"app_password": plain})
}

// AppPasswordClear POST /user/app-password/clear（Job000098）——清除应用密码。
//
// 清除后 DAV 客户端只能用主密码认证（davPasswordOK 回落主密码分支）。
// 同样必须提供当前密码 —— 否则会话劫持者可以一键抹掉用户的应用密码配置，
// 让用户所有 DAV 客户端突然失效（等价于一次针对性的可用性攻击）。
func (h *Handler) AppPasswordClear(c *gin.Context) {
	var req struct {
		CurrentPassword string `json:"current_password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST",
			`请求体需为 {"current_password":"…"}`)
		return
	}
	if req.CurrentPassword == "" {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", "缺少当前密码")
		return
	}

	userID := c.GetString("user_id")
	ctx := c.Request.Context()

	if !h.verifyCurrentPassword(c, userID, req.CurrentPassword) {
		return
	}

	if err := h.Store.ClearAppPassword(ctx, userID); err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "UPDATE_FAILED", "更新失败", err)
		return
	}

	h.record(c, audit.ActionAppPassword, audit.TargetUser, userID,
		map[string]any{"operation": "clear"})
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// verifyCurrentPassword 查库比对当前密码；不对时已写好同形响应并返回 false。
//
// 抽出来的理由：Generate 与 Clear 两处校验必须**逐字同形**，否则会话劫持者
// 能用响应差异探测"哪个操作对密码错更敏感"。同 ChangePassword：对"用户不存在"
// 与"旧密码错误"给同形响应，不暴露该用户是否有口令记录。
func (h *Handler) verifyCurrentPassword(c *gin.Context, userID, current string) bool {
	hash, err := h.Store.PasswordHash(c.Request.Context(), userID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return false
	}
	if !VerifyPassword(hash, current) {
		errResp(c, http.StatusBadRequest, "WRONG_PASSWORD", "当前密码不正确")
		return false
	}
	return true
}
