package auth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthRequired JWT 鉴权中间件：校验 Bearer access token，注入 user_id/role。
// 替换 T1.1 的 AuthPlaceholder。
func AuthRequired(secret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "UNAUTHORIZED", "message": "缺少访问令牌"},
			})
			return
		}
		claims, err := ParseAccess(secret, strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "INVALID_TOKEN", "message": "令牌无效或已过期"},
			})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequirePerm RBAC 权限判定（role_permissions 精确匹配）。
func RequirePerm(store *Store, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		ok, err := store.HasPerm(c.Request.Context(), c.GetString("role"), perm)
		if err != nil || !ok {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error": gin.H{"code": "FORBIDDEN", "message": "缺少权限: " + perm},
			})
			return
		}
		c.Next()
	}
}
