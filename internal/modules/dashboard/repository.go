package dashboard

import (
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"gorm.io/gorm"
)

type Repository interface {
	GetStats(filter DashboardFilter) (*DashboardResponse, error)
	GetRecentActivities(limit int, filter DashboardFilter) ([]ActivityItem, error)
}

type repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &repository{db: db}
}

func (r *repository) GetStats(filter DashboardFilter) (*DashboardResponse, error) {
	var result DashboardResponse

	activity, err := r.countActivity(filter)
	if err != nil {
		return nil, err
	}

	err = r.db.Raw(`
		SELECT
			(SELECT COUNT(*) FROM patients WHERE deleted_at IS NULL) AS patients,
			(SELECT COUNT(*) FROM patient_coverages WHERE deleted_at IS NULL AND is_active = true) AS insured,
			(SELECT COUNT(*) FROM insurance_companies WHERE deleted_at IS NULL) AS companies,
			(SELECT COUNT(*) FROM insurance_guarantors WHERE deleted_at IS NULL) AS guarantors
	`).Scan(&result).Error

	if err != nil {
		return nil, err
	}

	// Affecté après le Scan(&result), qui remet la structure à zéro.
	result.Consultations = activity.Consultations
	result.Hospitalizations = activity.Hospitalizations
	result.PharmacyPurchases = activity.PharmacyPurchases
	result.PatientsSeen = activity.PatientsSeen
	result.QueueTickets = activity.QueueTickets
	result.BilledAmount = activity.BilledAmount
	result.PatientReceivables = activity.PatientReceivables
	result.InsuranceReceivables = activity.InsuranceReceivables
	result.Refunds = activity.Refunds
	result.LabExams = activity.LabExams
	result.ImagingExams = activity.ImagingExams
	result.OtherActs = activity.OtherActs

	var vouchers voucherStats
	if err := insuranceVouchersQuery(r.db, filter).Select(`
			COUNT(*) AS total,
			COUNT(*) FILTER (WHERE iv.status = 'draft') AS draft,
			COUNT(*) FILTER (WHERE iv.status = 'submitted') AS submitted,
			COUNT(*) FILTER (WHERE iv.status = 'controlled') AS controlled,
			COUNT(*) FILTER (WHERE iv.status = 'validated') AS validated,
			COUNT(*) FILTER (WHERE iv.status = 'rejected') AS rejected,
			COUNT(*) FILTER (WHERE iv.status = 'cancelled') AS cancelled`).
		Scan(&vouchers).Error; err != nil {
		return nil, err
	}

	workflow := WorkflowStats{
		Draft:      vouchers.Draft,
		Submitted:  vouchers.Submitted,
		Controlled: vouchers.Controlled,
		Validated:  vouchers.Validated,
		Rejected:   vouchers.Rejected,
		Cancelled:  vouchers.Cancelled,
	}
	result.Vouchers = vouchers.Total
	result.Validated = vouchers.Validated
	result.Pending = vouchers.Submitted + vouchers.Controlled
	result.Rejected = vouchers.Rejected
	result.Workflow = workflow

	return &result, nil
}

func (r *repository) GetRecentActivities(limit int, filter DashboardFilter) ([]ActivityItem, error) {
	type row struct {
		OccurredAt   time.Time
		Description  string
		ActivityType string
	}

	var rows []row

	query := r.db.Table("workflow_histories AS wh").
		Select(`wh.occurred_at,
			CONCAT('Workflow ', wh.action, ' → ', wh.to_state, ' sur bon #', wh.entity_id) AS description,
			'workflow' AS activity_type`).
		Joins("JOIN insurance_vouchers AS iv ON iv.id = wh.entity_id").
		Joins("LEFT JOIN consultations AS c ON c.id = iv.consultation_id").
		Where("wh.entity_name = ?", "InsuranceVoucher")
	err := applyActivityFilter(query, filter, "wh.occurred_at", "c.service_id").
		Order("wh.occurred_at DESC").
		Limit(limit).
		Scan(&rows).Error

	if err != nil {
		return nil, err
	}

	activities := make([]ActivityItem, 0, len(rows))

	for _, item := range rows {
		activities = append(activities, ActivityItem{
			Time:        item.OccurredAt.Format("02/01 15:04"),
			Description: item.Description,
			Type:        item.ActivityType,
		})
	}

	return activities, nil
}

// applyActivityFilter applique le service et la période civile [from,to) ;
// les colonnes sont des constantes internes, jamais des entrées utilisateur.
func applyActivityFilter(query *gorm.DB, filter DashboardFilter, dateColumn, serviceColumn string) *gorm.DB {
	return filter.Apply(query, dateColumn, serviceColumn)
}

func consultationsQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	return applyActivityFilter(db.Table("consultations"), filter, "created_at", "service_id")
}

