package router

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/lallene/medcore-his/backend/internal/modules/patients"
)

func Register(r *gin.Engine, db *gorm.DB) {
	r.GET("/", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "MedCore HIS API",
		})
	})

	api := r.Group("/api")
	patients.RegisterRoutes(api, db)

}
