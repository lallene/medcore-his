package qa

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"
)

type SummaryArtifact struct{ Type, Name, Location string }
type SummaryTest struct {
	Key, Suite, Title, Status string
	Duration                  int64
	Error                     *string
	RetryCount                int
	Artifacts                 []SummaryArtifact
}
type Summary struct {
	RunID, CommitSHA, Branch, Environment, StartedAt, FinishedAt, Status, TriggeredBy string
	RunType                                                                           string `json:"type"`
	Duration                                                                          int64
	Total, Passed, Failed, Skipped, NotImplemented                                    int
	Tests                                                                             []SummaryTest
	Artifacts                                                                         []SummaryArtifact
}

var campaignStatuses = map[string]bool{StatusRunning: true, StatusPassed: true, StatusFailed: true, StatusCancelled: true}
var resultStatuses = map[string]bool{StatusPassed: true, StatusFailed: true, ResultSkipped: true, ResultNotImplemented: true}

func validateSummary(x *Summary) error {
	x.Status = strings.ToUpper(strings.TrimSpace(x.Status))
	x.RunType = strings.ToUpper(strings.TrimSpace(x.RunType))
	if x.RunType == "" {
		x.RunType = "FULL"
	}
	if x.RunType != "SMOKE" && x.RunType != "CRITICAL" && x.RunType != "FULL" && x.RunType != "PRODUCTION-SMOKE" {
		return fmt.Errorf("type campagne invalide: %q", x.RunType)
	}
	if !campaignStatuses[x.Status] {
		return fmt.Errorf("status campagne invalide: %q", x.Status)
	}
	if x.Total < 0 || x.Passed < 0 || x.Failed < 0 || x.Skipped < 0 || x.NotImplemented < 0 {
		return fmt.Errorf("compteurs QA négatifs interdits")
	}
	if x.Total != x.Passed+x.Failed+x.Skipped+x.NotImplemented || x.Total != len(x.Tests) {
		return fmt.Errorf("compteurs QA incohérents")
	}
	if x.Status == StatusPassed && (x.Failed > 0 || x.NotImplemented > 0) {
		return fmt.Errorf("une campagne avec FAIL ou NOT_IMPLEMENTED ne peut pas être PASSED")
	}
	keys := make(map[string]bool, len(x.Tests))
	counts := map[string]int{}
	for i := range x.Tests {
		t := &x.Tests[i]
		t.Status = strings.ToUpper(strings.TrimSpace(t.Status))
		if strings.TrimSpace(t.Key) == "" || strings.TrimSpace(t.Suite) == "" || strings.TrimSpace(t.Title) == "" {
			return fmt.Errorf("test QA incomplet")
		}
		if keys[t.Key] {
			return fmt.Errorf("testKey dupliquée: %s", t.Key)
		}
		keys[t.Key] = true
		if !resultStatuses[t.Status] {
			return fmt.Errorf("status résultat invalide: %q", t.Status)
		}
		counts[t.Status]++
	}
	if counts[StatusPassed] != x.Passed || counts[StatusFailed] != x.Failed || counts[ResultSkipped] != x.Skipped || counts[ResultNotImplemented] != x.NotImplemented {
		return fmt.Errorf("résultats et compteurs QA incohérents")
	}
	return nil
}

func ReadSummary(path string) (*Summary, error) {
	data, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var x Summary
	if e = json.Unmarshal(data, &x); e != nil {
		return nil, e
	}
	if strings.TrimSpace(x.RunID) == "" {
		return nil, fmt.Errorf("runId obligatoire")
	}
	if strings.TrimSpace(x.RunType) == "" {
		x.RunType = "FULL"
	}
	return &x, nil
}
func ImportSummary(db *gorm.DB, x *Summary) error {
	if e := validateSummary(x); e != nil {
		return e
	}
	started, e := time.Parse(time.RFC3339, x.StartedAt)
	if e != nil {
		return fmt.Errorf("startedAt invalide: %w", e)
	}
	var finished *time.Time
	if x.FinishedAt != "" {
		v, e := time.Parse(time.RFC3339, x.FinishedAt)
		if e != nil {
			return fmt.Errorf("finishedAt invalide: %w", e)
		}
		finished = &v
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var campaign Campaign
		e := tx.Where("run_id=?", x.RunID).First(&campaign).Error
		if e != nil && e != gorm.ErrRecordNotFound {
			return e
		}
		campaign.RunID = x.RunID
		campaign.RunType = strings.ToUpper(x.RunType)
		campaign.CommitSHA = x.CommitSHA
		campaign.Branch = x.Branch
		campaign.Environment = x.Environment
		campaign.StartedAt = started
		campaign.FinishedAt = finished
		campaign.DurationMS = x.Duration
		campaign.Total = x.Total
		campaign.Passed = x.Passed
		campaign.Failed = x.Failed
		campaign.Skipped = x.Skipped
		campaign.NotImplemented = x.NotImplemented
		campaign.Status = strings.ToUpper(x.Status)
		campaign.TriggeredBy = x.TriggeredBy
		if campaign.ID == 0 {
			if e = tx.Create(&campaign).Error; e != nil {
				return e
			}
		} else {
			if e = tx.Save(&campaign).Error; e != nil {
				return e
			}
			if e = tx.Where("campaign_id=?", campaign.ID).Delete(&Artifact{}).Error; e != nil {
				return e
			}
			if e = tx.Where("campaign_id=?", campaign.ID).Delete(&TestResult{}).Error; e != nil {
				return e
			}
		}
		for _, item := range x.Tests {
			r := TestResult{CampaignID: campaign.ID, Suite: item.Suite, TestKey: item.Key, Title: item.Title, Status: strings.ToUpper(item.Status), DurationMS: item.Duration, RetryCount: item.RetryCount}
			if item.Error != nil {
				r.ErrorMessage = *item.Error
			}
			if e = tx.Create(&r).Error; e != nil {
				return e
			}
			for _, a := range item.Artifacts {
				if e = tx.Create(&Artifact{CampaignID: campaign.ID, TestResultID: &r.ID, Type: strings.ToUpper(a.Type), Name: a.Name, Location: a.Location}).Error; e != nil {
					return e
				}
			}
		}
		for _, a := range x.Artifacts {
			if e = tx.Create(&Artifact{CampaignID: campaign.ID, Type: strings.ToUpper(a.Type), Name: a.Name, Location: a.Location}).Error; e != nil {
				return e
			}
		}
		return nil
	})
}
