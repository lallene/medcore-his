package grc

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// parseDashboardWindow defaults an absent window to 30 days and rejects a
// malformed one with 400; the service rejects values outside 1–365.
func parseDashboardWindow(c *gin.Context, name string) (int, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return DashboardDefaultWindowDays, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		fail(c, coreerrors.BadRequest(name+" invalide"))
		return 0, false
	}
	return value, true
}

func (h *Handler) GetDashboard(c *gin.Context) {
	var filter DashboardFilter
	var ok bool
	if filter.ExpiringSoonDays, ok = parseDashboardWindow(c, "expiringSoonDays"); !ok {
		return
	}
	if filter.DueSoonDays, ok = parseDashboardWindow(c, "dueSoonDays"); !ok {
		return
	}

	dashboard, err := h.service.Dashboard(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, dashboard)
}
