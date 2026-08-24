package auth

import (
	"strings"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
	"gorm.io/gorm"
)

func Middleware(secret string, databases ...*gorm.DB) gin.HandlerFunc {
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
		if len(databases) > 0 && databases[0] != nil {
			var active bool
			if e := databases[0].Table("users").Select("is_active").Where("id=?", claims.UserID).Scan(&active).Error; e != nil || !active {
				response.Error(c, coreerrors.Unauthorized("Compte utilisateur inactif"))
				c.Abort()
				return
			}
			functions, specialties, capabilities, e := staffIdentity(databases[0], claims.UserID)
			if e != nil {
				response.Error(c, coreerrors.Unauthorized("Profil personnel indisponible"))
				c.Abort()
				return
			}
			claims.Functions, claims.Specialties, claims.Capabilities = functions, specialties, capabilities
			claims.Permissions = rbac.EffectiveStaffPermissions(claims.Role, functions, specialties)
		}

		rbac.SetUser(c, claims.UserID, claims.Role, claims.Permissions)

		c.Next()
	}
}
