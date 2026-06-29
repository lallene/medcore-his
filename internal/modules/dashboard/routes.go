package dashboard

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/dashboard")

	group.GET("", rbac.Permission("dashboard.read"), handler.Show)
}
