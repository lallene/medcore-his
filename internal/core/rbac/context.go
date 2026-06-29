package rbac

import "github.com/gin-gonic/gin"

const (
	ContextUserID      = "user_id"
	ContextRole        = "role"
	ContextPermissions = "permissions"
)

func SetUser(c *gin.Context, userID uint, role string, permissions []string) {
	c.Set(ContextUserID, userID)
	c.Set(ContextRole, role)
	c.Set(ContextPermissions, permissions)
}
