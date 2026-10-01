package performed_acts

import (
	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	g := r.Group("/performed-acts")

	// Producer maps (register before /:id).
	maps := g.Group("/producer-maps")
	maps.GET("", rbac.Permission("performed_acts.producer_map.read"), h.ListProducerMaps)
	maps.GET("/readiness", rbac.Permission("performed_acts.producer_map.read"), h.ProducerReadiness)
	maps.POST("", rbac.Permission("performed_acts.producer_map.manage"), h.UpsertProducerMap)
	maps.GET("/:id", rbac.Permission("performed_acts.producer_map.read"), h.GetProducerMap)
	maps.PUT("/:id", rbac.Permission("performed_acts.producer_map.manage"), h.UpdateProducerMap)
	maps.POST("/:id/deactivate", rbac.Permission("performed_acts.producer_map.manage"), h.DeactivateProducerMap)

	g.GET("", rbac.Permission("performed_acts.read"), h.List)
	g.GET("/:id", rbac.Permission("performed_acts.read"), h.Get)
	g.POST("", rbac.Permission("performed_acts.create"), h.Create)
	g.POST("/:id/void", rbac.Permission("performed_acts.void"), h.Void)
}
