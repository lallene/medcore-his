package patients

import (
	"github.com/lallene/medcore-his/backend/internal/core/events"
	"github.com/lallene/medcore-his/backend/internal/core/logger"
)

type PatientAuditListener struct{}

func (PatientAuditListener) Handle(e events.Event) error {

	event := e.(PatientCreated)

	logger.Info(
		"Patient créé",
		"module", "patients",
		"event", event.Name(),
		"patientId", event.PatientID,
	)

	return nil
}
