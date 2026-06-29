package patients

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
)

func RegisterRoutes(router *gin.RouterGroup, db *gorm.DB) {
	repo := NewRepository(db)
	service := NewService(repo)
	handler := NewHandler(service)

	RegisterRoutesWithHandler(router, handler)
}

func RegisterRoutesWithHandler(router *gin.RouterGroup, handler *Handler) {
	group := router.Group("/patients")

	group.GET("", rbac.Permission("patients:read"), handler.List)
	group.GET("/:id", rbac.Permission("patients:read"), handler.FindByID)
	group.POST("", rbac.Permission("patients:create"), handler.Create)
	group.PUT("/:id", rbac.Permission("patients:update"), handler.Update)
	group.DELETE("/:id", rbac.Permission("patients:delete"), handler.Delete)
}
