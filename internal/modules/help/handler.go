package help

import (
	"net/http"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct{ service *Service }

func NewHandler(s *Service) *Handler { return &Handler{service: s} }

// Catalog returns the help entries visible to the caller in one response.
// ETag varies with the visible content and the caller's permissions.
func (h *Handler) Catalog(c *gin.Context) {
	raw, _ := c.Get(rbac.ContextPermissions)
	perms, _ := raw.([]string)
	out, err := h.service.Visible(perms)
	if err != nil {
		response.Error(c, coreerrors.Internal("Aide indisponible"))
		return
	}
	etag := `"` + out.Version + `"`
	c.Header("Cache-Control", "private, max-age=0, must-revalidate")
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}
	c.JSON(http.StatusOK, out)
}
