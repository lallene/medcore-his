package patient_queue

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
)

// LOT 23I — both booking HTTP surfaces reject queue.checkin-only at middleware.
func TestBookingRoutesRejectQueueCheckinOnly23I(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Test-Permissions"); raw != "" {
			c.Set(rbac.ContextPermissions, strings.Split(raw, ","))
			c.Set(rbac.ContextUserID, uint(1))
		}
		c.Next()
	})
	// Handler unused for denied requests (middleware aborts before service).
	RegisterRoutes(r.Group("/api"), NewHandler(NewService(nil)))

	body := `{"patientId":1,"serviceId":10,"startAt":"2026-09-14T09:00:00Z","durationMinutes":30}`
	legacyBody := `{"patientId":1,"serviceId":10,"scheduledAt":"2026-09-14T09:00:00Z","scheduledEndAt":"2026-09-14T09:30:00Z"}`

	request := func(method, path, payload string, permissions []string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		if permissions != nil {
			req.Header.Set("X-Test-Permissions", strings.Join(permissions, ","))
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	if got := request(http.MethodPost, "/api/appointments", body, []string{"queue.checkin"}); got != http.StatusForbidden {
		t.Fatalf("POST /api/appointments checkin-only want 403 got %d", got)
	}
	if got := request(http.MethodPost, "/api/queue/appointments", legacyBody, []string{"queue.checkin"}); got != http.StatusForbidden {
		t.Fatalf("POST /api/queue/appointments checkin-only want 403 got %d", got)
	}
	if got := request(http.MethodPost, "/api/queue/appointments/1/check-in", `{"identityConfirmed":true}`, []string{"appointment.create.service"}); got != http.StatusForbidden {
		t.Fatalf("check-in without queue.checkin want 403 got %d", got)
	}
}

func TestCanBookAppointmentsExcludesQueueCheckin23I(t *testing.T) {
	svc := &Service{}
	checkin := Access{UserID: 1, Permissions: map[string]bool{"queue.checkin": true}}
	if svc.canBookAppointments(checkin) {
		t.Fatal("queue.checkin alone must not canBookAppointments")
	}
	create := Access{UserID: 1, Permissions: map[string]bool{"appointment.create.service": true}}
	if !svc.canBookAppointments(create) {
		t.Fatal("appointment.create.service must canBookAppointments")
	}
	createAll := Access{UserID: 1, Permissions: map[string]bool{"appointment.create.all": true}}
	if !svc.canBookAppointments(createAll) {
		t.Fatal("appointment.create.all must canBookAppointments")
	}
}
