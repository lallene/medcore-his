package imaging

import "time"

type ListFilter struct {
	Page, Limit                                       int
	Status, Priority, Modality, Service, Search, Date string
	PatientID, ConsultationID                         *uint
	ServiceID                                         *uint
}

type ListResult struct {
	Data                    []ListItem
	Page, Limit, TotalPages int
	Total                   int64
}

type ScheduleRequest struct {
	ScheduledAt time.Time `json:"scheduledAt" binding:"required"`
	Comment     string    `json:"comment"`
}

type StartRequest struct {
	TechnicalNotes    string `json:"technicalNotes"`
	ContrastUsed      bool   `json:"contrastUsed"`
	ContrastProduct   string `json:"contrastProduct"`
	StudyInstanceUID  string `json:"studyInstanceUid"`
	ExternalViewerURL string `json:"externalViewerUrl"`
}

type ReportRequest struct {
	ClinicalIndication string `json:"clinicalIndication"`
	Technique          string `json:"technique"`
	Findings           string `json:"findings" binding:"required"`
	Conclusion         string `json:"conclusion" binding:"required"`
	Recommendation     string `json:"recommendation"`
	DocumentURL        string `json:"documentUrl"`
}

type CancelRequest struct {
	Reason string `json:"reason" binding:"required"`
}
