package patient_queue

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
)

func TestPostgresAppointmentTypeAdmin23M(t *testing.T) {
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES (810,'DrAT'),(811,'AdminAT') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(90,810,true,10),(91,811,true,10) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(90,10,true),(91,10,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(810,'P-AT-1','Type','One') ON CONFLICT DO NOTHING`)

	admin := adminAccess(811)
	mgr := Access{UserID: 811, Permissions: map[string]bool{"appointment_type.manage": true}}
	prac := uint(810)
	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	_, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
		PractitionerID: prac, ServiceID: 10, Weekday: int(time.Monday),
		StartTime: "08:00", EndTime: "18:00", ValidFrom: vf,
	}, admin)
	if err != nil {
		t.Fatal(err)
	}

	// Authorized create
	at, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "at-create", Name: "Consult AT", DefaultDurationMinutes: 30,
	}, mgr)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if at.Code != "AT-CREATE" || !at.Active {
		t.Fatalf("normalized create: %+v", at)
	}
	var audits int64
	db.Model(&ScheduleAuditEvent{}).Where("entity_type=? AND event_type=? AND entity_id=?",
		EntityAppointmentType, ApptTypeAuditCreated, at.ID).Count(&audits)
	if audits != 1 {
		t.Fatal("create audit missing")
	}

	// Duplicate code
	if _, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-CREATE", Name: "Dup", DefaultDurationMinutes: 20,
	}, mgr); statusOf(err) != 409 {
		t.Fatalf("dup code want 409 got %d (%v)", statusOf(err), err)
	}

	// Update
	newName := "Consult AT v2"
	dur45 := 45
	upd, err := svc.UpdateAppointmentType(at.ID, UpdateAppointmentTypeRequest{
		Name: &newName, DefaultDurationMinutes: &dur45,
	}, mgr)
	if err != nil || upd.Name != newName || upd.DefaultDurationMinutes != 45 {
		t.Fatalf("update: %v %+v", err, upd)
	}

	// Target inactive organization service rejected
	_ = db.Exec(`UPDATE organization_services SET active=false WHERE id=11`)
	sid11 := uint(11)
	if _, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-INACT-SVC", Name: "BadSvc", DefaultDurationMinutes: 30, ServiceID: &sid11,
	}, mgr); statusOf(err) != 400 {
		t.Fatalf("type→inactive service want 400 got %d (%v)", statusOf(err), err)
	}
	_ = db.Exec(`UPDATE organization_services SET active=true WHERE id=11`)

	// Service-scoped type + incompatible booking service
	atSvc, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-SVC11", Name: "Svc11", DefaultDurationMinutes: 30, ServiceID: &sid11,
	}, mgr)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 810, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &atSvc.ID, StartAt: time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("incompatible service type want 400 got %d", statusOf(err))
	}

	// Soft deactivate + cannot book/reschedule with inactive type
	disabled, err := svc.DisableAppointmentType(at.ID, mgr)
	if err != nil || disabled.Active {
		t.Fatalf("disable: %v %+v", err, disabled)
	}
	_, _, err = svc.BookAppointment(BookAppointmentRequest{
		PatientID: 810, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &at.ID, StartAt: time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC),
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("book inactive type want 400 got %d", statusOf(err))
	}

	activeType, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-ACTIVE-RS", Name: "Active RS", DefaultDurationMinutes: 30,
	}, mgr)
	if err != nil {
		t.Fatal(err)
	}
	booked, _, err := svc.BookAppointment(BookAppointmentRequest{
		PatientID: 810, ServiceID: 10, PractitionerID: &prac,
		AppointmentTypeID: &activeType.ID, StartAt: time.Date(2026, 9, 14, 11, 0, 0, 0, time.UTC),
	}, admin)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Model(&AppointmentType{}).Where("id=?", activeType.ID).Update("active", false)
	_, err = svc.RescheduleAppointment(booked.ID, RescheduleAppointmentRequest{
		StartAt:                time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC),
		ExpectedScheduledAt:    booked.ScheduledAt,
		ExpectedScheduledEndAt: *booked.ScheduledEndAt,
		AppointmentTypeID:      &activeType.ID,
		PractitionerID:         &prac,
	}, admin)
	if statusOf(err) != 400 {
		t.Fatalf("reschedule inactive type want 400 got %d (%v)", statusOf(err), err)
	}

	// GET list still works for schedule.read
	reader := Access{UserID: 811, Permissions: map[string]bool{"schedule.read.all": true}}
	tru := true
	rows, err := svc.ListAppointmentTypes(nil, &tru, reader)
	if err != nil {
		t.Fatal(err)
	}
	foundActive := false
	for _, r := range rows {
		if r.ID == at.ID {
			t.Fatal("deactivated type must not appear in active=true list")
		}
		if r.ID == atSvc.ID {
			foundActive = true
		}
	}
	if !foundActive {
		t.Fatal("active type missing from list")
	}

	manageOnly := Access{UserID: 811, Permissions: map[string]bool{"appointment_type.manage": true}}
	if _, err := svc.ListAppointmentTypes(nil, &tru, manageOnly); err != nil {
		t.Fatalf("appointment_type.manage must list types: %v", err)
	}

	// RBAC negatives (domain)
	denials := []Access{
		{UserID: 1, Permissions: map[string]bool{"schedule.read.all": true}},
		{UserID: 1, Permissions: map[string]bool{"schedule.manage.service": true}},
		{UserID: 1, Permissions: map[string]bool{"appointment.create.service": true}},
		{UserID: 1, Permissions: map[string]bool{"queue.checkin": true}},
		{UserID: 1, Permissions: map[string]bool{"patients:read": true}},
	}
	for _, a := range denials {
		if _, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
			Code: "DENIED", Name: "No", DefaultDurationMinutes: 30,
		}, a); statusOf(err) != 403 {
			t.Fatalf("deny %v want 403 got %d (%v)", a.Permissions, statusOf(err), err)
		}
	}
	// Wildcard still valid
	star := Access{UserID: 811, Permissions: map[string]bool{"*": true}}
	if _, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-STAR", Name: "Star", DefaultDurationMinutes: 25,
	}, star); err != nil {
		t.Fatalf("wildcard create: %v", err)
	}

	// Reactivation rejected when retained serviceId points at an inactive organization service.
	sidRe := uint(11)
	atRe, err := svc.CreateAppointmentType(CreateAppointmentTypeRequest{
		Code: "AT-REACT-SVC", Name: "React Svc", DefaultDurationMinutes: 30, ServiceID: &sidRe,
	}, mgr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DisableAppointmentType(atRe.ID, mgr); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE organization_services SET active=false WHERE id=11`).Error; err != nil {
		t.Fatal(err)
	}
	reactivate := true
	if _, err := svc.UpdateAppointmentType(atRe.ID, UpdateAppointmentTypeRequest{Active: &reactivate}, mgr); statusOf(err) != 400 {
		t.Fatalf("reactivate with inactive service want 400 got %d (%v)", statusOf(err), err)
	}
	var still AppointmentType
	if err := db.First(&still, atRe.ID).Error; err != nil {
		t.Fatal(err)
	}
	if still.Active {
		t.Fatal("appointment type must remain inactive after rejected reactivation")
	}
	if err := db.Exec(`UPDATE organization_services SET active=true WHERE id=11`).Error; err != nil {
		t.Fatal(err)
	}
}

