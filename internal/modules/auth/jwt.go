package auth

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID       uint     `json:"userId"`
	Email        string   `json:"email"`
	Role         string   `json:"role"`
	Permissions  []string `json:"permissions"`
	Functions    []string `json:"functions,omitempty"`
	Specialties  []string `json:"specialties,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	jwt.RegisteredClaims
}

func GenerateToken(secret string, user User, permissions, functions, specialties, capabilities []string) (string, error) {
	claims := Claims{
		UserID:       user.ID,
		Email:        user.Email,
		Role:         user.Role,
		Permissions:  permissions,
		Functions:    functions,
		Specialties:  specialties,
		Capabilities: capabilities,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString([]byte(secret))
}

func ParseToken(secret string, tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
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
