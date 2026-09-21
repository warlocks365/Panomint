package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"panoalbum/internal/httperr"
)

// AuthRequired JWT 鉴权中间件：校验 Bearer access token，注入 user_id/role。
// 替换 T1.1 的 AuthPlaceholder。
func AuthRequired(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			httperr.Abort(c, http.StatusUnauthorized, "UNAUTHORIZED", "缺少访问令牌")
			return
		}
		claims, err := ParseAccess(secret, strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			httperr.Abort(c, http.StatusUnauthorized, "INVALID_TOKEN", "令牌无效或已过期")
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequirePerm RBAC 权限判定（role_permissions 精确匹配 OR 用户级增量授权）。
// Job000067：用户授权是增量授予（user_permissions），角色未命中时再查用户层——
// 只增不减，写后直查立即生效（低频管理路径，不进 role 缓存）。
func RequirePerm(store *Store, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := store.HasPerm(c.Request.Context(), c.GetString("role"), perm)
		if err == nil && !ok {
			ok, err = store.HasPermUser(c.Request.Context(), c.GetString("user_id"), perm)
		}
		if err != nil || !ok {
			httperr.Abort(c, http.StatusForbidden, "FORBIDDEN", "缺少权限: "+perm)
			return
		}
		c.Next()
	}
}
