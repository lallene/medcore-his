package dashboard

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func dashboardTestContext(rawQuery string) *gin.Context {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/dashboard?"+rawQuery, nil)

	return c
}

func TestParseDashboardFilterEmpty(t *testing.T) {
	filter, err := parseDashboardFilter(dashboardTestContext(""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filter.ServiceID != nil || filter.From != "" || filter.To != "" {
		t.Fatalf("expected empty filter, got %+v", filter)
	}
}

func TestParseDashboardFilterValid(t *testing.T) {
	filter, err := parseDashboardFilter(
		dashboardTestContext("serviceId=3&from=2026-10-24&to=2026-10-25"),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if filter.ServiceID == nil || *filter.ServiceID != 3 {
		t.Fatalf("expected serviceId 3, got %+v", filter.ServiceID)
	}

	if filter.From != "2026-10-24" {
		t.Fatalf("expected from 2026-10-24, got %q", filter.From)
	}
	if filter.To != "2026-10-25" {
		t.Fatalf("expected to 2026-10-25, got %q", filter.To)
	}
}

func TestParseDashboardFilterRejectsInvalidServiceID(t *testing.T) {
	for _, raw := range []string{
		"serviceId=0",
		"serviceId=abc",
		"serviceId=-1",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseDashboardFilter(dashboardTestContext(raw)); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}

func TestParseDashboardFilterRejectsInvalidDates(t *testing.T) {
	for _, raw := range []string{
		"from=24-10-2026",
		"from=invalid",
		"to=25-10-2026",
		"to=invalid",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseDashboardFilter(dashboardTestContext(raw)); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}

func TestParseDashboardFilterRejectsInvalidRange(t *testing.T) {
	for _, raw := range []string{
		"from=2026-10-24&to=2026-10-24",
		"from=2026-10-25&to=2026-10-24",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseDashboardFilter(dashboardTestContext(raw)); err == nil {
				t.Fatalf("expected error for %q", raw)
			}
		})
	}
}
