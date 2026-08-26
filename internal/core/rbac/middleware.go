package rbac

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

func Permission(permission string) gin.HandlerFunc {
	return AnyPermission(permission)
}

// AnyPermission autorise la requête dès qu'une des permissions listées (ou *) est présente.
func AnyPermission(required ...string) gin.HandlerFunc {
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
			if item == "*" {
				c.Next()
				return
			}
			for _, permission := range required {
				if item == permission {
					c.Next()
					return
				}
			}
		}

		wanted := strings.Join(required, ",")
		response.Error(
			c,
			coreerrors.New(
				http.StatusForbidden,
				"PERMISSION_DENIED",
				"Permission refusée",
				map[string]string{
					"required": wanted,
				},
			),
		)
		c.Abort()
	}
}
