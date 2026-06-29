package dashboard

import (
	"time"

	"gorm.io/gorm"
)

type Repository interface {
	GetStats() (*DashboardResponse, error)
	GetRecentActivities(limit int) ([]ActivityItem, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) GetStats() (*DashboardResponse, error) {
	var result DashboardResponse

	err := r.db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM patients WHERE deleted_at IS NULL) AS patients,
			(SELECT COUNT(*) FROM patient_coverages WHERE deleted_at IS NULL AND is_active = true) AS insured,
			(SELECT COUNT(*) FROM insurance_companies WHERE deleted_at IS NULL) AS companies,
			(SELECT COUNT(*) FROM insurance_guarantors WHERE deleted_at IS NULL) AS guarantors,
			(SELECT COUNT(*) FROM insurance_vouchers WHERE deleted_at IS NULL) AS vouchers,
			(SELECT COUNT(*) FROM insurance_vouchers WHERE deleted_at IS NULL AND status = 'validated') AS validated,
			(SELECT COUNT(*) FROM insurance_vouchers WHERE deleted_at IS NULL AND status IN ('submitted', 'controlled')) AS pending,
			(SELECT COUNT(*) FROM insurance_vouchers WHERE deleted_at IS NULL AND status = 'rejected') AS rejected
	`).Scan(&result).Error

	if err != nil {
		return nil, err
	}

	var workflow WorkflowStats

	err = r.db.Raw(`
		SELECT
			COUNT(*) FILTER (WHERE status = 'draft') AS draft,
			COUNT(*) FILTER (WHERE status = 'submitted') AS submitted,
			COUNT(*) FILTER (WHERE status = 'controlled') AS controlled,
			COUNT(*) FILTER (WHERE status = 'validated') AS validated,
			COUNT(*) FILTER (WHERE status = 'rejected') AS rejected,
			COUNT(*) FILTER (WHERE status = 'cancelled') AS cancelled
		FROM insurance_vouchers
		WHERE deleted_at IS NULL
	`).Scan(&workflow).Error

	if err != nil {
		return nil, err
	}

	result.Workflow = workflow

	return &result, nil
}

func (r *repository) GetRecentActivities(limit int) ([]ActivityItem, error) {
	type row struct {
		OccurredAt   time.Time
		Description  string
		ActivityType string
	}

	var rows []row

	err := r.db.Raw(`
		SELECT
			occurred_at,
			CONCAT('Workflow ', action, ' → ', to_state, ' sur bon #', entity_id) AS description,
			'workflow' AS activity_type
		FROM workflow_histories
		WHERE entity_name = 'InsuranceVoucher'
		ORDER BY occurred_at DESC
		LIMIT ?
	`, limit).Scan(&rows).Error

	if err != nil {
		return nil, err
	}

	activities := make([]ActivityItem, 0, len(rows))

	for _, item := range rows {
		activities = append(activities, ActivityItem{
			Time:        item.OccurredAt.Format("15:04"),
			Description: item.Description,
			Type:        item.ActivityType,
		})
	}

	return activities, nil
}
