package dashboard

type DashboardResponse struct {
	Patients         int64          `json:"patients" gorm:"column:patients"`
	Insured          int64          `json:"insured" gorm:"column:insured"`
	Companies        int64          `json:"companies" gorm:"column:companies"`
	Guarantors       int64          `json:"guarantors" gorm:"column:guarantors"`
	Vouchers         int64          `json:"vouchers" gorm:"column:vouchers"`
	Validated        int64          `json:"validated" gorm:"column:validated"`
	Pending          int64          `json:"pending" gorm:"column:pending"`
	Rejected         int64          `json:"rejected" gorm:"column:rejected"`
	Workflow         WorkflowStats  `json:"workflow" gorm:"-"`
	RecentActivities []ActivityItem `json:"recentActivities" gorm:"-"`
}

type WorkflowStats struct {
	Draft      int64 `json:"draft" gorm:"column:draft"`
	Submitted  int64 `json:"submitted" gorm:"column:submitted"`
	Controlled int64 `json:"controlled" gorm:"column:controlled"`
	Validated  int64 `json:"validated" gorm:"column:validated"`
	Rejected   int64 `json:"rejected" gorm:"column:rejected"`
	Cancelled  int64 `json:"cancelled" gorm:"column:cancelled"`
}

type ActivityItem struct {
	Time        string `json:"time"`
	Description string `json:"description"`
	Type        string `json:"type"`
}
