package medical_records

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
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lallene/medcore-his/backend/internal/core/rbac"
	"gorm.io/gorm"
)

// LOT28E-B2-A — best-effort generic CMR timeline contract + observability.
//
// Intentional asymmetry (do not "harmonize" with B1):
//   - document_added / document_archived → same-TX fail-closed
//   - common_medical_record_updated → post-commit best-effort (CMR stays committed)

type b2aFailingTimelineRepo struct {
	Repository
	failCreate bool
}

func (r *b2aFailingTimelineRepo) CreateTimelineEvent(event *MedicalTimelineEvent) error {
	if r.failCreate {
		// Document lifecycle events use tx.Create inside SaveCommonMedicalRecord,
		// not CreateTimelineEvent — so this only breaks post-commit generic emit.
		return errors.New("b2a injected CreateTimelineEvent failure")
	}
	return r.Repository.CreateTimelineEvent(event)
}

type b2aRecordingHandler struct {
	records []slog.Record
	attrs   [][]slog.Attr
}

func (h *b2aRecordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *b2aRecordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r.Clone())
	var as []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		as = append(as, a)
		return true
	})
	h.attrs = append(h.attrs, as)
	return nil
}
func (h *b2aRecordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *b2aRecordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *b2aRecordingHandler) attrMap(i int) map[string]string {
	out := map[string]string{}
	if i < 0 || i >= len(h.attrs) {
		return out
	}
	for _, a := range h.attrs[i] {
		out[a.Key] = a.Value.String()
	}
	return out
}

