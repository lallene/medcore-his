package scheduling

// Query safeguards for the availability engine (LOT 23C).
const (
	MaxQueryRangeDays  = 31
	MinDurationMinutes = 5
	MaxDurationMinutes = 480 // 8h clinical upper bound
	MinSlotStepMinutes = 5
	MaxSlotStepMinutes = 240
	MaxGeneratedSlots  = 10000
	// LegacyAppointmentFallbackMinutes when scheduled_end_at is NULL and no type duration.
	LegacyAppointmentFallbackMinutes = 30
)

// EnvLegacyFallbackMinutes may override LegacyAppointmentFallbackMinutes (optional).
const EnvLegacyFallbackMinutes = "MEDCORE_LEGACY_APPOINTMENT_FALLBACK_MINUTES"
