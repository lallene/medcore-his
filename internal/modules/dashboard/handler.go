package dashboard

import (
	"github.com/gin-gonic/gin"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/core/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Show(c *gin.Context) {
	data, err := h.service.GetDashboard()

	if err != nil {
		response.Error(c, coreerrors.Internal(err.Error()))
		return
	}

	response.Success(c, "Dashboard chargé", data)
}
