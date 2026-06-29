package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

func Middleware(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")

		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, coreerrors.Unauthorized("Token manquant"))
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")

		claims, err := ParseToken(secret, tokenString)

		if err != nil {
			response.Error(c, coreerrors.Unauthorized("Token invalide"))
			c.Abort()
			return
		}

		rbac.SetUser(c, claims.UserID, claims.Role, claims.Permissions)

		c.Next()
	}
}
