package patients

import "time"

type PatientCreated struct {
	PatientID uint
}

func (PatientCreated) Name() string {
	return "patient.created"
}

func (PatientCreated) OccurredAt() time.Time {
	return time.Now()
}
