package branding

import (
	"fmt"
	"time"
)

const (
	DocumentTypeConsultationReport = "CR"
	DocumentTypePrescription       = "ORD"
	DocumentTypeExamRequest        = "EXA"
	DocumentTypeSickLeave          = "RM"
	DocumentTypeHospitalization    = "HOSP"
)

func DocumentReference(
	documentType string,
	sourceID uint,
	createdAt time.Time,
) string {
	return fmt.Sprintf(
		"CMSRA-%s-%d-%06d",
		documentType,
		createdAt.Year(),
		sourceID,
	)
}
