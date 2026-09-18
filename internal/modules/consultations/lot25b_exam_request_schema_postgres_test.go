package consultations

import (
	"testing"
)

// LOT 25B — consultationIntegrationDB must migrate the enriched domain join model
// ConsultationExamRequest, not only the implicit many2many FK pair.
func TestPostgresConsultationIntegrationDBMigratesConsultationExamRequestShape(t *testing.T) {
	db := consultationIntegrationDB(t)

	var schemaName string
	if err := db.Raw(`SELECT current_schema()`).Scan(&schemaName).Error; err != nil || schemaName == "" || schemaName == "public" {
		t.Fatalf("expected ephemeral current_schema, got %q err=%v", schemaName, err)
	}

	required := []string{
		"consultation_id",
		"medical_exam_id",
		"status",
		"notes",
		"priority",
		"prescribed_by",
		"created_at",
		"updated_at",
	}
	for _, col := range required {
		var n int64
		if err := db.Raw(`
			SELECT COUNT(*) FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name = 'consultation_exam_requests'
			  AND column_name = ?`, col,
		).Scan(&n).Error; err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("ephemeral schema %q: consultation_exam_requests.%s missing (want enriched ConsultationExamRequest shape)", schemaName, col)
		}
	}

	// Sanity: Repository.Create path can update prescribed_by without 42703.
	if err := db.Exec(`
		UPDATE consultation_exam_requests
		SET prescribed_by = 1, priority = 'ROUTINE', updated_at = NOW()
		WHERE consultation_id = 0`).Error; err != nil {
		t.Fatalf("prescribed_by update must be valid on ephemeral schema: %v", err)
	}
}
