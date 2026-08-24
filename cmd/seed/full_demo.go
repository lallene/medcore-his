package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/modules/billing"
	"github.com/lallene/medcore-his/backend/internal/modules/cash"
	"github.com/lallene/medcore-his/backend/internal/modules/consultations"
	"github.com/lallene/medcore-his/backend/internal/modules/hospitalizations"
	"github.com/lallene/medcore-his/backend/internal/modules/imaging"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/authorization"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/company"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/coverage"
	"github.com/lallene/medcore-his/backend/internal/modules/insurance/guarantor"
	"github.com/lallene/medcore-his/backend/internal/modules/laboratory"
	"github.com/lallene/medcore-his/backend/internal/modules/medical_records"
	"github.com/lallene/medcore-his/backend/internal/modules/patients"
	"github.com/lallene/medcore-his/backend/internal/modules/pharmacy"
	"gorm.io/gorm"
)

// seedFullDemo enrichit exclusivement une base locale de démonstration. Chaque
// ressource possède une clé DEMO stable afin qu'un second passage soit sans effet.
func seedFullDemo(db *gorm.DB) {
	requireDemoDatabase(db)
	seedAdmin(db)
	var adminID uint
	db.Table("users").Select("id").Where("email = ?", "admin@medcore.local").Scan(&adminID)
	if adminID == 0 {
		log.Fatal("administrateur DEMO introuvable")
	}

	now := time.Now().UTC().Truncate(time.Second)
	patientsByCode := map[string]*patients.Patient{}
	profiles := []struct{ code, nom, prenoms, sexe string }{
		{"P-DEMO-001", "SANS-ASSURANCE", "Alice", "F"}, {"P-DEMO-002", "ALLIANZ", "Boris", "M"},
		{"P-DEMO-003", "CNAM", "Carole", "F"}, {"P-DEMO-004", "PEC-REFUSEE", "David", "M"},
		{"P-DEMO-005", "PEC-PARTIELLE", "Emma", "F"}, {"P-DEMO-006", "HOSPITALISE", "Fabrice", "M"},
		{"P-DEMO-007", "PHARMACIE", "Grace", "F"},
	}
	for i, p := range profiles {
		item := patients.Patient{CodePatient: p.code, NumeroDossier: "D-" + p.code, Nom: p.nom, Prenoms: p.prenoms, Sexe: p.sexe, Telephone: fmt.Sprintf("070000%04d", i+1), Quartier: "DEMO", IsAssure: i > 0}
		if err := db.Where("code_patient = ?", p.code).FirstOrCreate(&item).Error; err != nil {
			log.Fatal(err)
		}
		record := medical_records.MedicalRecord{PatientID: item.ID, RecordNumber: "MR-" + p.code, Status: "active"}
		if err := db.Where("patient_id = ?", item.ID).FirstOrCreate(&record).Error; err != nil {
			log.Fatal(err)
		}
		patientsByCode[p.code] = &item
	}

	companies := map[string]*company.InsuranceCompany{}
	for _, spec := range []struct {
		code, name string
		rate       float64
	}{{"DEMO-ALLIANZ", "Allianz Côte d’Ivoire DEMO", 90}, {"DEMO-CNAM", "CNAM DEMO", 80}, {"DEMO-NSIA", "NSIA DEMO", 70}} {
		c := company.InsuranceCompany{Code: spec.code, Name: spec.name, Description: "Assureur DEMO", Country: "Côte d'Ivoire", IsActive: true}
		if err := db.Where("code = ?", spec.code).FirstOrCreate(&c).Error; err != nil {
			log.Fatal(err)
		}
		g := guarantor.InsuranceGuarantor{CompanyID: c.ID, Code: spec.code + "-GAR", Name: spec.name + " Garant", DefaultCoverageRate: spec.rate, IsActive: true}
		if err := db.Where("company_id=? AND code=?", c.ID, g.Code).FirstOrCreate(&g).Error; err != nil {
			log.Fatal(err)
		}
		companies[spec.code] = &c
		for i, patientCode := range []string{"P-DEMO-002", "P-DEMO-003", "P-DEMO-004", "P-DEMO-005", "P-DEMO-006", "P-DEMO-007"} {
			if (spec.code == "DEMO-ALLIANZ" && i != 0 && i != 4) || (spec.code == "DEMO-CNAM" && i != 1 && i != 2 && i != 3) || (spec.code == "DEMO-NSIA" && i != 5) {
				continue
			}
			from, to := now.AddDate(0, -1, 0), now.AddDate(1, 0, 0)
			cov := coverage.PatientCoverage{PatientID: patientsByCode[patientCode].ID, CompanyID: c.ID, GuarantorID: g.ID, MemberNumber: "DEMO-MEMBER-" + patientCode + "-" + spec.code, Subscriber: "Souscripteur DEMO", Beneficiary: patientCode, CoverageRate: spec.rate, ValidFrom: &from, ValidTo: &to, IsPrincipal: true, IsActive: true}
			if err := db.Where("member_number = ?", cov.MemberNumber).FirstOrCreate(&cov).Error; err != nil {
				log.Fatal(err)
			}
		}
	}
	// Couverture secondaire active et couverture expirée pour les cas de sélection.
	addDemoCoverage(db, patientsByCode["P-DEMO-002"].ID, companies["DEMO-NSIA"].ID, "DEMO-MEMBER-SECONDARY", 70, false, now.AddDate(1, 0, 0))
	addDemoCoverage(db, patientsByCode["P-DEMO-003"].ID, companies["DEMO-ALLIANZ"].ID, "DEMO-MEMBER-EXPIRED", 90, false, now.AddDate(0, -1, 0))

	services := []string{"Urgences", "Médecine générale", "Cardiologie", "ORL", "Gynécologie", "Chirurgie", "Pharmacie"}
	consults := map[string]*consultations.Consultation{}
	for i, p := range profiles {
		key := "DEMO-CONSULTATION-" + p.code
		started := now.AddDate(0, 0, -i-1)
		c := consultations.Consultation{PatientID: patientsByCode[p.code].ID, DoctorName: "Dr Admin DEMO", Service: services[i], Status: consultations.ConsultationStatusCompleted, StartedAt: &started, CompletedAt: &started, Diagnosis: key, Observations: "Données de démonstration"}
		if err := db.Where("diagnosis = ?", key).FirstOrCreate(&c).Error; err != nil {
			log.Fatal(err)
		}
		consults[p.code] = &c
	}

	exams := map[string]*consultations.MedicalExam{}
	for _, e := range []struct{ code, name, category string }{{"DEMO-NFS", "NFS DEMO", "Laboratoire"}, {"DEMO-CRP", "CRP DEMO", "Laboratoire"}, {"DEMO-GLY", "Glycémie DEMO", "Laboratoire"}, {"DEMO-CREA", "Créatinine DEMO", "Laboratoire"}, {"DEMO-XRAY", "Radiographie thoracique DEMO", "Imagerie"}, {"DEMO-US", "Échographie abdominale DEMO", "Imagerie"}, {"DEMO-OBUS", "Échographie obstétricale DEMO", "Imagerie"}, {"DEMO-CT", "Scanner DEMO", "Imagerie"}} {
		x := consultations.MedicalExam{Code: e.code, Name: e.name, Category: e.category, IsActive: true}
		if err := db.Where("code = ?", e.code).FirstOrCreate(&x).Error; err != nil {
			log.Fatal(err)
		}
		exams[e.code] = &x
	}
	labStatuses := []string{laboratory.StatusOrdered, laboratory.StatusSampleCollected, laboratory.StatusInProgress, laboratory.StatusValidated}
	for i, code := range []string{"DEMO-NFS", "DEMO-CRP", "DEMO-GLY", "DEMO-CREA"} {
		p := profiles[(i+1)%len(profiles)].code
		c := consults[p]
		e := exams[code]
		db.Where("consultation_id=? AND medical_exam_id=?", c.ID, e.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: e.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: adminID})
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		o := laboratory.Order{RequestNumber: fmt.Sprintf("LAB-DEMO-%03d", i+1), ConsultationID: c.ID, MedicalExamID: e.ID, PatientID: c.PatientID, MedicalRecordID: &rec.ID, Priority: "ROUTINE", Status: labStatuses[i], PrescribedBy: adminID, CreatedBy: adminID, UpdatedBy: adminID}
		if err := db.Where("request_number=?", o.RequestNumber).FirstOrCreate(&o).Error; err != nil {
			log.Fatal(err)
		}
	}
	imgStatuses := []string{imaging.StatusOrdered, imaging.StatusScheduled, imaging.StatusInProgress, imaging.StatusValidated}
	for i, code := range []string{"DEMO-XRAY", "DEMO-US", "DEMO-OBUS", "DEMO-CT"} {
		p := profiles[(i+2)%len(profiles)].code
		c := consults[p]
		e := exams[code]
		db.Where("consultation_id=? AND medical_exam_id=?", c.ID, e.ID).FirstOrCreate(&consultations.ConsultationExamRequest{ConsultationID: c.ID, MedicalExamID: e.ID, Status: "requested", Priority: "ROUTINE", PrescribedBy: adminID})
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		o := imaging.Order{OrderNumber: fmt.Sprintf("IMG-DEMO-%03d", i+1), AccessionNumber: fmt.Sprintf("ACC-DEMO-%03d", i+1), ConsultationID: c.ID, MedicalExamID: e.ID, PatientID: c.PatientID, MedicalRecordID: &rec.ID, Modality: "DEMO", Priority: "ROUTINE", Status: imgStatuses[i], PrescribedBy: adminID, CreatedBy: adminID, UpdatedBy: adminID}
		if err := db.Where("order_number=?", o.OrderNumber).FirstOrCreate(&o).Error; err != nil {
			log.Fatal(err)
		}
	}
	for i, status := range []string{hospitalizations.StatusPlanned, hospitalizations.StatusAdmitted, hospitalizations.StatusDischarged} {
		p := profiles[i+4].code
		c := consults[p]
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", c.PatientID).First(&rec)
		h := hospitalizations.Hospitalization{PatientID: c.PatientID, MedicalRecordID: rec.ID, SourceConsultationID: c.ID, AdmissionNumber: fmt.Sprintf("HOSP-DEMO-%03d", i+1), HospitalizationType: "Médicale", AdmissionReason: "Séjour DEMO", Department: services[i], Status: status}
		if err := db.Where("admission_number=?", h.AdmissionNumber).FirstOrCreate(&h).Error; err != nil {
			log.Fatal(err)
		}
	}
	seedDemoAuthorizations(db, adminID)
	seedDemoBillingAndCash(db, adminID, consults)
	seedPharmacyCatalog(db)
	seedDemoPharmacyWorkflow(db, adminID, consults["P-DEMO-007"])
}

