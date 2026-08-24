package laboratory

type ListFilter struct {
	Page, Limit                        int
	Status, Priority, Category, Search string
	PatientID, ConsultationID          *uint
	ServiceID                          *uint
}
type ListResult struct {
	Data       []ListItem `json:"data"`
	Page       int
	Limit      int
	Total      int64
	TotalPages int
}
type CollectRequest struct {
	SampleType string `json:"sampleType" binding:"required"`
	Comment    string `json:"comment"`
}
type ResultInput struct {
	Parameter     string   `json:"parameter" binding:"required"`
	Value         string   `json:"value" binding:"required"`
	Unit          string   `json:"unit"`
	ReferenceMin  *float64 `json:"referenceMin"`
	ReferenceMax  *float64 `json:"referenceMax"`
	ReferenceText string   `json:"referenceText"`
	CriticalMin   *float64 `json:"criticalMin"`
	CriticalMax   *float64 `json:"criticalMax"`
	Comment       string   `json:"comment"`
}
type EnterResultsRequest struct {
	Results []ResultInput `json:"results" binding:"required,min=1"`
}
type CancelRequest struct {
	Reason string `json:"reason" binding:"required"`
}
