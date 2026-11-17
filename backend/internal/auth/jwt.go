// internal/auth/jwt.go
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var cachedSecret []byte

// 提供密钥，加密算法（如HS256）使用密钥对header+payload进行加密后生成signature
// 密钥只有服务端可以获取
func jwtSecret() []byte {
	if cachedSecret != nil {
		return cachedSecret
	}
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			log.Printf("FATAL: cannot generate JWT secret: %v", err)
			cachedSecret = []byte("fallback-unsafe-key-change-me")
			return cachedSecret
		}
		secret = hex.EncodeToString(b)
		log.Printf("WARNING: JWT_SECRET not set, generated random key. All tokens invalid on restart.")
	}
	cachedSecret = []byte(secret)
	return cachedSecret
}

// Claims就是token（header+payload+signature）中的payload
type Claims struct {
	AccountID uint   `json:"account_id"`
	Username  string `json:"username"`
	jwt.RegisteredClaims
}

func GenerateToken(accountID uint, username string) (string, error) {
	now := time.Now()

	claims := Claims{
		AccountID: accountID,
		Username:  username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
		},
	}
	//这里还会自动生成header
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(jwtSecret())
}

// token是进入凭证，refreshtoken就是获取token的凭证
func GenerateRefreshToken(accountID uint) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
				return nil, errors.New("unexpected signing method")
			}
			return jwtSecret(), nil
		},
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}