// Admissions de la période ; hospitalisations annulées exclues.
func hospitalizationsQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("hospitalizations").
		Where("deleted_at IS NULL").
		Where("status <> ?", "CANCELLED")
	return applyActivityFilter(query, filter, "admitted_at", "service_id")
}

// Ordonnances pharmacie (bons) ayant au moins une délivrance effective sur la
// période : un bon compte une fois, quel que soit son nombre de médicaments.
func pharmacyPurchasesQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("pharmacy_dispensations AS d").
		Joins("JOIN pharmacy_voucher_lines AS vl ON vl.prescription_id = d.reference_id").
		Joins("JOIN pharmacy_vouchers AS pv ON pv.id = vl.voucher_id").
		Joins("JOIN consultations AS c ON c.id = pv.consultation_id").
		Where("d.reference_type = ?", "CONSULTATION_PRESCRIPTION").
		Where("d.status = ?", "COMPLETED").
		Where("pv.cancelled_at IS NULL")
	return applyActivityFilter(query, filter, "d.created_at", "c.service_id").Distinct("vl.voucher_id")
}

// Patients distincts ayant eu au moins une consultation sur la période.
func patientsSeenQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	return consultationsQuery(db, filter).Distinct("patient_id")
}

// Tickets de file arrivés sur la période ; tickets annulés exclus.
func queueTicketsQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("patient_queue_tickets").Where("status <> ?", "CANCELLED")
	return applyActivityFilter(query, filter, "arrived_at", "service_id")
}

// Bons de prise en charge créés sur la période ; service de la consultation d'origine.
func insuranceVouchersQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("insurance_vouchers AS iv").
		Joins("LEFT JOIN consultations AS c ON c.id = iv.consultation_id").
		Where("iv.deleted_at IS NULL")
	return applyActivityFilter(query, filter, "iv.created_at", "c.service_id")
}

// Service d'une ligne de facture : consultation ou hospitalisation de l'acte,
// consultation de la prescription, ou hospitalisation facturée.
const invoiceLineServiceColumn = "COALESCE(pac.service_id, pah.service_id, cpc.service_id, h.service_id)"

// Jointures qui résolvent le service d'une ligne de facture « l ».
const invoiceLineServiceJoins = `
	LEFT JOIN performed_acts AS pa ON l.act_type = 'PERFORMED_ACT' AND pa.id = l.reference_id
	LEFT JOIN consultations AS pac ON pac.id = pa.consultation_id
	LEFT JOIN hospitalizations AS pah ON pah.id = pa.hospitalization_id
	LEFT JOIN consultation_prescriptions AS cp ON l.act_type = 'MEDICATION' AND cp.id = l.reference_id
	LEFT JOIN consultations AS cpc ON cpc.id = cp.consultation_id
	LEFT JOIN hospitalizations AS h ON l.act_type = 'HOSPITALIZATION' AND h.id = l.reference_id`

// Montant brut des lignes actives des factures émises (ni brouillon ni annulée),
// datées par leur émission.
func billedAmountQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("billing_invoice_lines AS l").
		Select("COALESCE(SUM(l.gross_amount), 0)").
		Joins("JOIN billing_invoices AS i ON i.id = l.invoice_id").
		Joins(invoiceLineServiceJoins).
		Where("l.is_active").
		Where("i.status NOT IN ?", []string{"CANCELLED", "DRAFT"})
	return applyActivityFilter(query, filter, "i.issued_at", invoiceLineServiceColumn)
}

// Solde patient restant d'une facture (règle de l'écran Créances patients).
const patientBalanceExpr = "GREATEST(i.patient_amount - COALESCE(pay.paid, 0) - COALESCE(cred.credited, 0) - COALESCE(capp.applied, 0), 0)"

// Factures créées sur la période dont le solde patient reste dû ; avec un
// service, factures ayant au moins une ligne active de ce service.
func patientReceivablesQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("billing_invoices AS i").
		Joins("LEFT JOIN ("+billing.EffectivePaidSubquery+") pay ON pay.invoice_id = i.id").
		Joins("LEFT JOIN ("+billing.EffectiveCreditedSubquery+") cred ON cred.invoice_id = i.id").
		Joins("LEFT JOIN ("+billing.EffectiveCreditAppliedSubquery+") capp ON capp.invoice_id = i.id").
		Where("i.status IN ?", []string{billing.InvoiceIssued, billing.InvoicePartiallyPaid}).
		Where("NOT i.coverage_pending").
		Where(patientBalanceExpr + " > 0")
	if filter.ServiceID != nil {
		query = query.Where("EXISTS (SELECT 1 FROM billing_invoice_lines AS l"+invoiceLineServiceJoins+
			" WHERE l.invoice_id = i.id AND l.is_active AND "+invoiceLineServiceColumn+" = ?)", *filter.ServiceID)
	}
	return DashboardFilter{From: filter.From, To: filter.To}.Apply(query, "i.created_at", "")
}

