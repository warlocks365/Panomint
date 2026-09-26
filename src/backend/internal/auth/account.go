package auth

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
)

// ---------------------------------------------------------------------------
// Job000128：自助注册 / 账号策略配置 / 邀请码管理
//
// 三个端点共用一条 fail-closed 纪律：**读不到策略时按最严格处理**——
// 注册路径回 DefaultPolicy（注册必然被拒），管理端读取路径直接 500
// （管理员需要看到真实故障，静默回退会让他们以为配置生效了）。
// ---------------------------------------------------------------------------

// policyJSON 策略的统一 JSON 形态（GET/PUT 响应共用，避免两处键名漂移）。
func policyJSON(p AccountPolicy) gin.H {
	return gin.H{
		"allow_registration": p.AllowRegistration,
		"invite_required":    p.InviteRequired,
		"pwd_min_length":     p.PwdMinLength,
		"pwd_require_upper":  p.PwdRequireUpper,
		"pwd_require_lower":  p.PwdRequireLower,
		"pwd_require_digit":  p.PwdRequireDigit,
		"pwd_require_special": p.PwdRequireSpecial,
		"login_max_attempts": p.LoginMaxAttempts,
		"login_lock_minutes": p.LoginLockMinutes,
	}
}

// accountPolicyReq PUT /admin/account-config 的请求体（与 policyJSON 键一致）。
type accountPolicyReq struct {
	AllowRegistration bool `json:"allow_registration"`
	InviteRequired    bool `json:"invite_required"`
	PwdMinLength      int  `json:"pwd_min_length"`
	PwdRequireUpper   bool `json:"pwd_require_upper"`
	PwdRequireLower   bool `json:"pwd_require_lower"`
	PwdRequireDigit   bool `json:"pwd_require_digit"`
	PwdRequireSpecial bool `json:"pwd_require_special"`
	LoginMaxAttempts  int  `json:"login_max_attempts"`
	LoginLockMinutes  int  `json:"login_lock_minutes"`
}

// normalizeAccountPolicy 范围校验（与迁移 00043 的 DDL CHECK 一一对应），
// 应用层先给出干净的 400，而不是等 PG 报 23514 再翻成含糊的 500。
func normalizeAccountPolicy(in accountPolicyReq) (AccountPolicy, error) {
	p := AccountPolicy{
		AllowRegistration: in.AllowRegistration,
		InviteRequired:    in.InviteRequired,
		PwdMinLength:      in.PwdMinLength,
		PwdRequireUpper:   in.PwdRequireUpper,
		PwdRequireLower:   in.PwdRequireLower,
		PwdRequireDigit:   in.PwdRequireDigit,
		PwdRequireSpecial: in.PwdRequireSpecial,
		LoginMaxAttempts:  in.LoginMaxAttempts,
		LoginLockMinutes:  in.LoginLockMinutes,
	}
	if p.PwdMinLength < 8 || p.PwdMinLength > 64 {
		return p, errors.New("密码最小长度须在 8–64 之间")
	}
	if p.LoginMaxAttempts < 1 || p.LoginMaxAttempts > 50 {
		return p, errors.New("登录尝试次数上限须在 1–50 之间")
	}
	if p.LoginLockMinutes < 1 || p.LoginLockMinutes > 1440 {
		return p, errors.New("锁定时长须在 1–1440 分钟之间")
	}
	return p, nil
}

