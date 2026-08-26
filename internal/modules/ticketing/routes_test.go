package ticketing

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

func TestTicketRoutesRequireJWTContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(&Service{}))
	for _, path := range []string{"/api/tickets", "/api/ticketing/categories", "/api/ticketing/kpis"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s: got %d", path, res.Code)
		}
	}
}

func TestTicketMutationRoutesEnforcePermissions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Test-Permissions"); raw != "" {
			c.Set(rbac.ContextPermissions, strings.Split(raw, ","))
		}
		c.Next()
	})
	RegisterRoutes(r.Group("/api"), NewHandler(&Service{}))
	request := func(method, path string, permissions []string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if permissions != nil {
			req.Header.Set("X-Test-Permissions", strings.Join(permissions, ","))
		}
		r.ServeHTTP(w, req)
		return w.Code
	}
	if got := request(http.MethodPatch, "/api/tickets/1", nil); got != http.StatusUnauthorized {
		t.Fatalf("anonymous patch status=%d", got)
	}
	if got := request(http.MethodPost, "/api/tickets/1/workflow", nil); got != http.StatusUnauthorized {
		t.Fatalf("anonymous workflow status=%d", got)
	}
	if got := request(http.MethodPatch, "/api/tickets/1", []string{"ticket.read.own", "ticket.comment"}); got != http.StatusForbidden {
		t.Fatalf("patch without ticket.update status=%d", got)
	}
	if got := request(http.MethodPost, "/api/tickets/1/workflow", []string{"ticket.read.own", "ticket.comment"}); got != http.StatusForbidden {
		t.Fatalf("workflow without mutation permission status=%d", got)
	}
}

func TestKPIsRefuseSupportWithoutService(t *testing.T) {
	svc := &Service{}
	_, err := svc.KPIs(Access{UserID: 9, Permissions: map[string]bool{"ticket.read.service": true}})
	if err == nil {
		t.Fatal("expected forbidden when support has no service")
	}
	if !strings.Contains(err.Error(), "Périmètre service") && !strings.Contains(err.Error(), "KPI support") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestKPIsRefuseRequester(t *testing.T) {
	svc := &Service{}
	_, err := svc.KPIs(Access{UserID: 3, Permissions: map[string]bool{"ticket.read.own": true, "ticket.comment": true}})
	if err == nil {
		t.Fatal("requester must not access KPIs")
	}
}

func TestPostgresKPIServiceScopeAndReopened(t *testing.T) {
	db := ticketingPostgres(t)
	now := time.Now().UTC().Truncate(time.Second)
	svcA, svcB := uint(11), uint(22)
	mk := func(ref string, serviceID uint, reopen bool) Ticket {
		ticket := Ticket{
			Reference: ref, Type: "INCIDENT", Title: ref, Description: "scope", Status: "IN_PROGRESS", Priority: "P2",
			Impact: "SERVICE", Urgency: "MEDIUM", RequesterUserID: 50, ServiceID: &serviceID,
			ResponseDueAt: now.Add(time.Hour), ResolutionDueAt: now.Add(4 * time.Hour), CreatedAt: now, UpdatedAt: now,
		}
		if e := db.Create(&ticket).Error; e != nil {
			t.Fatal(e)
		}
		if reopen {
			if e := db.Create(&History{TicketID: ticket.ID, ActorUserID: 1, EventType: "REOPENED", CreatedAt: now}).Error; e != nil {
				t.Fatal(e)
			}
		}
		return ticket
	}
	mk("INC-A-OPEN", svcA, false)
	mk("INC-A-REOPEN", svcA, true)
	mk("INC-B-REOPEN", svcB, true)
	service := NewService(db)

	manager, err := service.KPIs(Access{UserID: 1, Permissions: map[string]bool{"ticket.read.all": true, "ticket.read.service": true}})
	if err != nil {
		t.Fatal(err)
	}
	if manager.Reopened != 2 {
		t.Fatalf("manager reopened=%d want 2", manager.Reopened)
	}

	agentA, err := service.KPIs(Access{UserID: 2, ServiceID: &svcA, Permissions: map[string]bool{"ticket.read.service": true}})
	if err != nil {
		t.Fatal(err)
	}
	if agentA.Open != 2 || agentA.Reopened != 1 {
		t.Fatalf("agent A open=%d reopened=%d", agentA.Open, agentA.Reopened)
	}

	agentB, err := service.KPIs(Access{UserID: 3, ServiceID: &svcB, Permissions: map[string]bool{"ticket.read.service": true}})
	if err != nil {
		t.Fatal(err)
	}
	if agentB.Open != 1 || agentB.Reopened != 1 {
		t.Fatalf("agent B open=%d reopened=%d", agentB.Open, agentB.Reopened)
	}

	_, err = service.KPIs(Access{UserID: 4, Permissions: map[string]bool{"ticket.read.service": true}})
	if err == nil {
		t.Fatal("support without service must not receive global KPIs")
	}

	ticketB := Ticket{}
	db.Where("reference=?", "INC-B-REOPEN").First(&ticketB)
	if _, err = service.Detail(ticketB.ID, Access{UserID: 2, ServiceID: &svcA, Permissions: map[string]bool{"ticket.read.service": true, "ticket.read.own": true}}); err == nil {
		t.Fatal("service A must not read service B ticket")
	}
}
