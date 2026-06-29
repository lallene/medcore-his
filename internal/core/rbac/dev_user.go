package rbac

import "github.com/gin-gonic/gin"

func DevUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		SetUser(c, 1, "admin", []string{
			"*",
			"patients:read",
			"patients:create",
			"patients:update",
			"patients:delete",
		})

		c.Next()
	}
}
