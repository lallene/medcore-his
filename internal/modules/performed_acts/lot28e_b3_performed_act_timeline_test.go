package performed_acts

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func b3DB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:lot28e_b3_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&patients.Patient{}, &act_catalog.Entry{}, &Act{}, &ProducerMap{},
		&medical_records.MedicalRecord{}, &medical_records.MedicalTimelineEvent{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func b3Seed(t *testing.T, db *gorm.DB, suffix string) (patients.Patient, act_catalog.Entry, medical_records.MedicalRecord) {
	t.Helper()
	p := patients.Patient{CodePatient: "B3-P-" + suffix, NumeroDossier: "B3-D-" + suffix, Nom: "B3"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "B3-" + suffix, Label: "Acte B3 " + suffix, Category: "PROCEDURE",
		BasePrice: 15000, Currency: "XOF", Billable: true, InsuranceEligible: true, IsActive: true,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	rec := medical_records.MedicalRecord{PatientID: p.ID, RecordNumber: "B3-MR-" + suffix, Status: "active"}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return p, cat, rec
}

func b3CountEvents(t *testing.T, db *gorm.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&medical_records.MedicalTimelineEvent{}).Where(query, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func b3FindEvent(t *testing.T, db *gorm.DB, eventType string, refID uint) medical_records.MedicalTimelineEvent {
	t.Helper()
	var ev medical_records.MedicalTimelineEvent
	if err := db.Where("event_type = ? AND reference_id = ?", eventType, refID).First(&ev).Error; err != nil {
		t.Fatalf("event %s ref=%d: %v", eventType, refID, err)
	}
	return ev
}

func b3PayloadContainsForbidden(s string) bool {
	lower := strings.ToLower(s)
	for _, frag := range []string{
		"price", "tarif", "base_price", "patient_share", "insurer_share",
		"coverage", "authorization", "payment", "invoice", "voidreason", "void_reason",
	} {
		if strings.Contains(lower, frag) {
			return true
		}
	}
	return false
}

// P101 — create persists PA + exactly one performed_act_performed.
func TestLot28EB3_P101_CreatePerformedTimeline(t *testing.T) {
	db := b3DB(t)
	p, cat, rec := b3Seed(t, db, "101")
	actor := uint(42)
	act, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if act.Status != StatusPerformed {
		t.Fatalf("status=%s", act.Status)
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActPerformed, act.ID) != 1 {
		t.Fatal("P101: expected exactly one performed event")
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	if ev.Category != TimelineCategoryPerformedAct || ev.ReferenceType != TimelineReferencePerformedAct {
		t.Fatalf("identity: %+v", ev)
	}
	if ev.ReferenceID == nil || *ev.ReferenceID != act.ID {
		t.Fatal("ReferenceID")
	}
	if ev.PatientID != p.ID || ev.MedicalRecordID != rec.ID {
		t.Fatalf("patient/mr: %+v", ev)
	}
	if !ev.EventDate.Equal(act.PerformedAt) {
		t.Fatalf("EventDate=%v PerformedAt=%v", ev.EventDate, act.PerformedAt)
	}
	if ev.CreatedBy != actor {
		t.Fatalf("CreatedBy=%d want %d", ev.CreatedBy, actor)
	}
	if ev.Title != "Acte réalisé" {
		t.Fatalf("title=%q", ev.Title)
	}
	if ev.Description != act.ActLabel {
		t.Fatalf("description snapshot=%q want %q", ev.Description, act.ActLabel)
	}
}

// P102 — retrospective PerformedAt becomes EventDate (≠ CreatedAt).
func TestLot28EB3_P102_RetrospectiveEventDate(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "102")
	historical := time.Date(2024, 6, 15, 9, 30, 0, 0, time.UTC)
	act, err := NewService(db).Create(CreateRequest{
		PatientID: p.ID, ActCatalogEntryID: cat.ID,
		PerformedAt: historical.Format(time.RFC3339),
	}, 7)
	if err != nil {
		t.Fatal(err)
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	if !ev.EventDate.Equal(historical) || !ev.EventDate.Equal(act.PerformedAt) {
		t.Fatalf("EventDate=%v want historical %v", ev.EventDate, historical)
	}
	// Fixture guarantees distinction: historical vs persistence time.
	if ev.CreatedAt.Equal(ev.EventDate) {
		t.Fatal("P102: EventDate must not equal CreatedAt for retrospective act")
	}
	if !ev.CreatedAt.After(ev.EventDate) {
		t.Fatalf("CreatedAt=%v should be after EventDate=%v", ev.CreatedAt, ev.EventDate)
	}
}

// P103 — void adds performed_act_voided; performed event untouched.
func TestLot28EB3_P103_VoidTimeline(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "103")
	s := NewService(db)
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 3)
	if err != nil {
		t.Fatal(err)
	}
	before := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	voided, err := s.Void(act.ID, VoidRequest{Reason: "saisie erronée"}, 11)
	if err != nil {
		t.Fatal(err)
	}
	if voided.Status != StatusVoided || voided.VoidedAt == nil {
		t.Fatalf("voided=%+v", voided)
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActPerformed, act.ID) != 1 {
		t.Fatal("performed event count changed")
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActVoided, act.ID) != 1 {
		t.Fatal("expected one void event")
	}
	voidEv := b3FindEvent(t, db, TimelineEventPerformedActVoided, act.ID)
	if !voidEv.EventDate.Equal(*voided.VoidedAt) {
		t.Fatalf("void EventDate=%v VoidedAt=%v", voidEv.EventDate, *voided.VoidedAt)
	}
	if voidEv.Title != "Acte annulé" || voidEv.Description != "" {
		t.Fatalf("void payload: %+v", voidEv)
	}
	if strings.Contains(strings.ToLower(voidEv.Title+voidEv.Description), "saisie") {
		t.Fatal("VoidReason leaked into timeline")
	}
	after := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	if after.ID != before.ID || after.Title != before.Title || after.Description != before.Description ||
		!after.EventDate.Equal(before.EventDate) || after.CreatedBy != before.CreatedBy {
		t.Fatal("performed event mutated after void")
	}
}

// P104 — void without performed_acts.void → auth failure, no void event.
func TestLot28EB3_P104_UnauthorizedVoid(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "104")
	act, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 88, "staff", []string{"performed_acts.read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(db)))

	body, _ := json.Marshal(VoidRequest{Reason: "tentative"})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts/"+strconv.FormatUint(uint64(act.ID), 10)+"/void", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	reloaded, err := NewService(db).GetByID(act.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != StatusPerformed || reloaded.VoidedAt != nil {
		t.Fatalf("PA mutated: %+v", reloaded)
	}
	if b3CountEvents(t, db, "event_type = ?", TimelineEventPerformedActVoided) != 0 {
		t.Fatal("void event created without permission")
	}
}

// P105 — timeline failure inside create TX rolls back PA.
func TestLot28EB3_P105_CreateTimelineFailureRollback(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "105")
	if err := db.Migrator().DropTable(&medical_records.MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	_, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 5)
	if err == nil {
		t.Fatal("expected create failure")
	}
	var n int64
	if err := db.Model(&Act{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("PA committed despite timeline failure: %d", n)
	}
	// Recreate table so count works; no PA events should exist from failed TX.
	if err := db.AutoMigrate(&medical_records.MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	if b3CountEvents(t, db, "event_type LIKE ?", "performed_act_%") != 0 {
		t.Fatal("timeline events committed")
	}
}

// P106 — timeline failure inside void TX rolls back void transition.
func TestLot28EB3_P106_VoidTimelineFailureRollback(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "106")
	s := NewService(db)
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrator().DropTable(&medical_records.MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.Void(act.ID, VoidRequest{Reason: "échec timeline"}, 9)
	if err == nil {
		t.Fatal("expected void failure")
	}
	reloaded, err := s.GetByID(act.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != StatusPerformed || reloaded.VoidedAt != nil || reloaded.VoidedBy != nil {
		t.Fatalf("void partially committed: %+v", reloaded)
	}
	if err := db.AutoMigrate(&medical_records.MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	if b3CountEvents(t, db, "event_type = ?", TimelineEventPerformedActVoided) != 0 {
		t.Fatal("void event committed")
	}
}

// P107 — genuine concurrent void → one success, one void event (Postgres).
func TestLot28EB3_P107_ConcurrentVoid(t *testing.T) {
	db := performedActsDB(t)
	p, cat := seedPG(t, db)
	s := NewService(db)
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}

	var okCount, failCount atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(actor uint) {
			defer wg.Done()
			<-start
			_, err := s.Void(act.ID, VoidRequest{Reason: "concurrent void"}, actor)
			if err == nil {
				okCount.Add(1)
			} else {
				failCount.Add(1)
			}
		}(uint(100 + i))
	}
	close(start)
	wg.Wait()

	if okCount.Load() != 1 {
		t.Fatalf("successful voids=%d want 1 (failures=%d)", okCount.Load(), failCount.Load())
	}
	if failCount.Load() < 1 {
		t.Fatal("expected competing voids to fail")
	}
	reloaded, err := s.GetByID(act.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Status != StatusVoided {
		t.Fatalf("status=%s", reloaded.Status)
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActVoided, act.ID) != 1 {
		t.Fatal("duplicate void timeline events")
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActPerformed, act.ID) != 1 {
		t.Fatal("performed event count wrong")
	}
}

// P108 — AUTH-B without performed_acts.read → generic occurrence.
func TestLot28EB3_P108_AuthBUnauthorizedDetail(t *testing.T) {
	ref := uint(501)
	src := medical_records.MedicalTimelineEvent{
		ID: 1, MedicalRecordID: 9, PatientID: 3,
		EventType: TimelineEventPerformedActPerformed, Category: TimelineCategoryPerformedAct,
		Title: "Acte réalisé", Description: "Suture complexe",
		ReferenceType: TimelineReferencePerformedAct, ReferenceID: &ref, Severity: "info",
		EventDate: time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC), CreatedBy: 4,
	}
	out := medical_records.ProjectTimelineEventsForCaller([]medical_records.MedicalTimelineEvent{src}, []string{"medical_records.read"})
	if len(out) != 1 {
		t.Fatal("expected one projected event")
	}
	got := out[0]
	if got.Title != "Acte réalisé" || got.Category != TimelineCategoryPerformedAct {
		t.Fatalf("generic occurrence: %+v", got)
	}
	if got.Description != "" || got.ReferenceType != "" || got.ReferenceID != nil {
		t.Fatalf("detail leaked: %+v", got)
	}
	if got.EventType != TimelineEventPerformedActPerformed {
		t.Fatalf("EventType should remain for FE labels: %s", got.EventType)
	}
	if !got.EventDate.Equal(src.EventDate) {
		t.Fatal("EventDate must remain visible")
	}
	// Source immutability
	if src.Description != "Suture complexe" || src.ReferenceID == nil || *src.ReferenceID != 501 {
		t.Fatal("source mutated")
	}

	voidSrc := src
	voidSrc.EventType = TimelineEventPerformedActVoided
	voidSrc.Title = "Acte annulé"
	voidSrc.Description = ""
	voidOut := medical_records.ProjectTimelineEventsForCaller([]medical_records.MedicalTimelineEvent{voidSrc}, []string{"medical_records.read"})
	if voidOut[0].Title != "Acte annulé" || voidOut[0].ReferenceType != "" || voidOut[0].ReferenceID != nil {
		t.Fatalf("void redaction: %+v", voidOut[0])
	}
}

// P109 — AUTH-B with performed_acts.read → safe detail + refs.
func TestLot28EB3_P109_AuthBAuthorizedDetail(t *testing.T) {
	ref := uint(502)
	src := medical_records.MedicalTimelineEvent{
		EventType: TimelineEventPerformedActPerformed, Category: TimelineCategoryPerformedAct,
		Title: "Acte réalisé", Description: "Suture",
		ReferenceType: TimelineReferencePerformedAct, ReferenceID: &ref, Severity: "info",
		EventDate: time.Now().UTC(), CreatedBy: 4,
	}
	out := medical_records.ProjectTimelineEventsForCaller(
		[]medical_records.MedicalTimelineEvent{src},
		[]string{"medical_records.read", "performed_acts.read"},
	)
	got := out[0]
	if got.Description != "Suture" || got.ReferenceType != TimelineReferencePerformedAct ||
		got.ReferenceID == nil || *got.ReferenceID != 502 {
		t.Fatalf("authorized detail: %+v", got)
	}
	payload := got.Title + got.Description + got.ReferenceType
	if b3PayloadContainsForbidden(payload) {
		t.Fatalf("forbidden content in authorized projection: %q", payload)
	}
}

// P110 — financial/insurance fields never in timeline payload.
func TestLot28EB3_P110_FinancialExclusion(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "110")
	// Catalog has price 15000 — must not appear in timeline description/title.
	act, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 2)
	if err != nil {
		t.Fatal(err)
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	blob, _ := json.Marshal(ev)
	s := string(blob)
	if strings.Contains(s, "15000") || strings.Contains(strings.ToLower(s), "xof") {
		t.Fatalf("price/currency leaked: %s", s)
	}
	if b3PayloadContainsForbidden(ev.Title + ev.Description + ev.ReferenceType + ev.Category) {
		t.Fatalf("forbidden semantic content: %+v", ev)
	}
	if act.BasePrice != 15000 {
		t.Fatal("fixture price missing on PA domain row")
	}
}

// P111 — pre-B3 PA without timeline remains readable; no lazy fabricate.
func TestLot28EB3_P111_HistoricalRowNoBackfill(t *testing.T) {
	db := b3DB(t)
	p, cat, rec := b3Seed(t, db, "111")
	legacy := Act{
		PatientID: p.ID, ActCatalogEntryID: cat.ID, ActCode: cat.Code, ActLabel: cat.Label,
		ActCategory: cat.Category, BasePrice: cat.BasePrice, Currency: cat.Currency,
		Billable: true, InsuranceEligible: true, Quantity: 1,
		PerformedAt: time.Now().Add(-48 * time.Hour), PerformedBy: 1, Status: StatusPerformed,
		CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	got, err := NewService(db).GetByID(legacy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusPerformed {
		t.Fatal("legacy PA unreadable")
	}
	before := b3CountEvents(t, db, "medical_record_id = ?", rec.ID)
	events, err := medical_records.NewService(medical_records.NewRepository(db)).
		ListTimelineEvents(rec.ID, []string{"medical_records.read", "performed_acts.read"})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.EventType == TimelineEventPerformedActPerformed || e.EventType == TimelineEventPerformedActVoided {
			t.Fatalf("lazy fabricated PA event: %+v", e)
		}
	}
	after := b3CountEvents(t, db, "medical_record_id = ?", rec.ID)
	if after != before {
		t.Fatal("timeline read created events")
	}
}

// P113 — voided PA remains list-visible; timeline shows performed + void.
func TestLot28EB3_P113_VoidedVisibility(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "113")
	s := NewService(db)
	act, err := s.Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Void(act.ID, VoidRequest{Reason: "annulation"}, 2); err != nil {
		t.Fatal(err)
	}
	page, err := s.List(ListFilter{PatientID: p.ID, Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range page.Data {
		if row.ID == act.ID && row.Status == StatusVoided {
			found = true
		}
	}
	if !found {
		t.Fatal("voided PA missing from list (product semantics)")
	}
	if b3CountEvents(t, db, "reference_id = ? AND event_type = ?", act.ID, TimelineEventPerformedActPerformed) != 1 {
		t.Fatal("missing performed chronology")
	}
	if b3CountEvents(t, db, "reference_id = ? AND event_type = ?", act.ID, TimelineEventPerformedActVoided) != 1 {
		t.Fatal("missing void chronology")
	}
}

// P114 — dual chronology: source-domain event coexists with PA event.
func TestLot28EB3_P114_DualChronology(t *testing.T) {
	db := b3DB(t)
	p, cat, rec := b3Seed(t, db, "114")
	labRef := uint(77)
	lab := medical_records.MedicalTimelineEvent{
		MedicalRecordID: rec.ID, PatientID: p.ID,
		EventType: "lab_result_validated", Category: "laboratory",
		Title: "Résultat laboratoire validé", Description: "NFS",
		ReferenceType: "LaboratoryOrder", ReferenceID: &labRef,
		Severity: "info", EventDate: time.Now().UTC(), CreatedBy: 1,
	}
	if err := db.Create(&lab).Error; err != nil {
		t.Fatal(err)
	}
	act, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if b3CountEvents(t, db, "event_type = ?", "lab_result_validated") != 1 {
		t.Fatal("lab event removed")
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActPerformed, act.ID) != 1 {
		t.Fatal("PA event missing")
	}
}

// P115 — CreatedBy is authenticated mutation actor (not client-forged).
func TestLot28EB3_P115_ActorProvenance(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "115")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 77, "staff", []string{"performed_acts.read", "performed_acts.create", "performed_acts.void"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(db)))

	body, _ := json.Marshal(map[string]any{
		"patientId": p.ID, "actCatalogEntryId": cat.ID, "quantity": 1,
		"createdBy": 9999, "performedBy": 9999,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/performed-acts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var created Act
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, created.ID)
	if ev.CreatedBy != 77 {
		t.Fatalf("create timeline CreatedBy=%d want 77", ev.CreatedBy)
	}
	if created.CreatedBy != 77 || created.PerformedBy != 77 {
		t.Fatalf("PA actor forged: createdBy=%d performedBy=%d", created.CreatedBy, created.PerformedBy)
	}

	vBody, _ := json.Marshal(VoidRequest{Reason: "correction"})
	vReq := httptest.NewRequest(http.MethodPost, "/api/performed-acts/"+strconv.FormatUint(uint64(created.ID), 10)+"/void", bytes.NewReader(vBody))
	vReq.Header.Set("Content-Type", "application/json")
	vw := httptest.NewRecorder()
	r.ServeHTTP(vw, vReq)
	if vw.Code != http.StatusOK {
		t.Fatalf("void status=%d", vw.Code)
	}
	voidEv := b3FindEvent(t, db, TimelineEventPerformedActVoided, created.ID)
	if voidEv.CreatedBy != 77 {
		t.Fatalf("void timeline CreatedBy=%d want 77", voidEv.CreatedBy)
	}
}

// Producer create uses caller TX (no nested Transaction) + one timeline event.
func TestLot28EB3_ProducerCreateSameTXTimeline(t *testing.T) {
	db := b3DB(t)
	p, cat, rec := b3Seed(t, db, "prod")
	if err := db.Create(&ProducerMap{
		SourceType: SourceConsultation, ClinicalKey: ConsultationClinicalKey,
		ActCatalogEntryID: cat.ID, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	s := NewService(db)
	var act *Act
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		act, err = s.EnsureFromProducer(tx, ProducerCreateRequest{
			SourceType: SourceConsultation, SourceID: 901, PatientID: p.ID, ActorID: 55,
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	if ev.MedicalRecordID != rec.ID || ev.CreatedBy != 55 {
		t.Fatalf("producer timeline: %+v", ev)
	}
	// Idempotent second ensure: no duplicate event
	again, err := s.EnsureFromProducer(db, ProducerCreateRequest{
		SourceType: SourceConsultation, SourceID: 901, PatientID: p.ID, ActorID: 55,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != act.ID {
		t.Fatal("duplicate PA")
	}
	if b3CountEvents(t, db, "event_type = ? AND reference_id = ?", TimelineEventPerformedActPerformed, act.ID) != 1 {
		t.Fatal("duplicate timeline on idempotent accept")
	}
}

// Projection copy-safety: unauthorized then authorized must not poison.
func TestLot28EB3_ProjectionCopySafety(t *testing.T) {
	ref := uint(777)
	src := []medical_records.MedicalTimelineEvent{{
		EventType: TimelineEventPerformedActPerformed, Category: TimelineCategoryPerformedAct,
		Title: "Acte réalisé", Description: "Label secret",
		ReferenceType: TimelineReferencePerformedAct, ReferenceID: &ref, Severity: "info",
	}}
	redacted := medical_records.ProjectTimelineEventsForCaller(src, []string{"medical_records.read"})
	if redacted[0].Description != "" || redacted[0].ReferenceID != nil {
		t.Fatal("redaction failed")
	}
	if src[0].Description != "Label secret" || src[0].ReferenceID == nil {
		t.Fatal("source poisoned by unauthorized projection")
	}
	full := medical_records.ProjectTimelineEventsForCaller(src, []string{"medical_records.read", "performed_acts.read"})
	if full[0].Description != "Label secret" || full[0].ReferenceID == nil || *full[0].ReferenceID != 777 {
		t.Fatalf("authorized projection poisoned: %+v", full[0])
	}
}

// Missing MedicalRecord fails closed on create.
func TestLot28EB3_MissingMedicalRecordFailsClosed(t *testing.T) {
	db := b3DB(t)
	p := patients.Patient{CodePatient: "B3-NOMR", NumeroDossier: "B3-NOMR", Nom: "NoMR"}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	cat := act_catalog.Entry{
		Code: "NOMR", Label: "No MR", Category: "OTHER", BasePrice: 100, Currency: "XOF",
		Billable: true, IsActive: true, CreatedBy: 1, UpdatedBy: 1,
	}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatal(err)
	}
	_, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err == nil {
		t.Fatal("expected missing MR failure")
	}
	var n int64
	db.Model(&Act{}).Count(&n)
	if n != 0 {
		t.Fatal("PA committed without MR")
	}
}

// ActLabel catalog rename must not rewrite stored timeline description.
func TestLot28EB3_ActLabelSnapshotImmutable(t *testing.T) {
	db := b3DB(t)
	p, cat, _ := b3Seed(t, db, "snap")
	act, err := NewService(db).Create(CreateRequest{PatientID: p.ID, ActCatalogEntryID: cat.ID}, 1)
	if err != nil {
		t.Fatal(err)
	}
	original := act.ActLabel
	cat.Label = "Label renommé après coup"
	if err := db.Save(&cat).Error; err != nil {
		t.Fatal(err)
	}
	ev := b3FindEvent(t, db, TimelineEventPerformedActPerformed, act.ID)
	if ev.Description != original {
		t.Fatalf("timeline description mutated to live catalog: %q", ev.Description)
	}
}