func TestAppointmentTypeAdminRoutesRBAC23M(t *testing.T) {
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

	body := `{"code":"X","name":"Y","defaultDurationMinutes":30}`
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

	negatives := [][]string{
		{"schedule.read.all"},
		{"schedule.manage.service"},
		{"schedule.manage.all"},
		{"appointment.create.service"},
		{"appointment.create.all"},
		{"queue.checkin"},
		{"organization.manage"},
	}
	for _, perms := range negatives {
		if got := request(http.MethodPost, "/api/appointment-types", body, perms); got != http.StatusForbidden {
			t.Fatalf("POST deny %v want 403 got %d", perms, got)
		}
		if got := request(http.MethodPatch, "/api/appointment-types/1", `{"name":"Z"}`, perms); got != http.StatusForbidden {
			t.Fatalf("PATCH deny %v want 403 got %d", perms, got)
		}
		if got := request(http.MethodDelete, "/api/appointment-types/1", "", perms); got != http.StatusForbidden {
			t.Fatalf("DELETE deny %v want 403 got %d", perms, got)
		}
	}
	// organization.manage alone must not unlock mutations (covered above) nor list.
	if got := request(http.MethodGet, "/api/appointment-types", "", []string{"organization.manage"}); got != http.StatusForbidden {
		t.Fatalf("GET list organization.manage want 403 got %d", got)
	}
	// queue.checkin alone is insufficient for list.
	if got := request(http.MethodGet, "/api/appointment-types", "", []string{"queue.checkin"}); got != http.StatusForbidden {
		t.Fatalf("GET list checkin-only want 403 got %d", got)
	}

	listGate := rbac.AnyPermission(
		"schedule.read.own", "schedule.read.service", "schedule.read.all",
		"appointment_type.manage",
	)
	mutateGate := rbac.AnyPermission("appointment_type.manage")

	// Middleware-only positives (no DB): list for manage / schedule.read / *; mutate for manage / *.
	assertAllow := func(t *testing.T, perms []string, method, path string, gate gin.HandlerFunc) {
		t.Helper()
		allow := gin.New()
		allow.Use(func(c *gin.Context) {
			c.Set(rbac.ContextPermissions, perms)
			c.Set(rbac.ContextUserID, uint(1))
			c.Next()
		})
		allow.Handle(method, path, gate, func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		allow.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("%s %v want 204 got %d", method, perms, w.Code)
		}
	}

	assertAllow(t, []string{"appointment_type.manage"}, http.MethodGet, "/api/appointment-types", listGate)
	assertAllow(t, []string{"schedule.read.own"}, http.MethodGet, "/api/appointment-types", listGate)
	assertAllow(t, []string{"schedule.read.service"}, http.MethodGet, "/api/appointment-types", listGate)
	assertAllow(t, []string{"schedule.read.all"}, http.MethodGet, "/api/appointment-types", listGate)
	assertAllow(t, []string{"*"}, http.MethodGet, "/api/appointment-types", listGate)

	assertAllow(t, []string{"appointment_type.manage"}, http.MethodPost, "/api/appointment-types", mutateGate)
	assertAllow(t, []string{"*"}, http.MethodPost, "/api/appointment-types", mutateGate)
	assertAllow(t, []string{"appointment_type.manage"}, http.MethodPatch, "/api/appointment-types/1", mutateGate)
	assertAllow(t, []string{"*"}, http.MethodPatch, "/api/appointment-types/1", mutateGate)
	assertAllow(t, []string{"appointment_type.manage"}, http.MethodDelete, "/api/appointment-types/1", mutateGate)
	assertAllow(t, []string{"*"}, http.MethodDelete, "/api/appointment-types/1", mutateGate)
}
