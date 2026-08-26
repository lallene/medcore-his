package qa

type Filter struct {
	Environment, Status, DateFrom, DateTo, Suite string
	Page, Limit                                  int
}
type Page struct {
	Data []Campaign `json:"data"`
	Meta PageMeta   `json:"meta"`
}
type PageMeta struct {
	Page, Limit int
	Total       int64 `json:"total"`
	TotalPages  int   `json:"totalPages"`
}
type KPIs struct {
	LastCampaign *Campaign `json:"lastCampaign"`
	Campaigns    int64     `json:"campaigns"`
	Passed       int64     `json:"passed"`
	Failed       int64     `json:"failed"`
	PassRate     float64   `json:"passRate"`
}
