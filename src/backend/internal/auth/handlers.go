package auth

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler 鉴权端点（API v1.1 §2）。
type Handler struct {
	Store  *Store
	Secret []byte
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
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
	c.JSON(http.StatusOK, tokenPair{access, refresh, int64(AccessTTL.Seconds()), "Bearer"})
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
