package consultations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"gorm.io/gorm"
)

// LOT28E-B2-C1 — Policy A: consultation_created post-commit best-effort.
//
// Residual (documented, not fixed): MODE 3 / concurrent direct POST can still
// create multiple consultation rows. C1 only prevents timeline failure from
// becoming HTTP 500 after a successful consultation commit.

type b2c1RecordingHandler struct {
	records []slog.Record
	attrs   [][]slog.Attr
}

func (h *b2c1RecordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *b2c1RecordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	var as []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		as = append(as, a)
		return true
	})
	h.attrs = append(h.attrs, as)
	return nil
}
func (h *b2c1RecordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *b2c1RecordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *b2c1RecordingHandler) attrMap(i int) map[string]string {
	out := map[string]string{}
	if i < 0 || i >= len(h.attrs) {
		return out
	}
	for _, a := range h.attrs[i] {
		out[a.Key] = a.Value.String()
	}
	return out
}

func (h *b2c1RecordingHandler) text() string {
	var b strings.Builder
	for i, r := range h.records {
		b.WriteString(r.Message)
		b.WriteByte(' ')
		for _, a := range h.attrs[i] {
			b.WriteString(a.Key)
			b.WriteByte('=')
			b.WriteString(a.Value.String())
			b.WriteByte(' ')
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// b2c1MRWrap wraps medical_records.Service for targeted consultation_created failure.
type b2c1MRWrap struct {
	medical_records.Service
	failCreated    bool
	failCreatedErr error
	createdCalls   atomic.Int32
	examCalls      atomic.Int32
	rxCalls        atomic.Int32
}

func (w *b2c1MRWrap) RecordConsultationCreated(
	patientID uint, consultationID uint, department string, doctorName string, authorID uint,
) error {
	w.createdCalls.Add(1)
	if w.failCreated {
		if w.failCreatedErr != nil {
			return w.failCreatedErr
		}
		return errors.New("b2c1 injected consultation_created failure")
	}
	return w.Service.RecordConsultationCreated(patientID, consultationID, department, doctorName, authorID)
}

func (w *b2c1MRWrap) RecordExamRequested(
	patientID uint, consultationID uint, examName string, service string, authorID uint,
) error {
	w.examCalls.Add(1)
	return w.Service.RecordExamRequested(patientID, consultationID, examName, service, authorID)
}

func (w *b2c1MRWrap) RecordMedicationPrescribed(
	patientID uint, consultationID uint, medicationName string, dosage string, service string, authorID uint,
) error {
	w.rxCalls.Add(1)
	return w.Service.RecordMedicationPrescribed(patientID, consultationID, medicationName, dosage, service, authorID)
}

func b2c1SeedPatient(t *testing.T, db *gorm.DB, suffix string) patients.Patient {
	t.Helper()
	p := patients.Patient{
		CodePatient:   fmt.Sprintf("B2C1-%s-%d", suffix, time.Now().UnixNano()%1_000_000),
		NumeroDossier: fmt.Sprintf("DOS-B2C1-%s-%d", suffix, time.Now().UnixNano()%1_000_000),
		Nom:           "B2C1",
		Prenoms:       suffix,
	}
	if err := db.Create(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

func b2c1SeedRecord(t *testing.T, db *gorm.DB, patientID uint) medical_records.MedicalRecord {
	t.Helper()
	rec := medical_records.MedicalRecord{
		PatientID:    patientID,
		RecordNumber: fmt.Sprintf("MR-B2C1-%d", patientID),
		Status:       "active",
	}
	if err := db.Create(&rec).Error; err != nil {
		t.Fatal(err)
	}
	return rec
}

func b2c1CountCreated(t *testing.T, db *gorm.DB, consultationID uint) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&medical_records.MedicalTimelineEvent{}).
		Where("event_type = ? AND reference_type = ? AND reference_id = ?",
			"consultation_created", "consultation", consultationID).
		Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func b2c1Router(svc *Service, authorID uint) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		rbac.SetUser(c, authorID, "doctor", []string{"consultations.create", "consultations.read", "*"})
		c.Next()
	})
	api := r.Group("/api")
	RegisterRoutesWithHandler(api, NewHandler(svc))
	return r
}

func TestLot28EB2C1_C101_C113_ConsultationCreatedBestEffort(t *testing.T) {
	db := consultationIntegrationDB(t)
	repo := NewRepository(db)
	baseMR := medical_records.NewService(medical_records.NewRepository(db))
	const authorID uint = 9101

	// C101 — normal create persists consultation + consultation_created
	t.Run("C101_normal_create", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C101")
		_ = b2c1SeedRecord(t, db, p.ID)
		svc := NewService(repo, baseMR)
		c, err := svc.CreateConsultation(CreateConsultationRequest{
			PatientID: p.ID, DoctorName: "Dr C101", Service: "Médecine",
		}, authorID)
		if err != nil {
			t.Fatalf("C101: %v", err)
		}
		var row Consultation
		if err := db.First(&row, c.ID).Error; err != nil {
			t.Fatalf("C101: consultation missing: %v", err)
		}
		if b2c1CountCreated(t, db, c.ID) != 1 {
			t.Fatal("C101: expected consultation_created event")
		}
	})

	// C102 — payload fields unchanged
	t.Run("C102_event_payload", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C102")
		_ = b2c1SeedRecord(t, db, p.ID)
		svc := NewService(repo, baseMR)
		c, err := svc.CreateConsultation(CreateConsultationRequest{
			PatientID: p.ID, DoctorName: "Dr C102", Service: "Cardiologie",
		}, authorID)
		if err != nil {
			t.Fatalf("C102: %v", err)
		}
		var evt medical_records.MedicalTimelineEvent
		if err := db.Where("event_type = ? AND reference_id = ?", "consultation_created", c.ID).
			First(&evt).Error; err != nil {
			t.Fatal(err)
		}
		if evt.EventType != "consultation_created" {
			t.Fatalf("C102: EventType=%q", evt.EventType)
		}
		if evt.Category != "consultation" {
			t.Fatalf("C102: Category=%q", evt.Category)
		}
		if evt.ReferenceType != "consultation" {
			t.Fatalf("C102: ReferenceType=%q", evt.ReferenceType)
		}
		if evt.ReferenceID == nil || *evt.ReferenceID != c.ID {
			t.Fatalf("C102: ReferenceID=%v want %d", evt.ReferenceID, c.ID)
		}
		if evt.Title != "Consultation créée" {
			t.Fatalf("C102: Title=%q", evt.Title)
		}
		if !strings.Contains(evt.Description, "Cardiologie") || !strings.Contains(evt.Description, "Dr C102") {
			t.Fatalf("C102: Description=%q", evt.Description)
		}
		if evt.Severity != "info" {
			t.Fatalf("C102: Severity=%q", evt.Severity)
		}
		if evt.CreatedBy != authorID {
			t.Fatalf("C102: CreatedBy=%d", evt.CreatedBy)
		}
		if evt.EventDate.IsZero() {
			t.Fatal("C102: EventDate empty")
		}
		if evt.DepartmentID != nil {
			t.Fatalf("C102: DepartmentID=%v want nil", evt.DepartmentID)
		}
	})

	// C103–C108 — injected timeline failure after business commit
	t.Run("C103_C108_timeline_failure_policy_a", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C103")
		_ = b2c1SeedRecord(t, db, p.ID)

		sentinel := errors.New("SENTINEL_B2C1_RAW_ERR patient=Dupont allergy=Penicillin DSN=postgres://u:p@h/db JWT=Bearer.abc SQL=SELECT * FROM patients")
		wrap := &b2c1MRWrap{
			Service:        baseMR,
			failCreated:    true,
			failCreatedErr: sentinel,
		}
		svc := NewService(repo, wrap)

		handler := &b2c1RecordingHandler{}
		prev := slog.Default()
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(prev) })

		router := b2c1Router(svc, authorID)
		body, _ := json.Marshal(map[string]any{
			"patientId":  p.ID,
			"doctorName": "Dr Fail",
			"service":    "Médecine",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/consultations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// C104 — HTTP 201 (not 500)
		if w.Code != http.StatusCreated {
			t.Fatalf("C104: status=%d body=%s", w.Code, w.Body.String())
		}

		var payload Consultation
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("C105: decode: %v", err)
		}
		if payload.ID == 0 {
			t.Fatal("C105: missing consultation id in response")
		}

		// C103 / C105 — consultation committed; returned ID matches row
		var row Consultation
		if err := db.First(&row, payload.ID).Error; err != nil {
			t.Fatalf("C103: consultation not committed: %v", err)
		}
		if row.PatientID != p.ID {
			t.Fatalf("C105: patient mismatch got %d want %d", row.PatientID, p.ID)
		}

		// C106 — no consultation_created row
		if b2c1CountCreated(t, db, payload.ID) != 0 {
			t.Fatal("C106: consultation_created must be absent after injected failure")
		}
		if wrap.createdCalls.Load() != 1 {
			t.Fatalf("C103: RecordConsultationCreated calls=%d", wrap.createdCalls.Load())
		}

		// C107 — safe Warn
		found := false
		for i, r := range handler.records {
			if r.Level != slog.LevelWarn {
				continue
			}
			if r.Message != "consultation_created timeline event persistence failed" {
				continue
			}
			attrs := handler.attrMap(i)
			if attrs["operation"] != consultationCreatedTimelineOperation {
				t.Fatalf("C107: operation=%q", attrs["operation"])
			}
			if attrs["event_type"] != consultationCreatedTimelineEventType {
				t.Fatalf("C107: event_type=%q", attrs["event_type"])
			}
			if attrs["consultation_id"] != fmt.Sprint(payload.ID) {
				t.Fatalf("C107: consultation_id=%q want %d", attrs["consultation_id"], payload.ID)
			}
			if attrs["error_class"] != consultationCreatedTimelineErrorClassPersist {
				t.Fatalf("C107: error_class=%q", attrs["error_class"])
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("C107: missing Warn; captured=%q", handler.text())
		}

		// C108 — no raw error / PHI / secrets in log
		out := handler.text()
		for _, marker := range []string{
			"SENTINEL_B2C1_RAW_ERR",
			"Dupont",
			"Penicillin",
			"postgres://",
			"Bearer.abc",
			"SELECT * FROM patients",
			"password",
			`"patientId"`,
		} {
			if strings.Contains(out, marker) {
				t.Fatalf("C108: log leaked marker %q in %q", marker, out)
			}
		}
		if strings.Contains(w.Body.String(), "timeline") || strings.Contains(w.Body.String(), "warning") {
			t.Fatalf("C105: response must not expose timeline warning: %s", w.Body.String())
		}
	})

	// C109 — actor server-owned from JWT author path
	t.Run("C109_actor_server_owned", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C109")
		_ = b2c1SeedRecord(t, db, p.ID)
		const jwtAuthor uint = 9209
		svc := NewService(repo, baseMR)
		router := b2c1Router(svc, jwtAuthor)
		body, _ := json.Marshal(map[string]any{
			"patientId": p.ID, "doctorName": "Dr Actor", "service": "Médecine",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/consultations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("C109: status=%d", w.Code)
		}
		var payload Consultation
		_ = json.Unmarshal(w.Body.Bytes(), &payload)
		var evt medical_records.MedicalTimelineEvent
		if err := db.Where("event_type = ? AND reference_id = ?", "consultation_created", payload.ID).
			First(&evt).Error; err != nil {
			t.Fatal(err)
		}
		if evt.CreatedBy != jwtAuthor {
			t.Fatalf("C109: CreatedBy=%d want %d", evt.CreatedBy, jwtAuthor)
		}
	})

	// C110 — business persistence failure still fails (no 201, no timeline attempt)
	t.Run("C110_business_failure_unchanged", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C110")
		_ = b2c1SeedRecord(t, db, p.ID)
		wrap := &b2c1MRWrap{Service: baseMR}
		svc := NewService(repo, wrap)
		router := b2c1Router(svc, authorID)
		body, _ := json.Marshal(map[string]any{
			"patientId":     p.ID,
			"doctorName":    "Dr BizFail",
			"service":       "Médecine",
			"prescriptions": []map[string]any{{"presentationId": 999999001, "quantity": 1}},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/consultations", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusCreated {
			t.Fatal("C110: must not return 201 on business failure")
		}
		if wrap.createdCalls.Load() != 0 {
			t.Fatal("C110: RecordConsultationCreated must not run after business failure")
		}
		var n int64
		if err := db.Model(&Consultation{}).Where("patient_id = ? AND doctor_name = ?", p.ID, "Dr BizFail").Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatal("C110: consultation must not be persisted on business failure")
		}
	})

	// C111 — TakeDoctor independence: direct create does not require timeline success;
	// queue TakeDoctor never calls RecordConsultationCreated (separate package regression).
	t.Run("C111_take_doctor_independence", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C111")
		// nil medicalRecordsService — mirrors queue create path (no timeline dependency)
		svc := NewService(repo, nil)
		c, err := svc.CreateConsultation(CreateConsultationRequest{
			PatientID: p.ID, DoctorName: "Dr QueueLike", Service: "Urgences",
		}, authorID)
		if err != nil {
			t.Fatalf("C111: create without medicalRecordsService: %v", err)
		}
		var row Consultation
		if err := db.First(&row, c.ID).Error; err != nil {
			t.Fatalf("C111: consultation missing: %v", err)
		}
		if b2c1CountCreated(t, db, c.ID) != 0 {
			t.Fatal("C111: no timeline expected without medicalRecordsService")
		}
	})

	// C112 — exam / Rx best-effort still invoked after created failure
	t.Run("C112_exam_rx_best_effort", func(t *testing.T) {
		p := b2c1SeedPatient(t, db, "C112")
		_ = b2c1SeedRecord(t, db, p.ID)

		exam := MedicalExam{Code: fmt.Sprintf("EX-B2C1-%d", time.Now().UnixNano()), Name: "NFS", Category: "lab", IsActive: true}
		if err := db.Create(&exam).Error; err != nil {
			t.Fatal(err)
		}
		family := pharmacy.MedicationFamily{Code: fmt.Sprintf("FAM-%d", time.Now().UnixNano()), Name: "Fam", IsActive: true}
		if err := db.Create(&family).Error; err != nil {
			t.Fatal(err)
		}
		med := pharmacy.Medication{FamilyID: family.ID, Code: fmt.Sprintf("MED-%d", time.Now().UnixNano()), Name: "Para", IsActive: true}
		if err := db.Create(&med).Error; err != nil {
			t.Fatal(err)
		}
		pres := pharmacy.MedicationPresentation{
			MedicationID: med.ID, Code: fmt.Sprintf("PRES-%d", time.Now().UnixNano()),
			Dosage: "500mg", Form: "cp", Route: "PO", IsActive: true,
		}
		if err := db.Create(&pres).Error; err != nil {
			t.Fatal(err)
		}

		wrap := &b2c1MRWrap{Service: baseMR, failCreated: true}
		svc := NewService(repo, wrap)
		c, err := svc.CreateConsultation(CreateConsultationRequest{
			PatientID:  p.ID,
			DoctorName: "Dr ExamRx",
			Service:    "Médecine",
			ExamIDs:    []uint{exam.ID},
			Prescriptions: []PrescriptionRequest{{
				PresentationID: pres.ID, Quantity: 1,
			}},
		}, authorID)
		if err != nil {
			t.Fatalf("C112: %v", err)
		}
		if wrap.createdCalls.Load() != 1 {
			t.Fatalf("C112: createdCalls=%d", wrap.createdCalls.Load())
		}
		if wrap.examCalls.Load() != 1 {
			t.Fatalf("C112: examCalls=%d (best-effort must still run)", wrap.examCalls.Load())
		}
		if wrap.rxCalls.Load() != 1 {
			t.Fatalf("C112: rxCalls=%d (best-effort must still run)", wrap.rxCalls.Load())
		}
		var row Consultation
		if err := db.First(&row, c.ID).Error; err != nil {
			t.Fatal(err)
		}
		if b2c1CountCreated(t, db, c.ID) != 0 {
			t.Fatal("C112: created event must be absent when injected fail")
		}
		// exam/rx events from real MR service should still exist
		var examN, rxN int64
		_ = db.Model(&medical_records.MedicalTimelineEvent{}).
			Where("reference_id = ? AND event_type = ?", c.ID, "exam_requested").Count(&examN)
		_ = db.Model(&medical_records.MedicalTimelineEvent{}).
			Where("reference_id = ? AND event_type = ?", c.ID, "medication_prescribed").Count(&rxN)
		if examN != 1 || rxN != 1 {
			t.Fatalf("C112: exam_requested=%d medication_prescribed=%d", examN, rxN)
		}
	})

	// C113 — classifier unit + B2-A files untouched (enforced by product scope)
	t.Run("C113_classifier_and_b2a_scope", func(t *testing.T) {
		if classifyConsultationCreatedTimelineError(nil) != consultationCreatedTimelineErrorClassNone {
			t.Fatal("C113: nil class")
		}
		if classifyConsultationCreatedTimelineError(errors.New("opaque")) != consultationCreatedTimelineErrorClassPersist {
			t.Fatal("C113: opaque class")
		}
		if classifyConsultationCreatedTimelineError(gorm.ErrInvalidDB) != consultationCreatedTimelineErrorClassDatabase {
			t.Fatal("C113: database class")
		}
		if classifyConsultationCreatedTimelineError(fmt.Errorf("wrap: %w", gorm.ErrDuplicatedKey)) != consultationCreatedTimelineErrorClassDatabase {
			t.Fatal("C113: wrapped database class")
		}
	})
}

