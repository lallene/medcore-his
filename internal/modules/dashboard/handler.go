package dashboard

import (
	"github.com/gin-gonic/gin"

	"github.com/lallene/medcore-his/backend/internal/core/period"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func parseDashboardFilter(c *gin.Context) (DashboardFilter, error) {
	return period.Parse(c)
}

func (h *Handler) Show(c *gin.Context) {
	filter, err := parseDashboardFilter(c)
	if err != nil {
		response.Error(c, err)
		return
	}

	data, err := h.service.GetDashboard(filter)

	if err != nil {
		response.Error(c, coreerrors.Internal(err.Error()))
		return
	}

	response.Success(c, "Dashboard chargé", data)
}
