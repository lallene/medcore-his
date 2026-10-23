package complete

import (
	"github.com/lallene/medcore-his/backend/internal/core/workflow"
	"github.com/lallene/medcore-his/backend/internal/modules/act_catalog"
	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/voucher"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance_receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patient_queue"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/performed_acts"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"github.com/lallene/medcore-his/backend/internal/modules/receivables"
	"github.com/lallene/medcore-his/backend/internal/modules/ticketing"
)

// Services mirrors the production wiring of cmd/api (module.go files), with the
// PerformedAct producers enabled: the seeder exercises the same code paths as the API.
type Services struct {
	Patients    patients.Service
	MedRec      medical_records.Service
	Consult     *consultations.Service
	Queue       *patient_queue.Service
	Lab         *laboratory.Service
	LabRepo     *laboratory.Repository
	Imaging     *imaging.Service
	ImagingRepo *imaging.Repository
	Hosp        *hospitalizations.Service
	Pharmacy    *pharmacy.Service
	Acts        *performed_acts.Service
	ActCatalog  *act_catalog.Service
	Billing     *billing.Service
	Cash        *cash.Service
	Auth        *authorization.Service
	Company     company.Service
	Guarantor   guarantor.Service
	Coverage    coverage.Service
	Voucher     voucher.Service
	InsRec      *insurance_receivables.Service
	Receivables *receivables.Service
	Ticketing   *ticketing.Service
}

func newServices(w *World) *Services {
	db := w.DB
	acts := performed_acts.NewService(db).WithProducersEnabled(true)
	medRec := medical_records.NewService(medical_records.NewRepository(db))
	labRepo := laboratory.NewRepository(db)
	imgRepo := imaging.NewRepository(db)
	covRepo := coverage.NewRepository(db)
	return &Services{
		Patients: patients.NewService(patients.NewRepository(db)),
		MedRec:   medRec,
		Consult:  consultations.NewService(consultations.NewRepository(db), medRec).WithPerformedActs(acts),
		Queue: patient_queue.NewService(db).WithNotificationLifecycleConfig(patient_queue.NotificationLifecycleConfig{EmailEnabled: true}).
			WithMedicalRecords(medRec).WithPerformedActs(acts),
		Lab:         laboratory.NewService(labRepo).WithPerformedActs(acts),
		LabRepo:     labRepo,
		Imaging:     imaging.NewService(imgRepo).WithPerformedActs(acts),
		ImagingRepo: imgRepo,
		Hosp:        hospitalizations.NewService(db, hospitalizations.NewRepository(db)),
		Pharmacy:    pharmacy.NewService(pharmacy.NewRepository(db)),
		Acts:        acts,
		ActCatalog:  act_catalog.NewService(db),
		Billing:     billing.NewService(db),
		Cash:        cash.NewService(db),
		Auth:        authorization.NewService(db),
		Company:     company.NewService(company.NewRepository(db)),
		Guarantor:   guarantor.NewService(guarantor.NewRepository(db)),
		Coverage:    coverage.NewService(covRepo),
		Voucher:     voucher.NewService(voucher.NewRepository(db), db, covRepo),
		InsRec:      insurance_receivables.NewService(db, w.Cfg.Location),
		Receivables: receivables.NewService(db, w.Cfg.Location),
		Ticketing:   ticketing.NewService(db),
	}
}

// permMap turns effective permissions into the map form used by module Access structs.
func (m *Member) permMap() map[string]bool {
	out := make(map[string]bool, len(m.Perms))
	for _, p := range m.Perms {
		out[p] = true
	}
	return out
}

func (m *Member) QueueAccess(w *World) patient_queue.Access {
	a := patient_queue.Access{UserID: m.UserID, Permissions: m.permMap()}
	if s := m.PrimaryService(); s != "" {
		id := w.Org.ServiceID(s)
		a.ServiceID = &id
	}
	return a
}
func (m *Member) ConsultAccess() consultations.Access {
	return consultations.Access{UserID: m.UserID, Permissions: m.permMap()}
}
func (m *Member) LabAccess() laboratory.Access {
	return laboratory.Access{UserID: m.UserID, Permissions: m.permMap()}
}
func (m *Member) ImagingAccess() imaging.Access {
	return imaging.Access{UserID: m.UserID, Permissions: m.permMap()}
}
func (m *Member) HospAccess() hospitalizations.Access {
	return hospitalizations.Access{UserID: m.UserID, Permissions: m.permMap()}
}
func (m *Member) AuthAccess() authorization.Access {
	return authorization.Access{UserID: m.UserID, Permissions: m.permMap()}
}
func (m *Member) TicketAccess(w *World) ticketing.Access {
	a := ticketing.Access{UserID: m.UserID, Permissions: m.permMap()}
	if s := m.PrimaryService(); s != "" {
		id := w.Org.ServiceID(s)
		a.ServiceID = &id
	}
	return a
}
func (m *Member) WorkflowActor() workflow.Actor {
	return workflow.Actor{UserID: m.UserID, Role: m.Role, Permissions: m.Perms}
}