func TestLot28EB2C1_False500RegressionGone(t *testing.T) {
	// Conceptual regression: timeline fail after commit must return 201 so clients
	// are not prompted into an error-driven second POST. Does NOT claim POST idempotency.
	db := consultationIntegrationDB(t)
	repo := NewRepository(db)
	baseMR := medical_records.NewService(medical_records.NewRepository(db))
	wrap := &b2c1MRWrap{Service: baseMR, failCreated: true}
	svc := NewService(repo, wrap)
	const authorID uint = 9301
	p := b2c1SeedPatient(t, db, "F500")
	_ = b2c1SeedRecord(t, db, p.ID)

	router := b2c1Router(svc, authorID)
	body, _ := json.Marshal(map[string]any{
		"patientId": p.ID, "doctorName": "Dr F500", "service": "Médecine",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/consultations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("false-500 regression: got %d want 201", w.Code)
	}
	var first Consultation
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil || first.ID == 0 {
		t.Fatalf("false-500 regression: bad body %s", w.Body.String())
	}
	if b2c1CountCreated(t, db, first.ID) != 0 {
		t.Fatal("false-500 regression: timeline should be missing")
	}
	// A successful (non-error) response means the create page navigates away rather
	// than showing an error that invites resubmit. Residual MODE 3 remains.
}