// GetAccountConfig GET /admin/account-config（需 admin:users）。
func (h *Handler) GetAccountConfig(c *gin.Context) {
	p, err := h.Store.LoadAccountPolicy(c.Request.Context())
	if err != nil {
		log.Printf("auth: 读取账号策略失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "读取配置失败（策略行不存在？）")
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": policyJSON(p)})
}

// PutAccountConfig PUT /admin/account-config（需 admin:users）← accountPolicyReq。
//
// 全量覆盖式 PUT（不带增量语义）：管理界面的保存按钮每次提交整份表单，
// 增量 patch 反而会引入"漏传字段被当成 false/0"的陷阱。
func (h *Handler) PutAccountConfig(c *gin.Context) {
	var in accountPolicyReq
	if err := c.ShouldBindJSON(&in); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	p, err := normalizeAccountPolicy(in)
	if err != nil {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}
	if err := h.Store.SaveAccountPolicy(c.Request.Context(), p); err != nil {
		log.Printf("auth: 保存账号策略失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "保存失败")
		return
	}
	h.record(c, audit.ActionSettingsPatch, audit.TargetSetting, "account_policy",
		map[string]any{
			"allow_registration": p.AllowRegistration,
			"invite_required":    p.InviteRequired,
			"pwd_min_length":     p.PwdMinLength,
			"login_max_attempts": p.LoginMaxAttempts,
			"login_lock_minutes": p.LoginLockMinutes,
		})
	c.JSON(http.StatusOK, gin.H{"policy": policyJSON(p)})
}

// ---------------------------------------------------------------------------
// 自助注册（POST /auth/register，公开端点）
// ---------------------------------------------------------------------------

// RegisterStatus GET /auth/register/status（公开）：注册页据此决定是否渲染注册入口、
// 是否摆出邀请码输入框。只暴露这两个布尔位——不含密码策略细节（那是给已登录管理员的）。
// 读库失败 fail-closed：按"注册关闭"返回，注册页自动隐藏入口（与 POST 同口径）。
func (h *Handler) RegisterStatus(c *gin.Context) {
	p, err := h.Store.LoadAccountPolicy(c.Request.Context())
	if err != nil {
		log.Printf("auth: 读取账号策略失败，注册状态按关闭返回: %v", err)
		c.JSON(http.StatusOK, gin.H{"allow_registration": false, "invite_required": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"allow_registration": p.AllowRegistration,
		"invite_required":    p.InviteRequired,
	})
}

// Register POST /auth/register（公开，Job000128 点 6）。
//
// 校验顺序：注册开关 → 邀请码 → 密码策略 → 建号（+事务内消耗邀请码）。
// 注册开关默认关闭（迁移 00043 DDL 默认 false）：自托管相册默认是私有的，
// "装完就向公网开放注册"是错误默认值，要开必须管理员显式打开。
//
// 枚举面：409 EMAIL_EXISTS 会暴露"邮箱已注册"。这是注册流程的必需反馈
// （不告诉用户重了，他会以为注册成功然后永远收不到账号），行业惯例均如此；
// 该端点整体受全局限流保护。
func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Email       string `json:"email" binding:"required,email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password" binding:"required"`
		InviteCode  string `json:"invite_code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误（需合法邮箱与密码）")
		return
	}

	ctx := c.Request.Context()
	p, err := h.Store.LoadAccountPolicy(ctx)
	if err != nil {
		log.Printf("auth: 读取账号策略失败，注册按关闭处理（fail-closed）: %v", err)
		p = DefaultPolicy() // AllowRegistration=false → 下方必拒
	}
	if !p.AllowRegistration {
		errResp(c, http.StatusForbidden, "REGISTRATION_DISABLED", "当前未开放新用户注册")
		return
	}
	if p.InviteRequired {
		if strings.TrimSpace(req.InviteCode) == "" {
			errResp(c, http.StatusBadRequest, "INVITE_REQUIRED", "注册需要邀请码")
			return
		}
		// 预检给细分文案（不存在/已用/过期）；最终一致性由 RegisterUser
		// 事务内的条件 UPDATE 兜底（并发抢码时返回 ErrInviteUsed）。
		if err := h.Store.CheckInviteUsable(ctx, strings.TrimSpace(req.InviteCode)); err != nil {
			switch {
			case errors.Is(err, ErrInviteInvalid):
				errResp(c, http.StatusBadRequest, "INVITE_INVALID", err.Error())
			case errors.Is(err, ErrInviteUsed):
				errResp(c, http.StatusConflict, "INVITE_USED", err.Error())
			case errors.Is(err, ErrInviteExpired):
				errResp(c, http.StatusBadRequest, "INVITE_EXPIRED", err.Error())
			default:
				log.Printf("auth: 邀请码预检失败: %v", err)
				errResp(c, http.StatusInternalServerError, "INTERNAL", "校验邀请码失败")
			}
			return
		}
	}
	if err := ValidatePasswordPolicy(req.Password, p); err != nil {
		errResp(c, http.StatusBadRequest, "WEAK_PASSWORD", err.Error())
		return
	}

	id, err := h.Store.RegisterUser(ctx, RegisterInput{
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Password:    req.Password,
		InviteCode:  strings.TrimSpace(req.InviteCode),
	})
	switch {
	case errors.Is(err, ErrEmailExists):
		errResp(c, http.StatusConflict, "EMAIL_EXISTS", ErrEmailExists.Error())
		return
	case errors.Is(err, ErrInviteUsed):
		errResp(c, http.StatusConflict, "INVITE_USED", ErrInviteUsed.Error())
		return
	case errors.Is(err, ErrRoleNotFound):
		// 默认角色缺失是部署环境问题（有人删了内置 member），不是用户能解决的
		log.Printf("auth: 注册失败——默认角色 %s 不存在: %v", DefaultRegisterRole, err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "注册失败")
		return
	case err != nil:
		log.Printf("auth: 注册失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "注册失败")
		return
	}

	// 公开端点：recordAs 显式传 actor（新账号 id），否则审计主体落 NULL。
	h.recordAs(c, id, audit.ActionRegister, audit.TargetUser, id,
		map[string]any{"invite_used": p.InviteRequired})
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// ---------------------------------------------------------------------------
// 邀请码管理（/admin/invites，需 admin:users）
// ---------------------------------------------------------------------------

// inviteMaxHours 邀请码有效期上限（30 天）：门卡不该是永久的——
// 长期有效的码一旦外泄，等于给陌生人留了一扇不会关的门。
const inviteMaxHours = 24 * 30

// ListInvites GET /admin/invites。
func (h *Handler) ListInvites(c *gin.Context) {
	invites, err := h.Store.ListInviteCodes(c.Request.Context())
	if err != nil {
		log.Printf("auth: 查询邀请码列表失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"invites": invites, "total": len(invites)})
}

// CreateInvite POST /admin/invites ← {expires_in_hours?}（默认 72，上限 720）。
//
// 邀请码明文在**响应里只出现一次**吗？不——与密码不同，邀请码本来就入库明文、
// 管理员可随时在列表复制重发（policy.go GenerateInviteCode 的设计说明）。
// 它是"一次性门卡"而不是"长期凭据"：用过即废、30 天必过期，泄漏面可控。
func (h *Handler) CreateInvite(c *gin.Context) {
	var req struct {
		ExpiresInHours int `json:"expires_in_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", `请求体需为 {"expires_in_hours":72}（可省略）`)
		return
	}
	if req.ExpiresInHours <= 0 {
		req.ExpiresInHours = 72
	}
	if req.ExpiresInHours > inviteMaxHours {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT",
			"有效期不能超过 720 小时（30 天）——长期有效的邀请码等于不设防")
		return
	}

	code, err := GenerateInviteCode()
	if err != nil {
		log.Printf("auth: 生成邀请码失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "生成失败")
		return
	}
	expiresAt := time.Now().Add(time.Duration(req.ExpiresInHours) * time.Hour)
	ic, err := h.Store.CreateInviteCode(c.Request.Context(), code, c.GetString("user_id"), expiresAt)
	if err != nil {
		log.Printf("auth: 保存邀请码失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "保存失败")
		return
	}

	h.record(c, audit.ActionInviteCreate, audit.TargetUser, ic.ID,
		map[string]any{"expires_in_hours": req.ExpiresInHours})
	c.JSON(http.StatusCreated, gin.H{"invite": ic})
}
