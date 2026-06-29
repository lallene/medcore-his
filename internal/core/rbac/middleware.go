package rbac

import (
	"net/http"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

func Permission(permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		rawPermissions, exists := c.Get(ContextPermissions)

		if !exists {
			response.Error(
				c,
				coreerrors.Unauthorized("Utilisateur non authentifié"),
			)
			c.Abort()
			return
		}

		permissions, ok := rawPermissions.([]string)

		if !ok {
			response.Error(
				c,
				coreerrors.Forbidden("Permissions invalides"),
			)
			c.Abort()
			return
		}

		for _, item := range permissions {
			if item == permission || item == "*" {
				c.Next()
				return
			}
		}

		response.Error(
			c,
			coreerrors.New(
				http.StatusForbidden,
				"PERMISSION_DENIED",
				"Permission refusée",
				map[string]string{
					"required": permission,
				},
			),
		)
		c.Abort()
	}
}
