package rbac

import (
	"errors"

	"github.com/gin-gonic/gin"
)

const (
	ContextUserID      = "user_id"
	ContextRole        = "role"
	ContextPermissions = "permissions"
)

var ErrCurrentUserUnavailable = errors.New("utilisateur authentifié introuvable dans le contexte")

func SetUser(c *gin.Context, userID uint, role string, permissions []string) {
	c.Set(ContextUserID, userID)
	c.Set(ContextRole, role)
	c.Set(ContextPermissions, permissions)
}

// CurrentUserID is the single trusted accessor for the authenticated author.
// It deliberately rejects missing, mistyped and zero-valued context entries.
func CurrentUserID(c *gin.Context) (uint, error) {
	value, exists := c.Get(ContextUserID)
	if !exists {
		return 0, ErrCurrentUserUnavailable
	}
	userID, ok := value.(uint)
	if !ok || userID == 0 {
		return 0, ErrCurrentUserUnavailable
	}
	return userID, nil
}