func (h *b2aRecordingHandler) text() string {
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

func b2aCountGeneric(t *testing.T, db *gorm.DB, patientID uint) int64 {
	t.Helper()
	return b1CountEvents(t, db, "patient_id = ? AND event_type = ?", patientID, genericCMRTimelineEventType)
}

func b2aSeedWithProfile(t *testing.T, db *gorm.DB, patientID uint) MedicalRecord {
	t.Helper()
	record := b1SeedRecord(t, db, patientID)
	if err := db.Create(&PatientMedicalProfile{
		MedicalRecordID: record.ID,
		PatientID:       patientID,
		Profession:      "baseline",
	}).Error; err != nil {
		t.Fatal(err)
	}
	return record
}

func TestLot28EB2A_A01_A12_GenericCMRBestEffort(t *testing.T) {
	db := c1TestDB(t)
	repo := NewRepository(db)
	service := NewService(repo)
	const authorID uint = 77

	// A01 — successful non-document mutation emits generic once
	t.Run("A01_non_document_emits_generic", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2901)
		before := b2aCountGeneric(t, db, rec.PatientID)
		resp, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Profile:           &PatientMedicalProfileRequest{Profession: str("chirurgien")},
		}, authorID)
		if err != nil {
			t.Fatalf("A01: %v", err)
		}
		after := b2aCountGeneric(t, db, rec.PatientID)
		if after != before+1 {
			t.Fatalf("A01: generic count before=%d after=%d", before, after)
		}
		var evt MedicalTimelineEvent
		if err := db.Where("patient_id = ? AND event_type = ?", rec.PatientID, genericCMRTimelineEventType).
			Order("id DESC").First(&evt).Error; err != nil {
			t.Fatal(err)
		}
		if evt.Category != "medical_record" || evt.ReferenceType != "medical_record" {
			t.Fatalf("A01: payload category/ref=%q/%q", evt.Category, evt.ReferenceType)
		}
		if evt.ReferenceID == nil || *evt.ReferenceID != resp.MedicalRecord.ID {
			t.Fatalf("A01: ReferenceID=%v want %d", evt.ReferenceID, resp.MedicalRecord.ID)
		}
		if evt.Title != "Dossier médical mis à jour" {
			t.Fatalf("A01: title=%q", evt.Title)
		}
		if evt.Description != "Les informations longitudinales du patient ont été mises à jour." {
			t.Fatalf("A01: description=%q", evt.Description)
		}
		if evt.Severity != "info" || evt.DepartmentID != nil {
			t.Fatalf("A01: severity/dept=%q/%v", evt.Severity, evt.DepartmentID)
		}
	})

	// A02 — document create only → document_added, no generic
	t.Run("A02_document_create_only_no_generic", func(t *testing.T) {
		rec := b1SeedRecord(t, db, 2902)
		before := b2aCountGeneric(t, db, rec.PatientID)
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Documents: PatchCollection[MedicalDocumentRequest]{
				Present: true,
				Upsert: []MedicalDocumentRequest{{
					Label: b1DocLabel(), Type: b1DocType(), FileURL: b1DocURL(),
				}},
			},
		}, authorID); err != nil {
			t.Fatalf("A02: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A02: generic emitted for document-only create")
		}
		if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", rec.PatientID, TimelineEventDocumentAdded) != 1 {
			t.Fatal("A02: missing document_added")
		}
	})

	// A03 — document archive only → document_archived, no generic
	t.Run("A03_document_archive_only_no_generic", func(t *testing.T) {
		rec := b1SeedRecord(t, db, 2903)
		doc := b1CreateDoc(t, db, repo, &rec, authorID, "A03 Doc")
		before := b2aCountGeneric(t, db, rec.PatientID)
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Documents: PatchCollection[MedicalDocumentRequest]{
				Present: true, DeleteIDs: []uint{doc.ID},
			},
		}, authorID); err != nil {
			t.Fatalf("A03: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A03: generic emitted for document-only archive")
		}
		if b1CountEvents(t, db, "patient_id = ? AND event_type = ? AND reference_id = ?",
			rec.PatientID, TimelineEventDocumentArchived, doc.ID) != 1 {
			t.Fatal("A03: missing document_archived")
		}
	})

	// A04 — document metadata / FileURL only → no generic
	t.Run("A04_document_metadata_only_no_generic", func(t *testing.T) {
		rec := b1SeedRecord(t, db, 2904)
		doc := b1CreateDoc(t, db, repo, &rec, authorID, "A04 Doc")
		before := b2aCountGeneric(t, db, rec.PatientID)
		newLabel := "A04 Label Edit"
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Documents: PatchCollection[MedicalDocumentRequest]{
				Present: true,
				Upsert:  []MedicalDocumentRequest{{ID: doc.ID, Label: &newLabel}},
			},
		}, authorID); err != nil {
			t.Fatalf("A04 meta: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A04: generic after metadata edit")
		}
		newURL := "https://docs.example.com/a04-v2.pdf"
		if err := db.First(&rec, rec.ID).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Documents: PatchCollection[MedicalDocumentRequest]{
				Present: true,
				Upsert:  []MedicalDocumentRequest{{ID: doc.ID, FileURL: &newURL}},
			},
		}, authorID); err != nil {
			t.Fatalf("A04 url: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A04: generic after FileURL edit")
		}
	})

	// A05 — mixed non-document + document create → document event + exactly one generic
	t.Run("A05_mixed_mutation_one_generic", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2905)
		before := b2aCountGeneric(t, db, rec.PatientID)
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Profile:           &PatientMedicalProfileRequest{Profession: str("mixed-prof")},
			Documents: PatchCollection[MedicalDocumentRequest]{
				Present: true,
				Upsert: []MedicalDocumentRequest{{
					Label: b1DocLabel(), Type: b1DocType(), FileURL: b1DocURL(),
				}},
			},
		}, authorID); err != nil {
			t.Fatalf("A05: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before+1 {
			t.Fatal("A05: expected exactly one new generic")
		}
		if b1CountEvents(t, db, "patient_id = ? AND event_type = ?", rec.PatientID, TimelineEventDocumentAdded) != 1 {
			t.Fatal("A05: missing document_added")
		}
	})

	// A06 — no-op → no generic
	t.Run("A06_noop_no_generic", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2906)
		stamp := rec.UpdatedAt
		before := b2aCountGeneric(t, db, rec.PatientID)
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &stamp,
		}, authorID); err != nil {
			t.Fatalf("A06: %v", err)
		}
		if err := db.First(&rec, rec.ID).Error; err != nil {
			t.Fatal(err)
		}
		if !rec.UpdatedAt.Equal(stamp) {
			t.Fatal("A06: UpdatedAt changed on no-op")
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A06: generic on no-op")
		}
	})

	// A07 + A08 + A09 — generic insert failure: CMR committed, HTTP 200, Warn observable
	t.Run("A07_A08_A09_generic_failure_best_effort", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2907)
		stampBefore := rec.UpdatedAt
		beforeGeneric := b2aCountGeneric(t, db, rec.PatientID)

		failRepo := &b2aFailingTimelineRepo{Repository: repo, failCreate: true}
		failSvc := NewService(failRepo)

		handler := &b2aRecordingHandler{}
		prev := slog.Default()
		slog.SetDefault(slog.New(handler))
		t.Cleanup(func() { slog.SetDefault(prev) })

		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(func(c *gin.Context) {
			rbac.SetUser(c, authorID, "doctor", nil)
			c.Next()
		})
		router.PUT("/api/patients/:id/common-medical-record", NewHandler(failSvc).UpdateCommonMedicalRecord)

		body, _ := json.Marshal(map[string]any{
			"expected_updated_at": stampBefore,
			"profile":             map[string]string{"profession": "best-effort-fail"},
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/patients/%d/common-medical-record", rec.PatientID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		// A08 — HTTP success unchanged
		if w.Code != http.StatusOK {
			t.Fatalf("A08: status=%d body=%s", w.Code, w.Body.String())
		}
		var payload CommonMedicalRecordResponse
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("A08: decode: %v", err)
		}
		if payload.Profile == nil || payload.Profile.Profession != "best-effort-fail" {
			t.Fatalf("A08: CMR not returned/updated: %#v", payload.Profile)
		}

		// A07 — CMR committed, generic missing
		var persisted MedicalRecord
		if err := db.First(&persisted, rec.ID).Error; err != nil {
			t.Fatal(err)
		}
		if persisted.UpdatedAt.Equal(stampBefore) {
			t.Fatal("A07: CMR UpdatedAt not committed after generic failure")
		}
		var profile PatientMedicalProfile
		if err := db.Where("medical_record_id = ?", rec.ID).First(&profile).Error; err != nil {
			t.Fatal(err)
		}
		if profile.Profession != "best-effort-fail" {
			t.Fatalf("A07: profile not persisted: %q", profile.Profession)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != beforeGeneric {
			t.Fatal("A07: unexpected generic event after injected failure")
		}

		// A09 — safe Warn observability
		found := false
		for i, r := range handler.records {
			if r.Level != slog.LevelWarn {
				continue
			}
			if r.Message != "generic CMR timeline event persistence failed" {
				continue
			}
			attrs := handler.attrMap(i)
			if attrs["operation"] != genericCMRTimelineOperation {
				t.Fatalf("A09: operation=%q", attrs["operation"])
			}
			if attrs["event_type"] != genericCMRTimelineEventType {
				t.Fatalf("A09: event_type=%q", attrs["event_type"])
			}
			if attrs["medical_record_id"] != fmt.Sprint(rec.ID) {
				t.Fatalf("A09: medical_record_id=%q want %d", attrs["medical_record_id"], rec.ID)
			}
			if attrs["error_class"] != genericCMRTimelineErrorClassPersist {
				t.Fatalf("A09: error_class=%q", attrs["error_class"])
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("A09: missing Warn log; captured=%q", handler.text())
		}
		out := handler.text()
		for _, marker := range []string{
			"best-effort-fail",
			"b2a injected",
			"password",
			"postgres://",
			"Bearer ",
			"Allergène",
			"@example",
		} {
			if strings.Contains(out, marker) {
				t.Fatalf("A09: log leaked marker %q in %q", marker, out)
			}
		}
	})

	// A10 — after commit + failed generic, retry with current OCC + identical payload → no-op, no generic
	t.Run("A10_retry_noop_after_failed_generic", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2910)
		failRepo := &b2aFailingTimelineRepo{Repository: repo, failCreate: true}
		failSvc := NewService(failRepo)

		resp, err := failSvc.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &rec.UpdatedAt,
			Profile:           &PatientMedicalProfileRequest{Profession: str("retry-once")},
		}, authorID)
		if err != nil {
			t.Fatalf("A10 mutate: %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != 0 {
			t.Fatal("A10: generic must be absent after injected failure")
		}
		current := resp.MedicalRecord.UpdatedAt
		before := b2aCountGeneric(t, db, rec.PatientID)

		// Switch to healthy repo so a no-op does not attempt (and cannot emit) falsely.
		okSvc := NewService(repo)
		again, err := okSvc.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &current,
			Profile:           &PatientMedicalProfileRequest{Profession: str("retry-once")},
		}, authorID)
		if err != nil {
			t.Fatalf("A10 retry: %v", err)
		}
		if !again.MedicalRecord.UpdatedAt.Equal(current) {
			t.Fatal("A10: retry with identical payload must be no-op (UpdatedAt unchanged)")
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A10: retry no-op must not emit generic")
		}
	})

	// A11 — stale OCC → conflict, no commit, no generic
	t.Run("A11_stale_occ_no_generic", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2911)
		stale := rec.UpdatedAt
		if _, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &stale,
			Profile:           &PatientMedicalProfileRequest{Profession: str("first-write")},
		}, authorID); err != nil {
			t.Fatalf("A11 setup: %v", err)
		}
		before := b2aCountGeneric(t, db, rec.PatientID)
		_, err := service.UpdateCommonMedicalRecord(rec.PatientID, UpdateCommonMedicalRecordRequest{
			ExpectedUpdatedAt: &stale,
			Profile:           &PatientMedicalProfileRequest{Profession: str("stale-write")},
		}, authorID)
		if !errors.Is(err, ErrCommonMedicalRecordConflict) {
			t.Fatalf("A11: want conflict got %v", err)
		}
		if b2aCountGeneric(t, db, rec.PatientID) != before {
			t.Fatal("A11: generic after stale OCC")
		}
		var profile PatientMedicalProfile
		db.Where("medical_record_id = ?", rec.ID).First(&profile)
		if profile.Profession != "first-write" {
			t.Fatalf("A11: stale write persisted: %q", profile.Profession)
		}
	})

	// A12 — CreatedBy is server-owned authorID (JWT), not client-forged
	t.Run("A12_actor_server_owned", func(t *testing.T) {
		rec := b2aSeedWithProfile(t, db, 2912)
		const jwtAuthor uint = 88
		gin.SetMode(gin.TestMode)
		router := gin.New()
		router.Use(func(c *gin.Context) {
			rbac.SetUser(c, jwtAuthor, "doctor", nil)
			c.Next()
		})
		router.PUT("/api/patients/:id/common-medical-record", NewHandler(service).UpdateCommonMedicalRecord)

		body, _ := json.Marshal(map[string]any{
			"expected_updated_at": rec.UpdatedAt,
			"created_by":          9999,
			"updated_by":          9999,
			"profile":             map[string]string{"profession": "actor-proof"},
		})
		req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/patients/%d/common-medical-record", rec.PatientID), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("A12: status=%d body=%s", w.Code, w.Body.String())
		}
		var evt MedicalTimelineEvent
		if err := db.Where("patient_id = ? AND event_type = ?", rec.PatientID, genericCMRTimelineEventType).
			Order("id DESC").First(&evt).Error; err != nil {
			t.Fatalf("A12: event missing: %v", err)
		}
		if evt.CreatedBy != jwtAuthor {
			t.Fatalf("A12: CreatedBy=%d want %d", evt.CreatedBy, jwtAuthor)
		}
	})
}

