package auth

import "github.com/gin-gonic/gin"

func RegisterRoutes(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/auth")

	group.POST("/login", handler.Login)
}
