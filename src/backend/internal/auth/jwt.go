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
	AccessTTL  = 15 * time.Minute
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
