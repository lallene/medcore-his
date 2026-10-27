package grc

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// parseFrameworkIDs reads frameworkIds=1,2,3: positive integers, at most
// ComplianceCoverageMaxFrameworks; absent means no breakdown.
func parseFrameworkIDs(c *gin.Context) ([]uint, bool) {
	raw := strings.TrimSpace(c.Query("frameworkIds"))
	if raw == "" {
		return nil, true
	}
	parts := strings.Split(raw, ",")
	if len(parts) > ComplianceCoverageMaxFrameworks {
		fail(c, coreerrors.BadRequest("frameworkIds invalide"))
		return nil, false
	}
	ids := make([]uint, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil || value == 0 {
			fail(c, coreerrors.BadRequest("frameworkIds invalide"))
			return nil, false
		}
		ids = append(ids, uint(value))
	}
	return ids, true
}

func (h *Handler) GetComplianceCoverage(c *gin.Context) {
	ids, ok := parseFrameworkIDs(c)
	if !ok {
		return
	}
	report, err := h.service.ComplianceCoverageReport(ids)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, report)
}
