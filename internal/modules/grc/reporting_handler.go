package grc

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

// parseReportingFrameworkIDs reads frameworkIds=1,2,3. Positive integers,
// duplicates removed in first-seen order, at most ComplianceCoverageMaxFrameworks
// distinct ids. Absent or blank means compliance is not filtered. Unknown ids
// are not rejected here: the coverage query ignores them, as the compliance
// coverage API does. Zero, negative and non-integers are 400.
func parseReportingFrameworkIDs(c *gin.Context) ([]uint, bool) {
	raw := strings.TrimSpace(c.Query("frameworkIds"))
	if raw == "" {
		return nil, true
	}
	seen := map[uint]bool{}
	ids := make([]uint, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil || value == 0 {
			fail(c, coreerrors.BadRequest("frameworkIds invalide"))
			return nil, false
		}
		id := uint(value)
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(ids) >= ComplianceCoverageMaxFrameworks {
			fail(c, coreerrors.BadRequest("frameworkIds invalide"))
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func (h *Handler) GetReporting(c *gin.Context) {
	var filter ReportingFilter
	var ok bool
	if filter.EvidenceExpiringSoonDays, ok = parseDashboardWindow(c, "evidenceExpiringSoonDays"); !ok {
		return
	}
	if filter.ActionDueSoonDays, ok = parseDashboardWindow(c, "actionDueSoonDays"); !ok {
		return
	}
	if filter.FrameworkIDs, ok = parseReportingFrameworkIDs(c); !ok {
		return
	}

	report, err := h.service.Reporting(filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, report)
}
