// Package auth 鉴权与 RBAC（独立后台，不对接 DSM）。
// 依据：API v1.1 §1.1/§2；TDD §3.5；DDL users/sessions/roles/role_permissions。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// AccessTTL 访问令牌有效期。
	//
	// Job000141 由 15min 收紧到 5min：access token 是自证的纯 JWT，服务端
	// **不查库**（ParseAccess 只验签与过期），所以「禁用用户/改密」这类操作
	// 无法让已签发的 token 立即失效——原先要等最长 15 分钟。收紧到 5 分钟把
	// 暴露窗口压到 1/3，配合前端已有的自动续期（src/utils/tokenStore.js +
	// api/http.js 的 refresh 链）对用户无感。
	//
	// 想彻底解决需引入 session_ver（写进 claims 并在鉴权时比对），代价是
	// 每请求多一次校验；当前自托管单实例的负载下，5min TTL 是性价比最高的取舍。
	AccessTTL  = 5 * time.Minute
	RefreshTTL = 7 * 24 * time.Hour
)

// Claims 访问令牌声明。
type Claims struct {
	UserID string `json:"sub"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// JWTSecret 从环境变量读取签名密钥（TDD §8.6：JWT_SECRET，轮换周期 90 天）。
func JWTSecret() ([]byte, error) {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		if os.Getenv("APP_ENV") == "prod" {
			return nil, errors.New("JWT_SECRET 未配置（生产环境必须设置）")
		}
		s = "dev-only-insecure-secret-do-not-use-in-prod"
	}
	return []byte(s), nil
}

// IssueAccess 签发访问令牌。
func IssueAccess(secret []byte, userID, role string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "panoalbum",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
}

// ParseAccess 校验访问令牌，返回声明。
func ParseAccess(secret []byte, tokenStr string) (*Claims, error) {
	tok, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非预期签名算法: %v", t.Header["alg"])
		}
		return secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || !tok.Valid {
		return nil, errors.New("令牌无效")
	}
	return claims, nil
}

// NewRefreshToken 生成不透明刷新令牌（64 字符 hex）。
func NewRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
