package patient_queue

import (
	"fmt"
	"time"
)

// AllowedTransitions defines the deterministic stage graph (no silent skips).
var AllowedTransitions = map[string]map[string]bool{
	StageReception: {
		StageWaitingTriage: true,
		StageCancelled:     true,
		StageOnHold:        true,
	},
	StageWaitingTriage: {
		StageTriageInProgress: true,
		StageCancelled:        true,
		StageOnHold:           true,
	},
	StageTriageInProgress: {
		StageWaitingDoctor: true,
		StageCancelled:     true,
		StageOnHold:        true,
	},
	StageWaitingDoctor: {
		StageDoctorInProgress: true,
		StageCancelled:        true,
		StageOnHold:           true,
		StageRedirected:       true,
	},
	StageDoctorInProgress: {
		StageCompleted:  true,
		StageCancelled:  true,
		StageOnHold:     true,
		StageRedirected: true,
	},
	StageOnHold: {
		StageWaitingTriage:    true,
		StageTriageInProgress: true,
		StageWaitingDoctor:    true,
		StageDoctorInProgress: true,
		StageCancelled:        true,
	},
	StageRedirected: {
		StageWaitingTriage: true,
		StageWaitingDoctor: true,
		StageCancelled:     true,
	},
}

func CanTransition(from, to string) bool {
	m, ok := AllowedTransitions[from]
	if !ok {
		return false
	}
	return m[to]
}

func AssertTransition(from, to string) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("transition interdite: %s → %s", from, to)
}

func PriorityRank(p string) int {
	switch p {
	case PriorityUrgent:
		return 1
	case PriorityHigh:
		return 2
	case PriorityNormal:
		return 3
	case PriorityLow:
		return 4
	default:
		return 99
	}
}

func Punctuality(scheduled, arrived time.Time) string {
	delta := arrived.Sub(scheduled)
	if delta < -AppointmentWindow {
		return PunctualEarly
	}
	if delta > AppointmentWindow {
		return PunctualLate
	}
	return PunctualOnTime
}
