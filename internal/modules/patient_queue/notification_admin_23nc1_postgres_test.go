package patient_queue

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/core/scheduling"
	"gorm.io/gorm"
)

func notificationAdminSeed(t *testing.T) (*gorm.DB, *Service, Access, Access, Access, []AppointmentNotificationIntent) {
	t.Helper()
	db := queuePostgres(t)
	svc := NewService(db)
	_ = scheduling.SetLocation("UTC")

	_ = db.Exec(`INSERT INTO users(id, name) VALUES
		(9201,'NotifMgr10'),(9202,'NotifAdmin'),(9203,'Prac10'),(9204,'Prac11') ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_profiles(id, user_id, active, primary_service_id) VALUES
		(9201,9201,true,10),(9202,9202,true,10),(9203,9203,true,10),(9204,9204,true,11) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO staff_service_assignments(profile_id, service_id, active) VALUES
		(9201,10,true),(9202,10,true),(9203,10,true),(9204,11,true) ON CONFLICT DO NOTHING`)
	_ = db.Exec(`INSERT INTO patients(id, code_patient, nom, prenoms) VALUES
		(9201,'P-N-1','Notif','One'),(9202,'P-N-2','Notif','Two') ON CONFLICT DO NOTHING`)

	admin := adminAccess(9202)
	mgr10 := scopedAccess(9201, 10, "schedule.manage.service")
	reader := scopedAccess(9201, 10, "schedule.read.all", "schedule.read.service")

	vf := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		prac, svcID uint
		wd          int
	}{
		{9203, 10, int(time.Monday)},
		{9204, 11, int(time.Monday)},
	} {
		if _, err := svc.CreateWorkingSchedule(CreateWorkingScheduleRequest{
			PractitionerID: row.prac, ServiceID: row.svcID, Weekday: row.wd,
			StartTime: "08:00", EndTime: "18:00", ValidFrom: vf,
		}, admin); err != nil {
			t.Fatal(err)
		}
	}

	start10 := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	start11 := time.Date(2026, 10, 12, 10, 0, 0, 0, time.UTC)
	end10 := start10.Add(30 * time.Minute)
	end11 := start11.Add(30 * time.Minute)

	appt10 := Appointment{
		PatientID: 9201, ServiceID: 10, ExpectedDoctorID: uintPtr(9203),
		ScheduledAt: start10, ScheduledEndAt: &end10, Status: ApptScheduled,
		Reason: "CONFIDENTIAL-REASON-PHI", CreatedBy: 9202,
	}
	appt11 := Appointment{
		PatientID: 9202, ServiceID: 11, ExpectedDoctorID: uintPtr(9204),
		ScheduledAt: start11, ScheduledEndAt: &end11, Status: ApptScheduled,
		Reason: "OTHER-SERVICE-REASON", CreatedBy: 9202,
	}
	if err := db.Create(&appt10).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&appt11).Error; err != nil {
		t.Fatal(err)
	}

	_, payload10, err := BuildNotificationPayload(appt10.ID, start10, "Consult", "Urgences", "ClinicA")
	if err != nil {
		t.Fatal(err)
	}
	_, payload11, err := BuildNotificationPayload(appt11.ID, start11, "Consult", "Médecine", "ClinicB")
	if err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	intents := []AppointmentNotificationIntent{
		{
			AppointmentID: appt10.ID, PatientID: 9201,
			Kind: NotifKindBooked, Channel: NotifChannelLog,
			OccurrenceKey: OccurrenceKeyFromScheduledAt(start10),
			SendAfter:     start10.Add(-48 * time.Hour),
			Status:        NotifStatusSent,
			PayloadJSON:   payload10,
			CreatedAt:     base.Add(2 * time.Hour),
			UpdatedAt:     base.Add(2 * time.Hour),
			SentAt:        timePtr(base.Add(3 * time.Hour)),
		},
		{
			AppointmentID: appt10.ID, PatientID: 9201,
			Kind: NotifKindReminderT24H, Channel: NotifChannelLog,
			OccurrenceKey: OccurrenceKeyFromScheduledAt(start10),
			SendAfter:     ReminderSendAfterT24H(start10),
			Status:        NotifStatusPending,
			PayloadJSON:   payload10,
			CreatedAt:     base.Add(1 * time.Hour),
			UpdatedAt:     base.Add(1 * time.Hour),
		},
		{
			AppointmentID: appt10.ID, PatientID: 9201,
			Kind: NotifKindCancelled, Channel: NotifChannelEmail,
			OccurrenceKey: OccurrenceKeyFromScheduledAt(start10),
			SendAfter:     base,
			Status:        NotifStatusFailed,
			PayloadJSON:   payload10,
			CreatedAt:     base,
			UpdatedAt:     base,
		},
		{
			AppointmentID: appt11.ID, PatientID: 9202,
			Kind: NotifKindBooked, Channel: NotifChannelLog,
			OccurrenceKey: OccurrenceKeyFromScheduledAt(start11),
			SendAfter:     start11.Add(-48 * time.Hour),
			Status:        NotifStatusSent,
			PayloadJSON:   payload11,
			CreatedAt:     base.Add(4 * time.Hour),
			UpdatedAt:     base.Add(4 * time.Hour),
			SentAt:        timePtr(base.Add(5 * time.Hour)),
		},
	}
	for i := range intents {
		if err := db.Create(&intents[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	msgID := "log-msg-1"
	errMsg := "adapter boom"
	attempts := []AppointmentNotificationAttempt{
		{IntentID: intents[0].ID, AttemptNo: 1, Provider: "log", ProviderMessageID: &msgID, CreatedAt: base.Add(3 * time.Hour)},
		{IntentID: intents[2].ID, AttemptNo: 2, Provider: "log", Error: &errMsg, CreatedAt: base.Add(30 * time.Minute)},
		{IntentID: intents[2].ID, AttemptNo: 1, Provider: "log", Error: &errMsg, CreatedAt: base.Add(10 * time.Minute)},
	}
	for i := range attempts {
		if err := db.Create(&attempts[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	return db, svc, admin, mgr10, reader, intents
}

func TestPostgresNotificationAdminRead23NC1(t *testing.T) {
	db, svc, admin, mgr10, reader, intents := notificationAdminSeed(t)
	_ = db

	// 2. schedule.read.* alone cannot inspect
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 50}, reader); statusOf(err) != 403 {
		t.Fatalf("read.* list want 403 got %d (%v)", statusOf(err), err)
	}
	if _, err := svc.GetNotificationIntentAdmin(intents[0].ID, reader); statusOf(err) != 403 {
		t.Fatalf("read.* get want 403 got %d (%v)", statusOf(err), err)
	}
	if _, err := svc.ListNotificationAttemptsAdmin(intents[0].ID, reader); statusOf(err) != 403 {
		t.Fatalf("read.* attempts want 403 got %d (%v)", statusOf(err), err)
	}

	// 3. manage.service lists in-scope only (service 10 → 3 intents)
	list10, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 50}, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	if list10.Total != 3 || len(list10.Items) != 3 {
		t.Fatalf("manage.service total=%d items=%d want 3", list10.Total, len(list10.Items))
	}
	for _, it := range list10.Items {
		if it.AppointmentID != intents[0].AppointmentID {
			t.Fatalf("out-of-scope appointment leaked: %+v", it)
		}
	}

	// 4–6. out-of-scope detail/attempts → 404
	outID := intents[3].ID
	if _, err := svc.GetNotificationIntentAdmin(outID, mgr10); statusOf(err) != 404 {
		t.Fatalf("out-of-scope get want 404 got %d (%v)", statusOf(err), err)
	}
	if _, err := svc.ListNotificationAttemptsAdmin(outID, mgr10); statusOf(err) != 404 {
		t.Fatalf("out-of-scope attempts want 404 got %d (%v)", statusOf(err), err)
	}

	// 7. manage.all sees all 4
	listAll, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 50}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if listAll.Total != 4 {
		t.Fatalf("manage.all total=%d want 4", listAll.Total)
	}

	// 8. pagination
	page1, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 2}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 4 || len(page1.Items) != 2 || page1.Page != 1 || page1.Limit != 2 {
		t.Fatalf("page1=%+v", page1)
	}
	page2, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 2, Limit: 2}, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 2 {
		t.Fatalf("page2 items=%d", len(page2.Items))
	}
	if page1.Items[0].ID == page2.Items[0].ID {
		t.Fatal("pagination overlap")
	}

	// 15. deterministic ordering: created_at DESC, id DESC
	if !page1.Items[0].CreatedAt.After(page1.Items[1].CreatedAt) && page1.Items[0].ID <= page1.Items[1].ID {
		// allow equal created_at with id desc
		if page1.Items[0].CreatedAt.Equal(page1.Items[1].CreatedAt) && page1.Items[0].ID < page1.Items[1].ID {
			t.Fatalf("order not deterministic: %+v then %+v", page1.Items[0], page1.Items[1])
		}
	}
	for i := 1; i < len(listAll.Items); i++ {
		prev, cur := listAll.Items[i-1], listAll.Items[i]
		if prev.CreatedAt.Before(cur.CreatedAt) {
			t.Fatalf("created_at not desc: %v then %v", prev.CreatedAt, cur.CreatedAt)
		}
		if prev.CreatedAt.Equal(cur.CreatedAt) && prev.ID < cur.ID {
			t.Fatalf("id tie-break not desc: %d then %d", prev.ID, cur.ID)
		}
	}

	// 9–12. filters
	byStatus, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Status: NotifStatusPending, Page: 1, Limit: 50}, admin)
	if err != nil || byStatus.Total != 1 || byStatus.Items[0].Kind != NotifKindReminderT24H {
		t.Fatalf("status filter: %+v %v", byStatus, err)
	}
	byKind, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Kind: NotifKindBooked, Page: 1, Limit: 50}, admin)
	if err != nil || byKind.Total != 2 {
		t.Fatalf("kind filter total=%d err=%v", byKind.Total, err)
	}
	byChannel, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Channel: NotifChannelEmail, Page: 1, Limit: 50}, admin)
	if err != nil || byChannel.Total != 1 {
		t.Fatalf("channel filter total=%d err=%v", byChannel.Total, err)
	}
	apptID := intents[0].AppointmentID
	byAppt, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{AppointmentID: &apptID, Page: 1, Limit: 50}, admin)
	if err != nil || byAppt.Total != 3 {
		t.Fatalf("appointmentId filter total=%d err=%v", byAppt.Total, err)
	}

	// 13. timestamp range filters
	from := time.Date(2026, 9, 20, 13, 0, 0, 0, time.UTC)
	to := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	byCreated, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{
		CreatedAtFrom: &from, CreatedAtTo: &to, Page: 1, Limit: 50,
	}, admin)
	if err != nil || byCreated.Total != 2 {
		t.Fatalf("createdAt range total=%d err=%v items=%+v", byCreated.Total, err, byCreated.Items)
	}
	sendFrom := ReminderSendAfterT24H(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)).Add(-time.Minute)
	sendTo := ReminderSendAfterT24H(time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)).Add(time.Minute)
	bySend, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{
		SendAfterFrom: &sendFrom, SendAfterTo: &sendTo, Page: 1, Limit: 50,
	}, admin)
	if err != nil || bySend.Total != 1 || bySend.Items[0].Kind != NotifKindReminderT24H {
		t.Fatalf("sendAfter range: %+v %v", bySend, err)
	}

	// 14. invalid filters
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Status: "NOPE", Page: 1, Limit: 50}, admin); statusOf(err) != 400 {
		t.Fatalf("bad status want 400 got %d", statusOf(err))
	}
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Kind: "NOPE", Page: 1, Limit: 50}, admin); statusOf(err) != 400 {
		t.Fatalf("bad kind want 400 got %d", statusOf(err))
	}
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Channel: "FAX", Page: 1, Limit: 50}, admin); statusOf(err) != 400 {
		t.Fatalf("bad channel want 400 got %d", statusOf(err))
	}
	badFrom := to
	badTo := from
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{
		CreatedAtFrom: &badFrom, CreatedAtTo: &badTo, Page: 1, Limit: 50,
	}, admin); statusOf(err) != 400 {
		t.Fatalf("inverted range want 400 got %d", statusOf(err))
	}

	// 16. attempts ordered by attempt_no ASC
	atts, err := svc.ListNotificationAttemptsAdmin(intents[2].ID, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 || atts[0].AttemptNo != 1 || atts[1].AttemptNo != 2 {
		t.Fatalf("attempts order=%+v", atts)
	}

	// 17. invalid / missing ID
	if _, err := svc.GetNotificationIntentAdmin(0, admin); statusOf(err) != 400 {
		t.Fatalf("id 0 want 400 got %d", statusOf(err))
	}
	if _, err := svc.GetNotificationIntentAdmin(999999, admin); statusOf(err) != 404 {
		t.Fatalf("missing want 404 got %d", statusOf(err))
	}

	// 19. attemptCount
	detail, err := svc.GetNotificationIntentAdmin(intents[0].ID, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	if detail.AttemptCount != 1 {
		t.Fatalf("attemptCount sent intent=%d", detail.AttemptCount)
	}
	failed, err := svc.GetNotificationIntentAdmin(intents[2].ID, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	if failed.AttemptCount != 2 {
		t.Fatalf("attemptCount failed intent=%d", failed.AttemptCount)
	}
	pending, err := svc.GetNotificationIntentAdmin(intents[1].ID, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	if pending.AttemptCount != 0 {
		t.Fatalf("attemptCount pending=%d", pending.AttemptCount)
	}

	// 18. PHI-safe response — corrupt payload with forbidden keys must not serialize them
	poison := `{"appointmentId":` + strconv.FormatUint(uint64(intents[0].AppointmentID), 10) +
		`,"scheduledAt":"2026-10-12T09:00:00Z","reason":"SECRET","telephone":"+221","email":"a@b.c","diagnosis":"flu","unknownKey":"x"}`
	if err := db.Model(&AppointmentNotificationIntent{}).Where("id=?", intents[0].ID).
		Update("payload_json", poison).Error; err != nil {
		t.Fatal(err)
	}
	safe, err := svc.GetNotificationIntentAdmin(intents[0].ID, mgr10)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(safe)
	if err != nil {
		t.Fatal(err)
	}
	lower := strings.ToLower(string(raw))
	for _, bad := range []string{`"reason"`, `"telephone"`, `"email"`, `"diagnosis"`, `"unknownkey"`, "secret", "+221", "a@b.c"} {
		if strings.Contains(lower, bad) {
			t.Fatalf("PHI leak %q in response %s", bad, raw)
		}
	}
	if safe.Payload.AppointmentID != intents[0].AppointmentID {
		t.Fatalf("fallback payload appointmentId=%d", safe.Payload.AppointmentID)
	}
}

func TestPostgresNotificationAdminPHIAllowList23NC1(t *testing.T) {
	_, svc, admin, _, _, intents := notificationAdminSeed(t)
	dto, err := svc.GetNotificationIntentAdmin(intents[1].ID, admin)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(dto)
	s := string(b)
	if strings.Contains(s, "payloadJson") || strings.Contains(s, "payload_json") {
		t.Fatalf("raw payload field leaked: %s", s)
	}
	if strings.Contains(strings.ToLower(s), "confidential-reason") {
		t.Fatalf("appointment reason leaked: %s", s)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["payload"]; !ok {
		t.Fatal("payload object required")
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(obj["payload"], &payload); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]struct{}{
		"appointmentId": {}, "scheduledAt": {}, "appointmentTypeName": {}, "serviceName": {}, "clinicLabel": {},
	}
	for k := range payload {
		if _, ok := allowed[k]; !ok {
			t.Fatalf("unexpected payload key %q", k)
		}
	}
}

func TestNotificationAdminRoutesRBAC23NC1(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if raw := c.GetHeader("X-Test-Permissions"); raw != "" {
			c.Set(rbac.ContextPermissions, strings.Split(raw, ","))
			c.Set(rbac.ContextUserID, uint(1))
		}
		c.Next()
	})
	RegisterRoutes(r.Group("/api"), NewHandler(NewService(nil)))

	request := func(method, path string, permissions []string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		if permissions != nil {
			req.Header.Set("X-Test-Permissions", strings.Join(permissions, ","))
		}
		r.ServeHTTP(w, req)
		return w.Code
	}

	paths := []string{
		"/api/appointment-notification-intents",
		"/api/appointment-notification-intents/1",
		"/api/appointment-notification-intents/1/attempts",
	}

	// 1. unauthenticated
	for _, p := range paths {
		if got := request(http.MethodGet, p, nil); got != http.StatusUnauthorized {
			t.Fatalf("unauth %s want 401 got %d", p, got)
		}
	}

	// schedule.read.* alone denied
	for _, perms := range [][]string{
		{"schedule.read.own"},
		{"schedule.read.service"},
		{"schedule.read.all"},
		{"appointment_type.manage"},
		{"queue.checkin"},
	} {
		for _, p := range paths {
			if got := request(http.MethodGet, p, perms); got != http.StatusForbidden {
				t.Fatalf("deny %v %s want 403 got %d", perms, p, got)
			}
		}
	}

	gate := rbac.AnyPermission("schedule.manage.service", "schedule.manage.all")
	assertAllow := func(t *testing.T, perms []string, path string) {
		t.Helper()
		allow := gin.New()
		allow.Use(func(c *gin.Context) {
			c.Set(rbac.ContextPermissions, perms)
			c.Set(rbac.ContextUserID, uint(1))
			c.Next()
		})
		allow.GET(path, gate, func(c *gin.Context) { c.Status(http.StatusNoContent) })
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		allow.ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("allow %v %s want 204 got %d", perms, path, w.Code)
		}
	}
	for _, p := range paths {
		assertAllow(t, []string{"schedule.manage.service"}, p)
		assertAllow(t, []string{"schedule.manage.all"}, p)
		assertAllow(t, []string{"*"}, p)
	}
}

func TestNotificationAdminPaginationValidation23NC1(t *testing.T) {
	gin.SetMode(gin.TestMode)

	parse := func(rawQuery string) (NotificationAdminListFilter, error) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req := httptest.NewRequest(http.MethodGet, "/api/appointment-notification-intents?"+rawQuery, nil)
		c.Request = req
		return parseNotificationAdminListFilter(c)
	}

	// missing page/limit => defaults 1/50
	f, err := parse("")
	if err != nil || f.Page != 1 || f.Limit != 50 {
		t.Fatalf("defaults: %+v %v", f, err)
	}
	f, err = parse("status=PENDING")
	if err != nil || f.Page != 1 || f.Limit != 50 {
		t.Fatalf("defaults with other qs: %+v %v", f, err)
	}

	// malformed / out-of-range => 400
	cases := []struct {
		q string
	}{
		{"page=abc"},
		{"page=0"},
		{"page=-1"},
		{"limit=abc"},
		{"limit=0"},
		{"limit=101"},
		{"page=1&limit=101"},
		{"page=0&limit=50"},
	}
	for _, tc := range cases {
		_, err := parse(tc.q)
		if statusOf(err) != 400 {
			t.Fatalf("%s want 400 got %d (%v)", tc.q, statusOf(err), err)
		}
	}

	// valid explicit page/limit
	f, err = parse("page=2&limit=25")
	if err != nil || f.Page != 2 || f.Limit != 25 {
		t.Fatalf("valid: %+v %v", f, err)
	}
	f, err = parse("page=1&limit=100")
	if err != nil || f.Page != 1 || f.Limit != 100 {
		t.Fatalf("limit=100: %+v %v", f, err)
	}

	// service also rejects invalid pagination (no silent normalize)
	_, svc, admin, _, _, _ := notificationAdminSeed(t)
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 0, Limit: 50}, admin); statusOf(err) != 400 {
		t.Fatalf("service page=0 want 400 got %d", statusOf(err))
	}
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 0}, admin); statusOf(err) != 400 {
		t.Fatalf("service limit=0 want 400 got %d", statusOf(err))
	}
	if _, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 101}, admin); statusOf(err) != 400 {
		t.Fatalf("service limit=101 want 400 got %d", statusOf(err))
	}
	ok, err := svc.ListNotificationIntentsAdmin(NotificationAdminListFilter{Page: 1, Limit: 2}, admin)
	if err != nil || ok.Page != 1 || ok.Limit != 2 || len(ok.Items) != 2 {
		t.Fatalf("service valid pagination: %+v %v", ok, err)
	}
}
