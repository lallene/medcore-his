package cash

import (
	"errors"
	"strings"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

// NormalizeSessionCommandKey reuses the LOT29B key length/trim contract.
func NormalizeSessionCommandKey(raw string) (string, error) {
	return billing.NormalizePaymentIdempotencyKey(raw)
}

func openFingerprintMatch(x Session, registerID uint, openingFloat int64, note string) bool {
	return x.CashRegisterID == registerID &&
		x.OpeningFloat == openingFloat &&
		x.OpeningNote == note
}

func openFingerprintConflict(x Session, registerID uint, openingFloat int64, note string) error {
	if openFingerprintMatch(x, registerID, openingFloat, note) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func closeFingerprintMatch(x Session, sessionID uint, counted int64, note string) bool {
	if x.ID != sessionID || x.CountedCashAmount == nil {
		return false
	}
	return *x.CountedCashAmount == counted && x.ClosingNote == note
}

func closeFingerprintConflict(x Session, sessionID uint, counted int64, note string) error {
	if closeFingerprintMatch(x, sessionID, counted, note) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func isSessionIdempotencyUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"open_idempotency_key", "close_idempotency_key", "cash_sessions",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}
