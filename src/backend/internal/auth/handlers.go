package auth

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/audit"
)

// Handler 鉴权端点（API v1.1 §2）。
type Handler struct {
	Store  *Store
	Secret []byte

	// Audit 审计写入器；可为 nil（测试/灰度时静默跳过，见 record）。
	Audit *audit.Recorder

	// Issuer 认证器里显示的服务名（otpauth 链接的 label）；空串取 DefaultMFAIssuer。
	Issuer string
}

// DefaultMFAIssuer otpauth 链接里默认的服务名。
const DefaultMFAIssuer = "全景相册 Panomint"

func (h *Handler) issuer() string {
	if h.Issuer == "" {
		return DefaultMFAIssuer
	}
	return h.Issuer
}

// record 写一条审计（**尽力而为**）。Audit 为 nil 时静默跳过。
//
// 刻意不把 detail 用于承载任何密钥类字段：审计表是永久保留的，
// 且 internal/audit.RedactDetail 只会剔除"键名命中敏感词"的项 —— 与其依赖它兜住，
// 不如从一开始就不传（下方各调用点都只传非敏感元信息）。
func (h *Handler) record(c *gin.Context, action, targetType, targetID string, detail map[string]any) {
	if h.Audit == nil {
		return
	}
	e := audit.FromGin(c)
	e.Action = action
	e.TargetType = targetType
	e.TargetID = targetID
	e.Detail = detail
	h.Audit.Record(c.Request.Context(), e)
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`

	// TOTPCode 二次验证口令。**可选**：未启用二次验证的账户照旧只传邮箱密码，
	// 老客户端因此完全不受影响（见 Login 的校验顺序说明）。
	TOTPCode string `json:"totp_code"`
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // access 秒数
	TokenType    string `json:"token_type"`
}

func errResp(c *gin.Context, code int, errCode, msg string) {
	c.JSON(code, gin.H{"error": gin.H{"code": errCode, "message": msg}})
}

// Login POST /auth/login。
func (h *Handler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	u, err := h.Store.FindByEmail(c.Request.Context(), req.Email)
	if err != nil || !VerifyPassword(u.PasswordHash, req.Password) {
		errResp(c, http.StatusUnauthorized, "BAD_CREDENTIALS", ErrBadCredentials.Error())
		return
	}
	if u.Status != "active" {
		errResp(c, http.StatusForbidden, "USER_DISABLED", ErrUserDisabled.Error())
		return
	}

	// ---- 二次验证（TOTP）----
	//
	// ⚠️ 顺序是**先验密码、再看二次验证**，不能反：
	// 若一上来就按邮箱返回 MFA_REQUIRED，攻击者无需任何凭据即可枚举出"哪些账号开了 2FA"，
	// 等于把高价值目标清单送出去。放在密码校验之后，MFA_REQUIRED 只对已持有正确密码的人可见。
	//
	// 错误码区分 MFA_REQUIRED 与 MFA_INVALID 是刻意的：前端要据此决定"弹出输入框"
	// 还是"提示口令错误"，而这两者对用户体验完全不同。
	if u.MFAEnabled {
		if req.TOTPCode == "" {
			errResp(c, http.StatusUnauthorized, "MFA_REQUIRED", "该账户已启用二次验证，请提供动态口令")
			return
		}
		_, ok, verr := VerifyTOTP(u.MFASecret, req.TOTPCode, time.Now())
		if verr != nil {
			// 密钥本身不可用（库里数据损坏）属**服务端**故障，不是用户输错了 ——
			// 报 401 会让用户反复重试一个永远不可能成功的口令，也可能被当成"防住了攻击"。
			log.Printf("auth: 用户 %s 的 TOTP 密钥不可用: %v", u.ID, verr)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "二次验证配置异常，请联系管理员")
			return
		}
		if !ok {
			errResp(c, http.StatusUnauthorized, "MFA_INVALID", "动态口令不正确或已过期")
			return
		}
	}

	access, err := IssueAccess(h.Secret, u.ID, u.Role)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	refresh, err := NewRefreshToken()
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	if err := h.Store.CreateSession(c.Request.Context(), u.ID, refresh, c.ClientIP(), c.GetHeader("User-Agent")); err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "会话创建失败")
		return
	}
	// 登录成功写审计（**失败登录刻意不写**：那是日志与限流的职责，
	// 且写失败登录会让"任何人可写审计表"成为一个滥用面 —— 见 internal/audit 的既有决定）。
	//
	// ⚠️ 键名不能叫 "mfa"：audit.RedactDetail 按**键名子串**脱敏，而 "mfa" 在敏感词表里，
	// 于是 detail 会被整键剔除、只剩 `{}`（实测确认过）。这类"合法元数据被脱敏吃掉"的问题
	// 不会报错、只会静默丢字段，所以审计 detail 的键名要避开敏感子串。
	h.record(c, audit.ActionLogin, audit.TargetUser, u.ID, map[string]any{"second_factor": u.MFAEnabled})
	c.JSON(http.StatusOK, tokenPair{access, refresh, int64(AccessTTL.Seconds()), "Bearer"})
}

// MFASetup POST /auth/mfa/setup（需鉴权）→ {secret, otpauth_url, digits, period}
//
// 只**生成待确认**的密钥（mfa_enabled 保持 false），需再调 /auth/mfa/confirm 才算生效。
// 两步设计的目的：避免"密钥写进去了但用户没来得及加进认证器"就把自己锁在门外。
//
// ⚠️ 响应体里的 secret 是**一次性明文下发**（用户必须把它加进认证器）。
// 服务端只存它本身（TOTP 需要在登录时用它算口令，无法只存哈希）；
// 因此它绝不能进日志、不能进审计 detail —— 本函数不记录它。
func (h *Handler) MFASetup(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	u, err := h.Store.FindByID(ctx, userID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	if u.MFAEnabled {
		// 已启用时不允许直接重绑：那会给会话劫持者一条替换认证器的捷径。
		errResp(c, http.StatusConflict, "MFA_ALREADY_ENABLED", ErrMFAAlreadyEnabled.Error())
		return
	}

	secret, err := GenerateTOTPSecret()
	if err != nil {
		log.Printf("auth: 生成 TOTP 密钥失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "生成密钥失败")
		return
	}
	if err := h.Store.SetPendingMFASecret(ctx, userID, secret); err != nil {
		if errors.Is(err, ErrMFAAlreadyEnabled) {
			errResp(c, http.StatusConflict, "MFA_ALREADY_ENABLED", err.Error())
			return
		}
		log.Printf("auth: 写入待确认 TOTP 密钥失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "保存密钥失败")
		return
	}

	h.record(c, audit.ActionMFASetup, audit.TargetUser, userID, nil)
	c.JSON(http.StatusOK, gin.H{
		"secret":      secret,
		"otpauth_url": OTPAuthURL(h.issuer(), u.Email, secret, TOTPDigits),
		"digits":      TOTPDigits,
		"period":      int(TOTPPeriod.Seconds()),
	})
}

// MFAConfirm POST /auth/mfa/confirm（需鉴权）← {code} → {mfa_enabled: true}
//
// 用一个**当场算出的口令**证明"认证器确实已经配对成功"，才把开关打开。
func (h *Handler) MFAConfirm(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误（需提供 code）")
		return
	}

	u, err := h.Store.FindByID(ctx, userID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	if u.MFAEnabled {
		errResp(c, http.StatusConflict, "MFA_ALREADY_ENABLED", ErrMFAAlreadyEnabled.Error())
		return
	}
	if u.MFASecret == "" {
		errResp(c, http.StatusBadRequest, "MFA_NOT_PENDING", ErrMFANotPending.Error())
		return
	}
	if !h.verifyOrFail(c, u.MFASecret, req.Code) {
		return
	}
	if err := h.Store.EnableMFA(ctx, userID); err != nil {
		if errors.Is(err, ErrMFANotPending) {
			errResp(c, http.StatusBadRequest, "MFA_NOT_PENDING", err.Error())
			return
		}
		log.Printf("auth: 启用二次验证失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "启用失败")
		return
	}

	h.record(c, audit.ActionMFAEnable, audit.TargetUser, userID, nil)
	c.JSON(http.StatusOK, gin.H{"mfa_enabled": true})
}

// MFADisable POST /auth/mfa/disable（需鉴权）← {code} → {mfa_enabled: false}
//
// ⚠️ 关闭也必须提交一个**有效口令**，不接受"仅凭会话即可关闭"。
// 理由：只凭会话就能关 2FA 的话，会话劫持者可以一键解除这道防线，
// 那二次验证对他而言形同不存在 —— 它恰恰是为了防"密码/会话已泄漏"的场景而存在的。
func (h *Handler) MFADisable(c *gin.Context) {
	ctx := c.Request.Context()
	userID := c.GetString("user_id")

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误（需提供 code）")
		return
	}

	u, err := h.Store.FindByID(ctx, userID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	if !u.MFAEnabled {
		errResp(c, http.StatusConflict, "MFA_NOT_ENABLED", ErrMFANotEnabled.Error())
		return
	}
	if !h.verifyOrFail(c, u.MFASecret, req.Code) {
		return
	}
	if err := h.Store.DisableMFA(ctx, userID); err != nil {
		if errors.Is(err, ErrMFANotEnabled) {
			errResp(c, http.StatusConflict, "MFA_NOT_ENABLED", err.Error())
			return
		}
		log.Printf("auth: 关闭二次验证失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "关闭失败")
		return
	}

	h.record(c, audit.ActionMFADisable, audit.TargetUser, userID, nil)
	c.JSON(http.StatusOK, gin.H{"mfa_enabled": false})
}

// verifyOrFail 校验口令并按需写响应；通过返回 true。
//
// 抽出来是为了让 confirm 与 disable 共用同一套错误语义：
// 密钥损坏报 500（服务端问题）、口令不对报 401（用户问题）。
// 两处若各写一遍，很容易一边写成 401 一边写成 500，排障时就会互相矛盾。
func (h *Handler) verifyOrFail(c *gin.Context, secret, code string) bool {
	_, ok, err := VerifyTOTP(secret, code, time.Now())
	if err != nil {
		log.Printf("auth: TOTP 校验失败（密钥不可用）: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "二次验证配置异常，请联系管理员")
		return false
	}
	if !ok {
		errResp(c, http.StatusUnauthorized, "MFA_INVALID", "动态口令不正确或已过期")
		return false
	}
	return true
}

// Refresh POST /auth/refresh（轮换 + 重放检测）。
func (h *Handler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	ctx := c.Request.Context()
	newRefresh, err := NewRefreshToken()
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	userID, err := h.Store.RotateSession(ctx, req.RefreshToken, newRefresh, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		errResp(c, http.StatusUnauthorized, "INVALID_REFRESH", err.Error())
		return
	}
	u, err := h.Store.FindByID(ctx, userID)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	// ⚠️ 必须**在 refresh 路径上查状态**：access token 一旦签出就是自证的（服务端不查库），
	// 而 refresh 能不断换出新的 access token。不查状态的话，"禁用某用户"要等 refresh token
	// 自然过期（默认 7 天）才真正生效 —— 期间他照常刷令牌。
	if u.Status != StatusActive {
		_ = h.Store.RevokeAllSessions(ctx, userID)
		errResp(c, http.StatusForbidden, "USER_DISABLED", ErrUserDisabled.Error())
		return
	}
	access, err := IssueAccess(h.Secret, u.ID, u.Role)
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "令牌签发失败")
		return
	}
	c.JSON(http.StatusOK, tokenPair{access, newRefresh, int64(AccessTTL.Seconds()), "Bearer"})
}

// Logout POST /auth/logout（吊销会话）。
func (h *Handler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	_ = h.Store.RevokeSession(c.Request.Context(), req.RefreshToken)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

// Me GET /auth/me（需鉴权）。
func (h *Handler) Me(c *gin.Context) {
	u, err := h.Store.FindByID(c.Request.Context(), c.GetString("user_id"))
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"id": u.ID, "email": u.Email, "display_name": u.DisplayName, "role": u.Role, "status": u.Status,
		// 契约 §2 的 /auth/me 本就承诺返回 mfa_enabled（此前一直没给，属既有缺口，本次补上）。
		// mfa_pending 是附加信息：设置二次验证分 setup→confirm 两步，界面需要知道"配到一半"。
		"mfa_enabled": u.MFAEnabled,
		"mfa_pending": u.MFAPending,
	})
}

// CreateUser POST /admin/users（需 admin:users 权限）。
func (h *Handler) CreateUser(c *gin.Context) {
	var req struct {
		Email       string `json:"email" binding:"required,email"`
		DisplayName string `json:"display_name"`
		Password    string `json:"password" binding:"required,min=8"`
		Role        string `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误（密码至少 8 位）")
		return
	}
	id, err := h.Store.CreateUser(c.Request.Context(), req.Email, req.DisplayName, req.Password, req.Role)
	if err != nil {
		errResp(c, http.StatusConflict, "CREATE_FAILED", "创建失败（邮箱可能已存在）")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// ListUsers GET /admin/users（需 admin:users 权限）。
func (h *Handler) ListUsers(c *gin.Context) {
	users, err := h.Store.ListUsers(c.Request.Context())
	if err != nil {
		errResp(c, http.StatusInternalServerError, "INTERNAL", "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "total": len(users)})
}

// ListRoles GET /admin/roles（需 admin:users 权限）。
// 返回角色及其权限与该角色下的用户数（管理界面据此决定能否改动某个角色）。
func (h *Handler) ListRoles(c *gin.Context) {
	roles, err := h.Store.ListRoles(c.Request.Context())
	if err != nil {
		log.Printf("auth: 查询角色列表失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "查询失败")
		return
	}
	c.JSON(http.StatusOK, gin.H{"roles": roles, "total": len(roles)})
}

// guardUserChange 施加"自锁 / 最后 owner"两条守卫；被拒时已写好响应并返回 false。
//
// 为什么抽成一个函数：PATCH 与 DELETE 都需要它，而这两条守卫是"改错了就再也进不去"的逻辑，
// 两处各写一遍迟早会漂移（一处拦、一处漏）。判定本身在 auth.CheckUserPatch（纯函数，可穷举单测）。
func (h *Handler) guardUserChange(c *gin.Context, target *User, in UserUpdate) bool {
	if !in.ChangesRoleOrStatus() {
		return true // 不碰角色/状态 → 两条守卫都不适用
	}
	others := -1
	if target.Role == "owner" && target.Status == StatusActive {
		n, err := h.Store.CountActiveOwnersExcept(c.Request.Context(), target.ID)
		if err != nil {
			log.Printf("auth: 统计可用 owner 失败: %v", err)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "校验失败")
			return false
		}
		others = n
	}
	if err := CheckUserPatch(c.GetString("user_id"), target, in, others); err != nil {
		switch {
		case errors.Is(err, ErrSelfLockout):
			errResp(c, http.StatusConflict, "SELF_LOCKOUT", err.Error())
		case errors.Is(err, ErrLastOwner):
			errResp(c, http.StatusConflict, "LAST_OWNER", err.Error())
		default:
			log.Printf("auth: 用户改动校验失败: %v", err)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "校验失败")
		}
		return false
	}
	return true
}

// UpdateUser PATCH /admin/users/:id（需 admin:users 权限）← {display_name?,role?,status?,password?}
//
// 部分更新：只传要改的字段。契约 §2 写的"改角色/状态/重置密码"即此。
func (h *Handler) UpdateUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	var in UserUpdate
	if err := c.ShouldBindJSON(&in); err != nil {
		errResp(c, http.StatusBadRequest, "BAD_REQUEST", "请求格式错误")
		return
	}
	norm, err := NormalizeUserUpdate(in)
	if err != nil {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	}
	if norm.Empty() {
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", "没有任何待更新字段")
		return
	}

	target, err := h.Store.GetUser(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		return
	}
	if err != nil {
		log.Printf("auth: 查询用户失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}
	if !h.guardUserChange(c, target, norm) {
		return
	}

	u, err := h.Store.UpdateUser(ctx, id, norm)
	switch {
	case errors.Is(err, ErrRoleNotFound):
		errResp(c, http.StatusBadRequest, "ROLE_NOT_FOUND", ErrRoleNotFound.Error())
		return
	case errors.Is(err, ErrInvalidInput):
		errResp(c, http.StatusBadRequest, "INVALID_INPUT", err.Error())
		return
	case errors.Is(err, ErrUserNotFound):
		errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		return
	case err != nil:
		log.Printf("auth: 更新用户失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "更新失败")
		return
	}

	// 禁用必须连带吊销会话：见 RevokeAllSessions 的说明（否则最长 7 天不生效）。
	// 吊销失败**不阻断**响应：状态已经改了，下次 refresh 会因 status 被拒；这里只记日志。
	if norm.Status != nil && *norm.Status == StatusDisabled {
		if err := h.Store.RevokeAllSessions(ctx, id); err != nil {
			log.Printf("auth: 禁用用户后吊销会话失败（user=%s）: %v", id, err)
		}
	}

	// 审计：改角色属更重的动作，单列 ActionRoleChange，便于按动作过滤时一眼看到权限变更。
	action := audit.ActionUserUpdate
	detail := map[string]any{}
	if norm.Role != nil {
		action = audit.ActionRoleChange
		detail["role"] = *norm.Role
	}
	if norm.Status != nil {
		detail["status"] = *norm.Status
	}
	if norm.DisplayName != nil {
		detail["display_name_changed"] = true
	}
	if norm.Password != nil {
		// 键名避开敏感子串（含 password/pwd/credential/secret 的键会被 RedactDetail 整键剔除）
		detail["auth_material_reset"] = true
	}
	h.record(c, action, audit.TargetUser, id, detail)

	c.JSON(http.StatusOK, gin.H{"user": u})
}

// DeleteUser DELETE /admin/users/:id（需 admin:users 权限）。
//
// 契约 §2 写的是「禁用/删除」——正好对应现实约束：
//   - 默认（不带参数）→ **硬删除**，但只要该用户还有归属数据就会被外键拒绝（返回 409
//     USER_HAS_ASSETS，并提示改用禁用）。media/albums/audit_log 等 7 处以 NO ACTION 引用
//     users(id)，这是刻意的：数据必须继续可归属，审计尤其不能因删用户而失去主体。
//   - `?disable=1` → **禁用**（status=disabled + 吊销会话），适用于绝大多数真实场景。
//
// 两种路径都受"自锁 / 最后 owner"守卫约束（禁用等价于 status=disabled）。
func (h *Handler) DeleteUser(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")

	target, err := h.Store.GetUser(ctx, id)
	if errors.Is(err, ErrUserNotFound) {
		errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		return
	}
	if err != nil {
		log.Printf("auth: 查询用户失败: %v", err)
		errResp(c, http.StatusInternalServerError, "INTERNAL", "用户查询失败")
		return
	}

	disabled := StatusDisabled
	if !h.guardUserChange(c, target, UserUpdate{Status: &disabled}) {
		return
	}

	soft := c.Query("disable") == "1" || c.Query("disable") == "true"
	if soft {
		if _, err := h.Store.UpdateUser(ctx, id, UserUpdate{Status: &disabled}); err != nil {
			log.Printf("auth: 禁用用户失败: %v", err)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "禁用失败")
			return
		}
		if err := h.Store.RevokeAllSessions(ctx, id); err != nil {
			log.Printf("auth: 禁用用户后吊销会话失败（user=%s）: %v", id, err)
		}
		h.record(c, audit.ActionUserDelete, audit.TargetUser, id, map[string]any{"mode": "disable"})
		c.JSON(http.StatusOK, gin.H{"disabled": id, "mode": "disable"})
		return
	}

	if err := h.Store.DeleteUser(ctx, id); err != nil {
		switch {
		case errors.Is(err, ErrUserHasAssets):
			errResp(c, http.StatusConflict, "USER_HAS_ASSETS",
				"该用户仍拥有媒体/相册/分享/审计等数据，无法删除；请改用禁用（?disable=1 或 PATCH status=disabled）")
		case errors.Is(err, ErrUserNotFound):
			errResp(c, http.StatusNotFound, "USER_NOT_FOUND", ErrUserNotFound.Error())
		default:
			log.Printf("auth: 删除用户失败: %v", err)
			errResp(c, http.StatusInternalServerError, "INTERNAL", "删除失败")
		}
		return
	}
	h.record(c, audit.ActionUserDelete, audit.TargetUser, id, map[string]any{"mode": "purge"})
	c.JSON(http.StatusOK, gin.H{"deleted": id, "mode": "purge"})
}