func addDemoCoverage(db *gorm.DB, patientID, companyID uint, member string, rate float64, principal bool, validTo time.Time) {
	var g guarantor.InsuranceGuarantor
	db.Where("company_id=?", companyID).First(&g)
	from := time.Now().AddDate(0, -2, 0)
	c := coverage.PatientCoverage{PatientID: patientID, CompanyID: companyID, GuarantorID: g.ID, MemberNumber: member, Subscriber: "DEMO", Beneficiary: "DEMO", CoverageRate: rate, ValidFrom: &from, ValidTo: &validTo, IsPrincipal: principal, IsActive: true}
	if err := db.Where("member_number=?", member).FirstOrCreate(&c).Error; err != nil {
		log.Fatal(err)
	}
}

func seedDemoAuthorizations(db *gorm.DB, user uint) {
	types := []string{"CONSULTATION", "IMAGING", "LABORATORY", "LABORATORY", "CONSULTATION"}
	statuses := []string{authorization.StatusPending, authorization.StatusApproved, authorization.StatusPartiallyApproved, authorization.StatusRejected, authorization.StatusApproved}
	requested := []float64{20000, 50000, 120000, 40000, 50000}
	for i := range types {
		var patient patients.Patient
		db.Where("code_patient=?", fmt.Sprintf("P-DEMO-%03d", i+2)).First(&patient)
		var rec medical_records.MedicalRecord
		db.Where("patient_id=?", patient.ID).First(&rec)
		var cov coverage.PatientCoverage
		db.Where("patient_id=? AND is_active", patient.ID).Order("is_principal DESC").First(&cov)
		var refID uint
		switch types[i] {
		case "CONSULTATION":
			db.Table("consultations").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		case "LABORATORY":
			db.Table("laboratory_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		case "IMAGING":
			db.Table("imaging_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&refID)
		}
		if refID == 0 || cov.ID == 0 {
			continue
		}
		number := fmt.Sprintf("PEC-DEMO-%03d", i+1)
		amount := requested[i]
		item := authorization.InsuranceAuthorization{AuthorizationNumber: number, PatientID: patient.ID, MedicalRecordID: rec.ID, PatientCoverageID: cov.ID, InsuranceCompanyID: cov.CompanyID, GuarantorID: cov.GuarantorID, ReferenceType: types[i], ReferenceID: refID, Service: "Service DEMO", RequestedAmount: &amount, RequestedAt: time.Now(), RequestedBy: user, Status: statuses[i], ExternalReference: "DEMO-EXT-" + number, Comment: "Scénario DEMO", CreatedBy: user, UpdatedBy: user}
		if statuses[i] == authorization.StatusApproved {
			rate, insured, patientAmount := 70.0, 35000.0, 15000.0
			item.ApprovedRate = &rate
			item.ApprovedAmount = &insured
			item.InsuranceAmount = &insured
			item.PatientAmount = &patientAmount
		}
		if statuses[i] == authorization.StatusPartiallyApproved {
			rate, insured, patientAmount, cap := 80.0, 70000.0, 50000.0, 70000.0
			item.ApprovedRate = &rate
			item.ApprovedAmount = &insured
			item.InsuranceAmount = &insured
			item.PatientAmount = &patientAmount
			item.CeilingAmount = &cap
		}
		if statuses[i] == authorization.StatusRejected {
			zero := 0.0
			item.InsuranceAmount = &zero
			item.PatientAmount = &amount
			item.RejectionReason = "Refus DEMO"
		}
		if err := db.Where("authorization_number=?", number).FirstOrCreate(&item).Error; err != nil {
			log.Fatal(err)
		}
		if i == 4 {
			var imagingID uint
			db.Table("imaging_orders").Select("id").Where("patient_id=?", patient.ID).Scan(&imagingID)
			if imagingID > 0 {
				link := authorization.InsuranceAuthorizationAct{InsuranceAuthorizationID: item.ID, PatientID: patient.ID, PatientCoverageID: cov.ID, ReferenceType: "IMAGING", ReferenceID: imagingID, RelationType: authorization.RelationCovered, IsActive: true, CreatedBy: user}
				if err := db.Where("patient_id=? AND patient_coverage_id=? AND reference_type='IMAGING' AND reference_id=? AND is_active", patient.ID, cov.ID, imagingID).FirstOrCreate(&link).Error; err != nil {
					log.Fatal(err)
				}
			}
		}
	}
}

func seedDemoBillingAndCash(db *gorm.DB, user uint, consults map[string]*consultations.Consultation) {
	billingService := billing.NewService(db)
	active := true
	today := time.Now().Format("2006-01-02")
	prices := []int64{10000, 15000, 8000, 6000, 40000, 35000, 120000, 5000}
	for i, typ := range []string{"CONSULTATION", "CONSULTATION", "LABORATORY", "LABORATORY", "IMAGING", "IMAGING", "HOSPITALIZATION", "MEDICATION"} {
		code := fmt.Sprintf("DEMO-TAR-%02d", i+1)
		var found int64
		db.Model(&billing.Tariff{}).Where("code=?", code).Count(&found)
		if found == 0 {
			if _, e := billingService.CreateTariff(billing.TariffRequest{ActType: typ, Code: code, Label: "Tarif " + code, UnitPrice: prices[i], EffectiveFrom: today, IsActive: &active}, user); e != nil {
				log.Fatal(e)
			}
		}
	}
	var register cash.Register
	cashService := cash.NewService(db)
	if db.Where("code=?", "DEMO-CAISSE-PRINCIPALE").First(&register).Error != nil {
		r, e := cashService.SaveRegister(0, cash.RegisterRequest{Code: "DEMO-CAISSE-PRINCIPALE", Name: "Caisse principale DEMO", Location: "Accueil", Active: &active}, user)
		if e != nil {
			log.Fatal(e)
		}
		register = *r
	}
	var open cash.Session
	if db.Where("cash_register_id=? AND status=?", register.ID, cash.SessionOpen).First(&open).Error != nil {
		s, e := cashService.Open(cash.OpenRequest{CashRegisterID: register.ID, OpeningFloat: 50000, Note: "Session DEMO ouverte"}, user)
		if e != nil {
			log.Fatal(e)
		}
		open = s.Session
	}
	for i, code := range []string{"P-DEMO-001", "P-DEMO-002", "P-DEMO-003", "P-DEMO-004", "P-DEMO-005", "P-DEMO-006", "P-DEMO-007"} {
		c := consults[code]
		var tariff billing.Tariff
		db.Where("code=?", fmt.Sprintf("DEMO-TAR-%02d", (i%2)+1)).First(&tariff)
		var line billing.InvoiceLine
		if db.Where("billable_key=?", fmt.Sprintf("CONSULTATION:%d", c.ID)).First(&line).Error == nil {
			continue
		}
		inv, e := billingService.CreateInvoice(billing.CreateInvoiceRequest{PatientID: c.PatientID, Lines: []billing.InvoiceLineRequest{{ActType: "CONSULTATION", ReferenceID: c.ID, TariffID: tariff.ID}}}, user)
		if e != nil {
			log.Fatal(e)
		}
		if i == 0 {
			continue
		}
		if inv.CoveragePending {
			// La facture reste DRAFT tant que la PEC DEMO est en attente.
			continue
		}
		inv, e = billingService.Issue(inv.ID, user)
		if e != nil {
			log.Fatal(e)
		}
		if i == 2 {
			_, e = cashService.Pay(open.ID, cash.PaymentRequest{InvoiceID: inv.ID, Amount: inv.BalanceAmount / 2, PaymentMethod: "CASH", IdempotencyKey: "DEMO-PAY-PARTIAL"}, user)
			if e != nil {
				log.Fatal(e)
			}
		}
		if i == 3 {
			_, e = cashService.Pay(open.ID, cash.PaymentRequest{InvoiceID: inv.ID, Amount: inv.BalanceAmount, PaymentMethod: "MOBILE_MONEY", MobileOperator: "Orange Money", ExternalReference: "DEMO-MM", IdempotencyKey: "DEMO-PAY-FULL"}, user)
			if e != nil {
				log.Fatal(e)
			}
		}
		if i == 4 {
			_, e = billingService.Cancel(inv.ID, "Annulation DEMO", user)
			if e != nil && !strings.Contains(e.Error(), "paiement") {
				log.Fatal(e)
			}
		}
		if i == 6 {
			quarter := inv.BalanceAmount / 4
			payments := []cash.PaymentRequest{
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "CARD", ExternalReference: "DEMO-CARD", IdempotencyKey: "DEMO-PAY-CARD"},
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "MOBILE_MONEY", MobileOperator: "MTN Money", ExternalReference: "DEMO-MM-2", IdempotencyKey: "DEMO-PAY-MM-2"},
				{InvoiceID: inv.ID, Amount: quarter, PaymentMethod: "BANK_TRANSFER", ExternalReference: "DEMO-BANK", IdempotencyKey: "DEMO-PAY-BANK"},
				{InvoiceID: inv.ID, Amount: inv.BalanceAmount - quarter*3, PaymentMethod: "CHECK", ExternalReference: "DEMO-CHECK", IdempotencyKey: "DEMO-PAY-CHECK"},
			}
			for _, payment := range payments {
				if _, e = cashService.Pay(open.ID, payment, user); e != nil {
					log.Fatal(e)
				}
			}
		}
	}
	var closed cash.Session
	if db.Where("cash_register_id=? AND status=? AND opening_note=?", register.ID, cash.SessionClosed, "Session DEMO historique").First(&closed).Error != nil {
		past := time.Now().AddDate(0, 0, -1)
		expected, counted, diff := int64(100000), int64(98000), int64(-2000)
		closed = cash.Session{CashRegisterID: register.ID, OpenedBy: user, OpenedAt: past, OpeningFloat: 100000, OpeningNote: "Session DEMO historique", Status: cash.SessionClosed, ClosedBy: &user, ClosedAt: &past, ExpectedCashAmount: &expected, CountedCashAmount: &counted, CashDifference: &diff, ClosingNote: "Écart DEMO de recette"}
		if e := db.Create(&closed).Error; e != nil {
			log.Fatal(e)
		}
	}
}