func TestLot28EB2A_ClassifyGenericCMRTimelineError(t *testing.T) {
	if classifyGenericCMRTimelineError(nil) != genericCMRTimelineErrorClassNone {
		t.Fatal("nil")
	}
	if classifyGenericCMRTimelineError(errors.New("opaque")) != genericCMRTimelineErrorClassPersist {
		t.Fatal("opaque")
	}
	if classifyGenericCMRTimelineError(gorm.ErrInvalidDB) != genericCMRTimelineErrorClassDatabase {
		t.Fatal("invalid db")
	}
	if classifyGenericCMRTimelineError(fmt.Errorf("wrap: %w", gorm.ErrDuplicatedKey)) != genericCMRTimelineErrorClassDatabase {
		t.Fatal("dup")
	}
}

func TestLot28EB2A_ShouldEmitGenericCMREventUnchanged(t *testing.T) {
	// Lock truth table — do not change ShouldEmitGenericCMREvent in B2-A.
	cases := []struct {
		name string
		r    CommonMedicalRecordSaveResult
		want bool
	}{
		{"non_document", CommonMedicalRecordSaveResult{NonDocumentChanged: true}, true},
		{"doc_lifecycle", CommonMedicalRecordSaveResult{DocumentLifecycleChanged: true}, false},
		{"doc_meta", CommonMedicalRecordSaveResult{DocumentMetadataChanged: true}, false},
		{"mixed", CommonMedicalRecordSaveResult{NonDocumentChanged: true, DocumentLifecycleChanged: true}, true},
		{"noop", CommonMedicalRecordSaveResult{}, false},
	}
	for _, tc := range cases {
		if got := tc.r.ShouldEmitGenericCMREvent(); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
