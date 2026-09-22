package performed_acts

// CreateRequest identifies the catalogue entry and occurrence context.
// Snapshot fields are server-derived from ActCatalog — never client-authoritative.
type CreateRequest struct {
	PatientID         uint    `json:"patientId" binding:"required"`
	ActCatalogEntryID uint    `json:"actCatalogEntryId" binding:"required"`
	Quantity          float64 `json:"quantity"`
	PerformedAt       string  `json:"performedAt"` // RFC3339; empty = now
	ConsultationID    *uint   `json:"consultationId"`
	AppointmentID     *uint   `json:"appointmentId"`
	HospitalizationID *uint   `json:"hospitalizationId"`
	SourceType        string  `json:"sourceType"`
	SourceID          *uint   `json:"sourceId"`
}

// VoidRequest voids a PERFORMED act.
type VoidRequest struct {
	Reason string `json:"reason" binding:"required"`
}

// ListFilter bounds list queries.
type ListFilter struct {
	PatientID      uint
	Status         string
	Category       string
	ConsultationID uint
	PerformedFrom  string
	PerformedTo    string
	Page           int
	Limit          int
}

// Page is the paginated list response.
type Page struct {
	Data       []Act `json:"data"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"totalPages"`
}