func seedDemoPharmacyWorkflow(db *gorm.DB, user uint, consultation *consultations.Consultation) {
	var presentation pharmacy.MedicationPresentation
	if err := db.Preload("Medication").Joins("JOIN pharmacy_stocks s ON s.presentation_id=medication_presentations.id AND s.is_stock_managed").Where("dosage=?", "1 g").Order("s.quantity_available DESC").First(&presentation).Error; err != nil {
		log.Fatal("présentation FEFO DEMO introuvable: ", err)
	}
	name := "DEMO Prescription pharmacie FEFO"
	prescription := consultations.ConsultationPrescription{ConsultationID: consultation.ID, PresentationID: &presentation.ID, MedicationName: name, Dosage: presentation.Dosage, Form: presentation.Form, Route: presentation.Route, Quantity: 12, Instructions: "8 unités puis reliquat de 4 — DEMO"}
	if err := db.Where("consultation_id=? AND medication_name=?", consultation.ID, name).FirstOrCreate(&prescription).Error; err != nil {
		log.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return pharmacy.MaterializeVoucher(tx, consultation.ID, &user) }); err != nil {
		log.Fatal(err)
	}
	service := pharmacy.NewService(pharmacy.NewRepository(db))
	patientID := consultation.PatientID
	if _, err := service.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: presentation.ID, PrescriptionID: &prescription.ID, PatientID: &patientID, Quantity: 8, Notes: "Première délivrance partielle DEMO", IdempotencyKey: "DEMO-PHARMACY-PARTIAL-8"}, user); err != nil {
		log.Fatal(err)
	}
	partial, err := service.GetPrescriptionDispensationStatus(prescription.ID)
	if err != nil {
		log.Fatal(err)
	}
	partialStatus := "PARTIAL"
	if partial.IsFullyDispensed {
		partialStatus = "COMPLETED (rejeu idempotent)"
	}
	log.Printf("Pharmacy DEMO après première clé: délivré %.0f / %.0f, reste %.0f, statut %s", partial.DispensedQuantity, partial.PrescribedQuantity, partial.RemainingQuantity, partialStatus)
	if _, err := service.CreateDispensation(pharmacy.CreateDispensationRequest{PresentationID: presentation.ID, PrescriptionID: &prescription.ID, PatientID: &patientID, Quantity: 4, Notes: "Délivrance du reliquat DEMO", IdempotencyKey: "DEMO-PHARMACY-REMAINDER-4"}, user); err != nil {
		log.Fatal(err)
	}
	completed, err := service.GetPrescriptionDispensationStatus(prescription.ID)
	if err != nil {
		log.Fatal(err)
	}
	finalStatus := "PARTIAL"
	if completed.IsFullyDispensed {
		finalStatus = "COMPLETED"
	}
	log.Printf("Pharmacy DEMO final: délivré %.0f / %.0f, reste %.0f, statut %s", completed.DispensedQuantity, completed.PrescribedQuantity, completed.RemainingQuantity, finalStatus)
}

func seedFullDemoPharmacy(db *gorm.DB) {
	requireDemoDatabase(db)
	var adminID uint
	db.Table("users").Select("id").Where("email=?", "admin@medcore.local").Scan(&adminID)
	if adminID == 0 {
		log.Fatal("administrateur DEMO introuvable")
	}
	var consultation consultations.Consultation
	if err := db.Joins("JOIN patients p ON p.id=consultations.patient_id").Where("p.code_patient=?", "P-DEMO-007").First(&consultation).Error; err != nil {
		log.Fatal("consultation Pharmacy DEMO introuvable: ", err)
	}
	seedDemoPharmacyWorkflow(db, adminID, &consultation)
}

func requireDemoDatabase(db *gorm.DB) {
	var name string
	if err := db.Raw("SELECT current_database()").Scan(&name).Error; err != nil {
		log.Fatal(err)
	}
	if name != "medcore_full_demo" && name != "medcore_lot12_demo" {
		log.Fatalf("seed DEMO refusé sur la base %q", name)
	}
}
