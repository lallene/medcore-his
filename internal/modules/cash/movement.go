package cash

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	coreerrors "github.com/lallene/medcore-his/backend/internal/core/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MaxMovementReasonLen = 500
	MinMovementReasonLen = 3
	MovementAuditCreated = "cash_movement_created"
)

type MovementRequest struct {
	Direction      string `json:"direction" binding:"required"`
	Type           string `json:"type" binding:"required"`
	Amount         int64  `json:"amount" binding:"required"`
	Reason         string `json:"reason" binding:"required"`
	IdempotencyKey string `json:"idempotencyKey"`
}

func NormalizeMovementReason(raw string) (string, error) {
	reason := strings.TrimSpace(raw)
	if reason == "" {
		return "", coreerrors.BadRequest("Motif obligatoire")
	}
	if utf8.RuneCountInString(reason) < MinMovementReasonLen {
		return "", coreerrors.BadRequest("Motif trop court")
	}
	if utf8.RuneCountInString(reason) > MaxMovementReasonLen {
		return "", coreerrors.BadRequest("Motif trop long")
	}
	return reason, nil
}

func normalizeMovementDirectionType(direction, typ string) (string, string, error) {
	d := strings.ToUpper(strings.TrimSpace(direction))
	t := strings.ToUpper(strings.TrimSpace(typ))
	switch {
	case d == MovementIn && t == MovementManualIn:
		return MovementIn, MovementManualIn, nil
	case d == MovementOut && t == MovementManualOut:
		return MovementOut, MovementManualOut, nil
	default:
		return "", "", coreerrors.BadRequest("Type de mouvement de caisse invalide")
	}
}

func movementFingerprintMatch(m CashMovement, sessionID uint, direction, typ string, amount int64, reason string) bool {
	return m.CashSessionID == sessionID &&
		m.Direction == direction &&
		m.Type == typ &&
		m.Amount == amount &&
		m.Reason == reason
}

func movementFingerprintConflict(m CashMovement, sessionID uint, direction, typ string, amount int64, reason string) error {
	if movementFingerprintMatch(m, sessionID, direction, typ, amount, reason) {
		return nil
	}
	return coreerrors.Conflict("Clé d'idempotence déjà utilisée")
}

func isMovementIdempotencyUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"duplicate key", "unique constraint", "sqlstate 23505",
		"idempotency_key", "cash_movements",
	} {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

// CreateMovement records an append-only physical cash movement on an OPEN session.
// canCreateAny is reserved for elevated authority (cash.movement.create); V1 requires that permission at the route.
func (s *Service) CreateMovement(sessionID uint, r MovementRequest, user uint) (*CashMovement, error) {
	direction, typ, err := normalizeMovementDirectionType(r.Direction, r.Type)
	if err != nil {
		return nil, err
	}
	if r.Amount <= 0 {
		return nil, coreerrors.BadRequest("Montant de mouvement invalide")
	}
	reason, err := NormalizeMovementReason(r.Reason)
	if err != nil {
		return nil, err
	}
	key, err := NormalizeSessionCommandKey(r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	var outID uint
	e := s.db.Transaction(func(tx *gorm.DB) error {
		var prior CashMovement
		if e := tx.Where("idempotency_key=?", key).First(&prior).Error; e == nil {
			if e := movementFingerprintConflict(prior, sessionID, direction, typ, r.Amount, reason); e != nil {
				return e
			}
			outID = prior.ID
			return nil
		} else if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}

		var session Session
		if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&session, sessionID).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return coreerrors.NotFound("CASH_SESSION")
			}
			return e
		}
		if session.Status != SessionOpen {
			return coreerrors.Conflict("Impossible d'enregistrer un mouvement sur une session fermée")
		}

		payTotals, e := loadSessionPaymentTotals(tx, sessionID)
		if e != nil {
			return e
		}
		movTotals, e := loadSessionMovementTotals(tx, sessionID)
		if e != nil {
			return e
		}
		expected := liveExpectedCash(session.OpeningFloat, payTotals.Cash, movTotals.In, movTotals.Out)
		if direction == MovementOut && r.Amount > expected {
			return coreerrors.Conflict("Sortie supérieure aux espèces attendues en caisse")
		}

		now := time.Now()
		m := CashMovement{
			CashSessionID:  session.ID,
			Direction:      direction,
			Type:           typ,
			Amount:         r.Amount,
			Reason:         reason,
			CreatedBy:      user,
			OccurredAt:     now,
			IdempotencyKey: key,
		}
		if e := tx.Create(&m).Error; e != nil {
			if isMovementIdempotencyUniqueViolation(e) {
				var raced CashMovement
				if load := tx.Where("idempotency_key=?", key).First(&raced).Error; load == nil {
					if e := movementFingerprintConflict(raced, sessionID, direction, typ, r.Amount, reason); e != nil {
						return e
					}
					outID = raced.ID
					return nil
				}
			}
			return e
		}
		audit := CashMovementAudit{
			MovementID: m.ID,
			EventType:  MovementAuditCreated,
			SessionID:  session.ID,
			Direction:  direction,
			Type:       typ,
			Amount:     r.Amount,
			Reason:     reason,
			ActorID:    user,
			CreatedAt:  now,
		}
		if e := tx.Create(&audit).Error; e != nil {
			return e
		}
		outID = m.ID
		return nil
	})
	if e != nil {
		return nil, e
	}
	var m CashMovement
	if e := s.db.First(&m, outID).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_MOVEMENT")
	}
	return &m, nil
}

func (s *Service) ListMovements(sessionID uint) ([]CashMovement, error) {
	var session Session
	if e := s.db.Select("id").First(&session, sessionID).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_SESSION")
	}
	var rows []CashMovement
	if e := s.db.Where("cash_session_id=?", sessionID).Order("occurred_at ASC, id ASC").Find(&rows).Error; e != nil {
		return nil, e
	}
	return rows, nil
}

func (s *Service) GetMovement(id uint) (*CashMovement, error) {
	var m CashMovement
	if e := s.db.First(&m, id).Error; e != nil {
		return nil, coreerrors.NotFound("CASH_MOVEMENT")
	}
	return &m, nil
}
