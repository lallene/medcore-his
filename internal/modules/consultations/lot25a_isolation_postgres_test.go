package consultations

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// LOT 25A — consultationIntegrationDB must own PharmacyDispensation writes.
// Explicit TableName() models must resolve via search_path into the ephemeral
// schema, not public.
func TestPostgresConsultationIntegrationDBIsolatesPharmacyDispensationFromPublic(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL absent: test PostgreSQL JWT ignoré")
	}

	db := consultationIntegrationDB(t)

	var schemaName string
	if err := db.Raw(`SELECT current_schema()`).Scan(&schemaName).Error; err != nil || schemaName == "" || schemaName == "public" {
		t.Fatalf("expected ephemeral current_schema, got %q err=%v", schemaName, err)
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adminSQL.Close() })

	var ephemeralTable int64
	if err := admin.Raw(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = ? AND table_name = 'pharmacy_dispensations'`, schemaName,
	).Scan(&ephemeralTable).Error; err != nil {
		t.Fatal(err)
	}
	if ephemeralTable != 1 {
		t.Fatalf("pharmacy_dispensations must exist in ephemeral schema %q, count=%d", schemaName, ephemeralTable)
	}

	var publicBefore, publicEmptyBefore int64
	if err := admin.Raw(`SELECT COUNT(*) FROM public.pharmacy_dispensations`).Scan(&publicBefore).Error; err != nil {
		t.Fatal(err)
	}
	if err := admin.Raw(`SELECT COUNT(*) FROM public.pharmacy_dispensations WHERE idempotency_key = ''`).Scan(&publicEmptyBefore).Error; err != nil {
		t.Fatal(err)
	}

	key := fmt.Sprintf("LOT25A-ISOLATION-%d", time.Now().UnixNano())
	ref := uint(424242)
	row := pharmacy.PharmacyDispensation{
		PresentationID: 11,
		Quantity:       4,
		Status:         pharmacy.DispensationStatusCompleted,
		ReferenceType:  "CONSULTATION_PRESCRIPTION",
		ReferenceID:    &ref,
		IdempotencyKey: key,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create in consultationIntegrationDB: %v", err)
	}

	var inEphemeral, inPublic int64
	ephemeralSQL := fmt.Sprintf(`SELECT COUNT(*) FROM "%s".pharmacy_dispensations WHERE idempotency_key = ?`, schemaName)
	if err := admin.Raw(ephemeralSQL, key).Scan(&inEphemeral).Error; err != nil {
		t.Fatal(err)
	}
	if err := admin.Raw(`SELECT COUNT(*) FROM public.pharmacy_dispensations WHERE idempotency_key = ?`, key).Scan(&inPublic).Error; err != nil {
		t.Fatal(err)
	}
	if inEphemeral != 1 {
		t.Fatalf("row must live in ephemeral schema %q, count=%d", schemaName, inEphemeral)
	}
	if inPublic != 0 {
		t.Fatalf("public.pharmacy_dispensations must not receive test row, count=%d", inPublic)
	}

	var publicAfter, publicEmptyAfter int64
	if err := admin.Raw(`SELECT COUNT(*) FROM public.pharmacy_dispensations`).Scan(&publicAfter).Error; err != nil {
		t.Fatal(err)
	}
	if err := admin.Raw(`SELECT COUNT(*) FROM public.pharmacy_dispensations WHERE idempotency_key = ''`).Scan(&publicEmptyAfter).Error; err != nil {
		t.Fatal(err)
	}
	if publicAfter != publicBefore {
		t.Fatalf("public.pharmacy_dispensations mutated: before=%d after=%d", publicBefore, publicAfter)
	}
	if publicEmptyAfter != publicEmptyBefore {
		t.Fatalf("public empty idempotency_key count mutated: before=%d after=%d", publicEmptyBefore, publicEmptyAfter)
	}
}
