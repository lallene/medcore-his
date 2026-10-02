package medical_records

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func b2bDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:lot28e_b2b_"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&MedicalRecord{}, &MedicalTimelineEvent{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func b2bPtr(u uint) *uint { return &u }

func b2bSeedMixedTimeline(t *testing.T, db *gorm.DB) MedicalRecord {
	t.Helper()
	record := MedicalRecord{PatientID: 2802}
	if err := db.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	dept := uint(7)
	invoiceRef := uint(9001)
	pecRef := uint(8001)
	docRef := uint(7001)
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	events := []MedicalTimelineEvent{
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "consultation_created", Category: "clinical",
			Title: "Consultation créée", Description: "Médecine générale",
			Severity: "info", EventDate: base, CreatedBy: 1, DepartmentID: &dept,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "invoice_issued", Category: "billing",
			Title: "Facture émise", Description: "FAC-2026-0042",
			ReferenceType: "billing_invoice", ReferenceID: &invoiceRef,
			Severity: "info", EventDate: base.Add(1 * time.Hour), CreatedBy: 2, DepartmentID: &dept,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "payment_received", Category: "billing",
			Title: "Paiement reçu", Description: "FAC-2026-0042",
			ReferenceType: "billing_invoice", ReferenceID: &invoiceRef,
			Severity: "info", EventDate: base.Add(2 * time.Hour), CreatedBy: 2,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "payment_received", Category: "billing",
			Title: "Paiement reçu", Description: "FAC-2026-0042",
			ReferenceType: "billing_invoice", ReferenceID: &invoiceRef,
			Severity: "info", EventDate: base.Add(3 * time.Hour), CreatedBy: 2,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "insurance_authorization_approved", Category: "insurance",
			Title: "Décision assureur enregistrée", Description: "PEC-2026-0017",
			ReferenceType: "insurance_authorization", ReferenceID: &pecRef,
			Severity: "info", EventDate: base.Add(4 * time.Hour), CreatedBy: 3, DepartmentID: &dept,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "insurance_authorization_rejected", Category: "insurance",
			Title: "Décision assureur enregistrée", Description: "PEC-2026-0099",
			ReferenceType: "insurance_authorization", ReferenceID: b2bPtr(8002),
			Severity: "warning", EventDate: base.Add(5 * time.Hour), CreatedBy: 3,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: TimelineEventDocumentAdded, Category: TimelineCategoryDocument,
			Title: "Document médical ajouté", Description: "Compte rendu",
			ReferenceType: TimelineReferenceMedicalDocument, ReferenceID: &docRef,
			Severity: "info", EventDate: base.Add(6 * time.Hour), CreatedBy: 1,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: TimelineEventDocumentArchived, Category: TimelineCategoryDocument,
			Title: "Document médical archivé", Description: "Ancien scan",
			ReferenceType: TimelineReferenceMedicalDocument, ReferenceID: b2bPtr(7002),
			Severity: "info", EventDate: base.Add(7 * time.Hour), CreatedBy: 1,
		},
		{
			MedicalRecordID: record.ID, PatientID: record.PatientID,
			EventType: "common_medical_record_updated", Category: "clinical",
			Title: "Dossier médical mis à jour", Description: "Profil commun",
			Severity: "info", EventDate: base.Add(8 * time.Hour), CreatedBy: 1,
		},
	}
	for i := range events {
		if err := db.Create(&events[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	return record
}

func b2bAssertRedactedBilling(t *testing.T, e MedicalTimelineEvent) {
	t.Helper()
	if e.EventType != TimelineEventBillingOccurrence {
		t.Fatalf("billing EventType: got %q want %q", e.EventType, TimelineEventBillingOccurrence)
	}
	if e.Category != timelineCategoryBilling {
		t.Fatalf("billing Category: got %q", e.Category)
	}
	if e.Title != timelineTitleBillingOccurrence {
		t.Fatalf("billing Title: got %q", e.Title)
	}
	if e.Description != "" {
		t.Fatalf("billing Description must be empty, got %q", e.Description)
	}
	if e.ReferenceType != "" || e.ReferenceID != nil {
		t.Fatalf("billing refs must be cleared: type=%q id=%v", e.ReferenceType, e.ReferenceID)
	}
	if e.Severity != "info" {
		t.Fatalf("billing Severity must be info, got %q", e.Severity)
	}
	blob, _ := json.Marshal(e)
	s := string(blob)
	for _, leak := range []string{"FAC-", "invoice_issued", "payment_received", "invoice_paid", "invoice_cancelled", "billing_invoice"} {
		if strings.Contains(s, leak) {
			t.Fatalf("redacted billing leaked %q in %s", leak, s)
		}
	}
}

func b2bAssertRedactedInsurance(t *testing.T, e MedicalTimelineEvent) {
	t.Helper()
	if e.EventType != TimelineEventInsuranceOccurrence {
		t.Fatalf("insurance EventType: got %q want %q", e.EventType, TimelineEventInsuranceOccurrence)
	}
	if e.Category != timelineCategoryInsurance {
		t.Fatalf("insurance Category: got %q", e.Category)
	}
	if e.Title != timelineTitleInsuranceOccurrence {
		t.Fatalf("insurance Title: got %q", e.Title)
	}
	if e.Description != "" {
		t.Fatalf("insurance Description must be empty, got %q", e.Description)
	}
	if e.ReferenceType != "" || e.ReferenceID != nil {
		t.Fatalf("insurance refs must be cleared: type=%q id=%v", e.ReferenceType, e.ReferenceID)
	}
	if e.Severity != "info" {
		t.Fatalf("insurance Severity must be info, got %q", e.Severity)
	}
	blob, _ := json.Marshal(e)
	s := string(blob)
	for _, leak := range []string{
		"PEC-", "insurance_authorization_approved", "insurance_authorization_rejected",
		"insurance_authorization_created", "insurance_authorization_submitted",
		"insurance_authorization_partially_approved", "insurance_authorization_cancelled",
		"insurance_authorization_act_linked", `"insurance_authorization"`,
	} {
		if strings.Contains(s, leak) {
			t.Fatalf("redacted insurance leaked %q in %s", leak, s)
		}
	}
}

func b2bAssertFullBilling(t *testing.T, e MedicalTimelineEvent) {
	t.Helper()
	if e.EventType != "invoice_issued" && e.EventType != "payment_received" &&
		e.EventType != "invoice_paid" && e.EventType != "invoice_cancelled" {
		t.Fatalf("expected full billing event type, got %q", e.EventType)
	}
	if e.Description == "" || e.ReferenceID == nil || e.ReferenceType != "billing_invoice" {
		t.Fatalf("authorized billing detail missing: %+v", e)
	}
}

func b2bAssertFullInsurance(t *testing.T, e MedicalTimelineEvent) {
	t.Helper()
	if !strings.HasPrefix(e.EventType, "insurance_authorization_") {
		t.Fatalf("expected full insurance event type, got %q", e.EventType)
	}
	if e.Description == "" || e.ReferenceID == nil || e.ReferenceType != "insurance_authorization" {
		t.Fatalf("authorized insurance detail missing: %+v", e)
	}
}

func TestLot28EB2B_A01_TimelineForbiddenWithoutMedicalRecordsRead(t *testing.T) {
	db := b2bDB(t)
	_ = b2bSeedMixedTimeline(t, db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 7, "staff", []string{"billing.read", "insurance.authorization.read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/medical-records/1/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("A01: expected 403 without medical_records.read, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestLot28EB2B_A02_A20_AuthorityMatrix(t *testing.T) {
	db := b2bDB(t)
	record := b2bSeedMixedTimeline(t, db)
	svc := NewService(NewRepository(db))

	// Snapshot stored rows before any projection.
	var storedBefore []MedicalTimelineEvent
	if err := db.Where("medical_record_id = ?", record.ID).Order("event_date ASC, id ASC").Find(&storedBefore).Error; err != nil {
		t.Fatal(err)
	}
	if len(storedBefore) < 9 {
		t.Fatalf("seed expected >=9 events, got %d", len(storedBefore))
	}

	mrOnly := []string{"medical_records.read"}
	mrBilling := []string{"medical_records.read", "billing.read"}
	mrInsurance := []string{"medical_records.read", "insurance.authorization.read"}
	mrBoth := []string{"medical_records.read", "billing.read", "insurance.authorization.read"}

	// A02 / A03 / A09–A13 / A16–A20 — MR read only
	redacted, err := svc.ListTimelineEvents(record.ID, mrOnly)
	if err != nil {
		t.Fatal(err)
	}
	if len(redacted) != len(storedBefore) {
		t.Fatalf("A16: occurrence count must match stored (%d vs %d)", len(redacted), len(storedBefore))
	}

	var billingCount, insuranceCount, paymentCount int
	var sawDocAdded, sawDocArchived, sawCMR, sawClinical bool
	for i, e := range redacted {
		// Ordering preserved relative to stored snapshot (desc by repo — compare IDs sequence).
		_ = i
		switch classifyTimelineEventDomain(storedBefore[findStoredIndex(storedBefore, e.ID)]) {
		case timelineDomainFinancial:
			billingCount++
			b2bAssertRedactedBilling(t, e)
			if e.EventType == TimelineEventBillingOccurrence {
				// map back via stored for payment_received multiplicity check
			}
		case timelineDomainInsurance:
			insuranceCount++
			b2bAssertRedactedInsurance(t, e)
		default:
			stored := storedBefore[findStoredIndex(storedBefore, e.ID)]
			if e.EventType != stored.EventType || e.Title != stored.Title || e.Description != stored.Description {
				t.Fatalf("clinical/document must be unchanged: got %+v want %+v", e, stored)
			}
			switch e.EventType {
			case TimelineEventDocumentAdded:
				sawDocAdded = true
			case TimelineEventDocumentArchived:
				sawDocArchived = true
			case "common_medical_record_updated":
				sawCMR = true
			case "consultation_created":
				sawClinical = true
			}
		}
	}
	for _, s := range storedBefore {
		if s.EventType == "payment_received" {
			paymentCount++
		}
	}
	if billingCount < 2 {
		t.Fatalf("A02: expected billing occurrences visible, got %d", billingCount)
	}
	if insuranceCount < 1 {
		t.Fatalf("A03: expected insurance occurrences visible, got %d", insuranceCount)
	}
	if paymentCount != 2 {
		t.Fatalf("A17: expected 2 payment_received stored, got %d", paymentCount)
	}
	paymentsVisible := 0
	for _, e := range redacted {
		st := storedBefore[findStoredIndex(storedBefore, e.ID)]
		if st.EventType == "payment_received" {
			paymentsVisible++
			b2bAssertRedactedBilling(t, e)
		}
	}
	if paymentsVisible != 2 {
		t.Fatalf("A17: both payment occurrences must remain visible, got %d", paymentsVisible)
	}
	if !sawDocAdded {
		t.Fatal("A18: document_added must remain unchanged")
	}
	if !sawDocArchived {
		t.Fatal("A19: document_archived must remain unchanged")
	}
	if !sawCMR {
		t.Fatal("A20: common_medical_record_updated must remain unchanged")
	}
	if !sawClinical {
		t.Fatal("clinical event must remain unchanged")
	}

	// A16 ordering: projected IDs follow same order as service/repo list
	repoOrder, err := NewRepository(db).ListTimelineEvents(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(repoOrder) != len(redacted) {
		t.Fatal("A16 length mismatch")
	}
	for i := range repoOrder {
		if repoOrder[i].ID != redacted[i].ID {
			t.Fatalf("A16 ordering changed at %d: stored=%d projected=%d", i, repoOrder[i].ID, redacted[i].ID)
		}
	}

	// A04 — MR + billing
	withBilling, err := svc.ListTimelineEvents(record.ID, mrBilling)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range withBilling {
		st := storedBefore[findStoredIndex(storedBefore, e.ID)]
		switch classifyTimelineEventDomain(st) {
		case timelineDomainFinancial:
			b2bAssertFullBilling(t, e)
		case timelineDomainInsurance:
			b2bAssertRedactedInsurance(t, e)
		}
	}

	// A05 — MR + insurance
	withIns, err := svc.ListTimelineEvents(record.ID, mrInsurance)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range withIns {
		st := storedBefore[findStoredIndex(storedBefore, e.ID)]
		switch classifyTimelineEventDomain(st) {
		case timelineDomainFinancial:
			b2bAssertRedactedBilling(t, e)
		case timelineDomainInsurance:
			b2bAssertFullInsurance(t, e)
		}
	}

	// A06 covered by A04 loop; A07 by A05 loop.

	// A08 — both
	full, err := svc.ListTimelineEvents(record.ID, mrBoth)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range full {
		st := storedBefore[findStoredIndex(storedBefore, e.ID)]
		switch classifyTimelineEventDomain(st) {
		case timelineDomainFinancial:
			b2bAssertFullBilling(t, e)
		case timelineDomainInsurance:
			b2bAssertFullInsurance(t, e)
		default:
			if e.EventType != st.EventType || e.Description != st.Description {
				t.Fatalf("A08 clinical changed: %+v", e)
			}
		}
	}

	// A14 — stored DB unchanged after redacted reads
	var storedAfter []MedicalTimelineEvent
	if err := db.Where("medical_record_id = ?", record.ID).Order("id ASC").Find(&storedAfter).Error; err != nil {
		t.Fatal(err)
	}
	if len(storedAfter) != len(storedBefore) {
		t.Fatal("A14: stored count changed")
	}
	byID := map[uint]MedicalTimelineEvent{}
	for _, s := range storedBefore {
		byID[s.ID] = s
	}
	for _, s := range storedAfter {
		before := byID[s.ID]
		if s.EventType != before.EventType || s.Title != before.Title || s.Description != before.Description ||
			s.ReferenceType != before.ReferenceType || s.Severity != before.Severity {
			t.Fatalf("A14: stored row mutated: before=%+v after=%+v", before, s)
		}
		if (s.ReferenceID == nil) != (before.ReferenceID == nil) {
			t.Fatal("A14: ReferenceID presence changed")
		}
		if s.ReferenceID != nil && before.ReferenceID != nil && *s.ReferenceID != *before.ReferenceID {
			t.Fatal("A14: ReferenceID value changed")
		}
	}

	// A15 — unauthorized then authorized: no in-memory mutation leak
	_, err = svc.ListTimelineEvents(record.ID, mrOnly)
	if err != nil {
		t.Fatal(err)
	}
	afterAuth, err := svc.ListTimelineEvents(record.ID, mrBoth)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range afterAuth {
		st := storedBefore[findStoredIndex(storedBefore, e.ID)]
		switch classifyTimelineEventDomain(st) {
		case timelineDomainFinancial:
			b2bAssertFullBilling(t, e)
		case timelineDomainInsurance:
			b2bAssertFullInsurance(t, e)
		}
	}
}

func findStoredIndex(stored []MedicalTimelineEvent, id uint) int {
	for i, s := range stored {
		if s.ID == id {
			return i
		}
	}
	return 0
}

func TestLot28EB2B_HTTP_ClinicalOnlyRoleReceivesRedactedJSON(t *testing.T) {
	db := b2bDB(t)
	record := b2bSeedMixedTimeline(t, db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Reachable clinical-like principal: MR yes, billing/insurance no.
		rbac.SetUser(c, 11, "staff", []string{"medical_records.read", "patients:read", "consultations.read"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/medical-records/"+itoa(record.ID)+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var events []MedicalTimelineEvent
	if err := json.Unmarshal(w.Body.Bytes(), &events); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if strings.Contains(body, "FAC-") || strings.Contains(body, "PEC-") {
		t.Fatalf("HTTP response leaked identifiers: %s", body)
	}
	if strings.Contains(body, "invoice_issued") || strings.Contains(body, "insurance_authorization_approved") {
		t.Fatalf("HTTP response leaked protected EventType: %s", body)
	}
	sawBilling, sawInsurance := false, false
	for _, e := range events {
		if e.EventType == TimelineEventBillingOccurrence {
			sawBilling = true
			b2bAssertRedactedBilling(t, e)
		}
		if e.EventType == TimelineEventInsuranceOccurrence {
			sawInsurance = true
			b2bAssertRedactedInsurance(t, e)
		}
	}
	if !sawBilling || !sawInsurance {
		t.Fatalf("expected redacted occurrences visible (billing=%v insurance=%v)", sawBilling, sawInsurance)
	}
}

func TestLot28EB2B_HTTP_AuthorizedFullDetail(t *testing.T) {
	db := b2bDB(t)
	record := b2bSeedMixedTimeline(t, db)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, 12, "staff", []string{
			"medical_records.read", "billing.read", "insurance.authorization.read",
		})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutes(api, NewHandler(NewService(NewRepository(db))))

	req := httptest.NewRequest(http.MethodGet, "/api/medical-records/"+itoa(record.ID)+"/timeline", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "FAC-2026-0042") || !strings.Contains(body, "PEC-2026-0017") {
		t.Fatalf("authorized response missing identifiers: %s", body)
	}
	if !strings.Contains(body, "invoice_issued") || !strings.Contains(body, "insurance_authorization_approved") {
		t.Fatalf("authorized response missing event types: %s", body)
	}
}

func TestLot28EB2B_ProjectionDoesNotMutateSourceSlice(t *testing.T) {
	invoiceRef := uint(42)
	src := []MedicalTimelineEvent{{
		ID: 1, EventType: "invoice_paid", Category: "billing",
		Title: "Facture soldée", Description: "FAC-1",
		ReferenceType: "billing_invoice", ReferenceID: &invoiceRef, Severity: "info",
	}}
	out := ProjectTimelineEventsForCaller(src, []string{"medical_records.read"})
	b2bAssertRedactedBilling(t, out[0])
	if src[0].EventType != "invoice_paid" || src[0].Description != "FAC-1" || src[0].ReferenceID == nil {
		t.Fatalf("source slice mutated: %+v", src[0])
	}
	out2 := ProjectTimelineEventsForCaller(src, []string{"medical_records.read", "billing.read"})
	b2bAssertFullBilling(t, out2[0])
}

func TestLot28EB2B_InventoryCoverage(t *testing.T) {
	// Every runtime financial/insurance type must classify and redact.
	for et := range financialTimelineEventTypes {
		e := MedicalTimelineEvent{EventType: et, Category: "billing", Title: "x", Description: "FAC-X", ReferenceType: "billing_invoice", ReferenceID: b2bPtr(1), Severity: "warning"}
		out := ProjectTimelineEventsForCaller([]MedicalTimelineEvent{e}, nil)
		b2bAssertRedactedBilling(t, out[0])
	}
	for et := range insuranceTimelineEventTypes {
		e := MedicalTimelineEvent{EventType: et, Category: "insurance", Title: "x", Description: "PEC-X", ReferenceType: "insurance_authorization", ReferenceID: b2bPtr(1), Severity: "warning"}
		out := ProjectTimelineEventsForCaller([]MedicalTimelineEvent{e}, nil)
		b2bAssertRedactedInsurance(t, out[0])
	}
}

func TestLot28EB2B_DepartmentPreservedOnRedaction(t *testing.T) {
	dept := uint(3)
	e := MedicalTimelineEvent{
		EventType: "invoice_issued", Category: "billing", Title: "Facture émise",
		Description: "FAC-9", ReferenceType: "billing_invoice", ReferenceID: b2bPtr(9),
		Severity: "info", DepartmentID: &dept,
	}
	out := ProjectTimelineEventsForCaller([]MedicalTimelineEvent{e}, []string{"medical_records.read"})
	if out[0].DepartmentID == nil || *out[0].DepartmentID != 3 {
		t.Fatalf("DepartmentID should remain (occurrence context): %+v", out[0].DepartmentID)
	}
}

func itoa(u uint) string {
	if u == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}
