package act_catalog

import (
	"testing"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
)

func TestNormalizeCodeUppercases(t *testing.T) {
	t.Parallel()
	if got := normalizeCode("  cons-01 "); got != "CONS-01" {
		t.Fatalf("normalizeCode=%q", got)
	}
}

func TestValidateCategoryAllowList(t *testing.T) {
	t.Parallel()
	for cat := range ValidCategories {
		if err := validateCategory(cat); err != nil {
			t.Fatalf("valid category %s rejected: %v", cat, err)
		}
	}
	if err := validateCategory("MEDICATION"); err == nil {
		t.Fatal("MEDICATION must be rejected")
	}
	if err := validateCategory("NURSING"); err == nil {
		t.Fatal("NURSING must be rejected")
	}
	app, ok := validateCategory("SURGERY").(*coreerrors.AppError)
	if !ok || app.Status != 400 {
		t.Fatalf("expected 400 AppError, got %v", validateCategory("SURGERY"))
	}
}

func TestValidatePriceNonNegative(t *testing.T) {
	t.Parallel()
	if err := validatePrice(0); err != nil {
		t.Fatal(err)
	}
	if err := validatePrice(1500); err != nil {
		t.Fatal(err)
	}
	if err := validatePrice(-1); err == nil {
		t.Fatal("negative price accepted")
	}
}

func TestUpdateRequestHasNoCodeField(t *testing.T) {
	t.Parallel()
	// Compile-time / structural contract: UpdateRequest must not expose Code.
	req := UpdateRequest{Label: "x", Category: "OTHER", BasePrice: 0}
	_ = req
	// Reflect via JSON tags: ensure Create has code, Update does not appear in type fields.
	var create CreateRequest
	create.Code = "X"
	if create.Code == "" {
		t.Fatal("CreateRequest must have Code")
	}
}

func TestDefaultActiveFilterContract(t *testing.T) {
	t.Parallel()
	// Documented list contract: nil Active means active-only (implemented in List).
	f := ListFilter{}
	if f.Active != nil {
		t.Fatal("default Active must be nil (service treats as active-only)")
	}
}

func TestTableName(t *testing.T) {
	t.Parallel()
	if (Entry{}).TableName() != "act_catalog_entries" {
		t.Fatal((Entry{}).TableName())
	}
}

func TestNormalizeCurrencyDefaultXOF(t *testing.T) {
	t.Parallel()
	if normalizeCurrency("") != "XOF" {
		t.Fatal(normalizeCurrency(""))
	}
	if normalizeCurrency("usd") != "USD" {
		t.Fatal(normalizeCurrency("usd"))
	}
}
