package help

import "github.com/gin-gonic/gin"

// RegisterRoutes mounts the read-only help catalog. Authentication is required
// (router group middleware); visibility is filtered by permissions in the service.
func RegisterRoutes(r *gin.RouterGroup, h *Handler) {
	r.GET("/help/entries", h.Catalog)
}