// Factures créées sur la période ayant une part assurance restant due (règle
// de l'écran Créances assurances) ; service de la ligne couverte.
func insuranceReceivablesQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("billing_invoice_lines AS l").
		Joins("JOIN billing_invoices AS i ON i.id = l.invoice_id").
		Joins("JOIN insurance_authorizations AS a ON a.id = l.authorization_id").
		Joins(`LEFT JOIN (SELECT al.invoice_line_id, SUM(al.amount) AS paid
			FROM insurance_settlement_allocations AS al
			JOIN insurance_settlements AS s ON s.id = al.settlement_id AND s.status = 'POSTED'
			GROUP BY al.invoice_line_id) AS sa ON sa.invoice_line_id = l.id`).
		Joins(invoiceLineServiceJoins).
		Where("l.is_active AND l.insurance_amount > 0").
		Where("l.coverage_resolution IN ?", []string{"DIRECT", "COVERED"}).
		Where("a.status IN ?", []string{"APPROVED", "PARTIALLY_APPROVED"}).
		Where("NOT i.coverage_pending").
		Where("i.status IN ?", []string{billing.InvoiceIssued, billing.InvoicePartiallyPaid, billing.InvoicePaid}).
		Where("l.insurance_amount - COALESCE(sa.paid, 0) > 0")
	return applyActivityFilter(query, filter, "i.created_at", invoiceLineServiceColumn).Distinct("l.invoice_id")
}

// Remboursements de solde patient exécutés sur la période.
func refundsQuery(db *gorm.DB, filter DashboardFilter) *gorm.DB {
	query := db.Table("billing_refunds AS r").
		Joins("JOIN billing_refund_executions AS e ON e.refund_id = r.id").
		Where("r.status = ?", "EXECUTED")
	return DashboardFilter{From: filter.From, To: filter.To}.Apply(query, "e.executed_at", "").Distinct("r.id")
}

// Actes réalisés non annulés des catégories données ; service de la
// consultation ou de l'hospitalisation d'origine.
func performedActsQuery(categories ...string) func(*gorm.DB, DashboardFilter) *gorm.DB {
	return func(db *gorm.DB, filter DashboardFilter) *gorm.DB {
		query := db.Table("performed_acts AS pa").
			Joins("LEFT JOIN consultations AS c ON c.id = pa.consultation_id").
			Joins("LEFT JOIN hospitalizations AS h ON h.id = pa.hospitalization_id").
			Where("pa.status = ? AND pa.voided_at IS NULL", "PERFORMED").
			Where("pa.act_category IN ?", categories)
		return applyActivityFilter(query, filter, "pa.performed_at", "COALESCE(c.service_id, h.service_id)")
	}
}

var (
	labExamsQuery     = performedActsQuery("LABORATORY")
	imagingExamsQuery = performedActsQuery("IMAGING")
	otherActsQuery    = performedActsQuery("PROCEDURE", "OTHER")
)

type voucherStats struct {
	Total      int64
	Draft      int64
	Submitted  int64
	Controlled int64
	Validated  int64
	Rejected   int64
	Cancelled  int64
}

type activityCounts struct {
	Consultations     int64
	Hospitalizations  int64
	PharmacyPurchases int64
	PatientsSeen      int64
	QueueTickets      int64
	BilledAmount      int64

	PatientReceivables   int64
	InsuranceReceivables int64
	Refunds              *int64
	LabExams             int64
	ImagingExams         int64
	OtherActs            int64
}

func (r *repository) countActivity(filter DashboardFilter) (activityCounts, error) {
	var counts activityCounts
	queries := []struct {
		build func(*gorm.DB, DashboardFilter) *gorm.DB
		dest  *int64
	}{
		{consultationsQuery, &counts.Consultations},
		{hospitalizationsQuery, &counts.Hospitalizations},
		{pharmacyPurchasesQuery, &counts.PharmacyPurchases},
		{patientsSeenQuery, &counts.PatientsSeen},
		{queueTicketsQuery, &counts.QueueTickets},
		{patientReceivablesQuery, &counts.PatientReceivables},
		{insuranceReceivablesQuery, &counts.InsuranceReceivables},
		{labExamsQuery, &counts.LabExams},
		{imagingExamsQuery, &counts.ImagingExams},
		{otherActsQuery, &counts.OtherActs},
	}
	for _, q := range queries {
		if err := q.build(r.db, filter).Count(q.dest).Error; err != nil {
			return activityCounts{}, err
		}
	}
	if err := billedAmountQuery(r.db, filter).Scan(&counts.BilledAmount).Error; err != nil {
		return activityCounts{}, err
	}
	if filter.ServiceID == nil {
		var refunds int64
		if err := refundsQuery(r.db, filter).Count(&refunds).Error; err != nil {
			return activityCounts{}, err
		}
		counts.Refunds = &refunds
	}
	return counts, nil
}
