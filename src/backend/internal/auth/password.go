package auth

import (
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

	hash, err := h.Store.PasswordHash(ctx, userID)
	if err != nil {
		httperr.Fail(c, http.StatusInternalServerError, "QUERY_FAILED", "查询失败", err)
		return
	}
	if !VerifyPassword(hash, req.CurrentPassword) {
		// 对"用户不存在"与"旧密码错误"给同形响应：不向外暴露该用户是否有口令记录。
		errResp(c, http.StatusBadRequest, "WRONG_PASSWORD", "当前密码不正确")
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
