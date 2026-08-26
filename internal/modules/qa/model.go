package qa

import "time"

const (
	StatusRunning        = "RUNNING"
	StatusPassed         = "PASSED"
	StatusFailed         = "FAILED"
	StatusCancelled      = "CANCELLED"
	ResultSkipped        = "SKIPPED"
	ResultNotImplemented = "NOT_IMPLEMENTED"
)

type Campaign struct {
	ID             uint         `gorm:"primaryKey" json:"id"`
	RunID          string       `gorm:"size:160;not null;uniqueIndex" json:"runId"`
	RunType        string       `gorm:"size:30;not null;default:'FULL';index" json:"runType"`
	CommitSHA      string       `gorm:"size:80;index" json:"commitSha"`
	Branch         string       `gorm:"size:160;index" json:"branch"`
	Environment    string       `gorm:"size:40;not null;index" json:"environment"`
	StartedAt      time.Time    `gorm:"not null;index" json:"startedAt"`
	FinishedAt     *time.Time   `json:"finishedAt"`
	DurationMS     int64        `gorm:"not null;default:0" json:"durationMs"`
	Total          int          `gorm:"not null;default:0" json:"total"`
	Passed         int          `gorm:"not null;default:0" json:"passed"`
	Failed         int          `gorm:"not null;default:0" json:"failed"`
	Skipped        int          `gorm:"not null;default:0" json:"skipped"`
	NotImplemented int          `gorm:"not null;default:0" json:"notImplemented"`
	Status         string       `gorm:"size:20;not null;index;check:qa_campaign_status_valid,status IN ('RUNNING','PASSED','FAILED','CANCELLED')" json:"status"`
	TriggeredBy    string       `gorm:"size:160" json:"triggeredBy"`
	CreatedAt      time.Time    `gorm:"not null" json:"createdAt"`
	Results        []TestResult `gorm:"foreignKey:CampaignID" json:"results,omitempty"`
	Artifacts      []Artifact   `gorm:"foreignKey:CampaignID" json:"artifacts,omitempty"`
}

func (Campaign) TableName() string { return "qa_campaigns" }

type TestResult struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	CampaignID   uint       `gorm:"not null;index;uniqueIndex:ux_qa_test_run" json:"campaignId"`
	Suite        string     `gorm:"size:100;not null;index" json:"suite"`
	TestKey      string     `gorm:"size:160;not null;uniqueIndex:ux_qa_test_run" json:"testKey"`
	Title        string     `gorm:"size:300;not null" json:"title"`
	Status       string     `gorm:"size:20;not null;index" json:"status"`
	DurationMS   int64      `gorm:"not null;default:0" json:"durationMs"`
	ErrorMessage string     `gorm:"type:text" json:"errorMessage,omitempty"`
	RetryCount   int        `gorm:"not null;default:0" json:"retryCount"`
	CreatedAt    time.Time  `gorm:"not null" json:"createdAt"`
	Artifacts    []Artifact `gorm:"foreignKey:TestResultID" json:"artifacts,omitempty"`
}

func (TestResult) TableName() string { return "qa_test_results" }

type Artifact struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	CampaignID   uint      `gorm:"not null;index" json:"campaignId"`
	TestResultID *uint     `gorm:"index" json:"testResultId"`
	Type         string    `gorm:"size:30;not null;index" json:"type"`
	Name         string    `gorm:"size:255;not null" json:"name"`
	Location     string    `gorm:"type:text;not null" json:"location"`
	CreatedAt    time.Time `gorm:"not null" json:"createdAt"`
}

func (Artifact) TableName() string { return "qa_artifacts" }
