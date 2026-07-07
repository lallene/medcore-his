package consultations

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/lallene/medcore-his/backend/internal/core/branding"
	"golang.org/x/text/encoding/charmap"
)

func pdfText(value string) string {
	out, err := charmap.ISO8859_1.NewEncoder().String(value)
	if err != nil {
		return value
	}
	return out
}

func patientFullName(c *Consultation) string {
	name := strings.TrimSpace(c.Patient.Nom + " " + c.Patient.Prenoms)
	if name == "" {
		return fmt.Sprintf("Patient #%d", c.PatientID)
	}

	return name
}

func formatDatePDF(value *time.Time) string {
	if value == nil {
		return "-"
	}

	return value.Format("02/01/2006")
}

func GenerateSickLeavePDF(c *Consultation) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(190, 10, pdfText("FICHE DE REPOS MALADIE"))
	pdf.Ln(15)

	pdf.SetFont("Helvetica", "", 12)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Consultation N° : %d", c.ID)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Patient : %s", patientFullName(c))))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Médecin : %s", c.DoctorName)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Service : %s", c.Service)))
	pdf.Ln(12)

	if !c.SickLeaveRequired {
		pdf.MultiCell(190, 8, pdfText("Aucun repos maladie n'a été prescrit pour cette consultation."), "", "", false)
	} else {
		pdf.Cell(190, 8, pdfText(fmt.Sprintf("Durée du repos : %d jour(s)", c.SickLeaveDays)))
		pdf.Ln(8)

		pdf.Cell(190, 8, pdfText(fmt.Sprintf("Date de début : %s", formatDatePDF(c.SickLeaveStartDate))))
		pdf.Ln(8)

		pdf.Cell(190, 8, pdfText(fmt.Sprintf("Date de fin : %s", formatDatePDF(c.SickLeaveEndDate))))
		pdf.Ln(12)
	}

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(190, 8, pdfText("Diagnostic"))
	pdf.Ln(8)

	pdf.SetFont("Helvetica", "", 12)
	pdf.MultiCell(190, 8, pdfText(c.Diagnosis), "", "", false)
	pdf.Ln(5)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(190, 8, pdfText("Observations"))
	pdf.Ln(8)

	pdf.SetFont("Helvetica", "", 12)
	pdf.MultiCell(190, 8, pdfText(c.Observations), "", "", false)
	pdf.Ln(15)

	pdf.Cell(190, 8, pdfText("Fait le : "+time.Now().Format("02/01/2006 15:04")))
	pdf.Ln(20)

	pdf.Cell(190, 8, pdfText("Signature et cachet du médecin"))
	pdf.Ln(15)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}

func GenerateExamRequestPDF(c *Consultation) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(190, 10, pdfText("DEMANDE / AUTORISATION D'EXAMENS"))
	pdf.Ln(15)

	pdf.SetFont("Helvetica", "", 12)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Consultation N° : %d", c.ID)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Patient : %s", patientFullName(c))))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Médecin : %s", c.DoctorName)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Service : %s", c.Service)))
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(190, 8, pdfText("Examens demandés"))
	pdf.Ln(10)

	pdf.SetFont("Helvetica", "", 12)

	if len(c.Exams) == 0 {
		pdf.Cell(190, 8, pdfText("Aucun examen demandé."))
		pdf.Ln(8)
	} else {
		for _, exam := range c.Exams {
			line := fmt.Sprintf("- %s (%s)", exam.Name, exam.Category)
			pdf.Cell(190, 8, pdfText(line))
			pdf.Ln(8)
		}
	}

	pdf.Ln(8)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(190, 8, pdfText("Diagnostic / Renseignement clinique"))
	pdf.Ln(8)

	pdf.SetFont("Helvetica", "", 12)
	pdf.MultiCell(190, 8, pdfText(c.Diagnosis), "", "", false)
	pdf.Ln(8)

	if len(c.Reasons) > 0 {
		var reasonNames []string
		for _, reason := range c.Reasons {
			reasonNames = append(reasonNames, reason.Name)
		}

		pdf.SetFont("Helvetica", "", 12)
		pdf.MultiCell(190, 8, pdfText(strings.Join(reasonNames, ", ")), "", "", false)
		pdf.Ln(8)
	}

	pdf.Cell(190, 8, pdfText("Fait le : "+time.Now().Format("02/01/2006 15:04")))
	pdf.Ln(20)

	pdf.Cell(190, 8, pdfText("Signature et cachet du médecin"))
	pdf.Ln(15)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}

func GeneratePrescriptionPDF(c *Consultation) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(true, 28)
	pdf.AddPage()

	reference := branding.DocumentReference(
		branding.DocumentTypePrescription,
		c.ID,
		c.CreatedAt,
	)

	drawClinicHeader(pdf)
	drawDocumentTitle(pdf, "Ordonnance", reference)

	addLine := func(label string, value string) {
		if value == "" {
			value = "-"
		}

		pdf.SetFont("Arial", "B", 9)
		pdf.SetTextColor(
			branding.Clinic.Muted.R,
			branding.Clinic.Muted.G,
			branding.Clinic.Muted.B,
		)
		pdf.CellFormat(48, 6, pdfText(label), "", 0, "L", false, 0, "")

		pdf.SetFont("Arial", "", 9)
		setPDFTextColor(pdf)
		pdf.MultiCell(142, 6, pdfText(value), "", "L", false)
	}

	drawPDFSectionTitle(pdf, "Informations du patient")
	addLine("Nom et prénoms :", patientFullName(c))
	addLine("Date de naissance / âge :", patientBirthOrAge(c))

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		addLine("Matricule assuré :", c.Patient.MatriculeAssure)
	}

	drawPDFSectionTitle(pdf, "Informations de prescription")
	addLine("Médecin :", c.DoctorName)
	addLine("Service :", c.Service)
	addLine("Date :", formatDateTimePDF(time.Now()))

	drawPDFSectionTitle(pdf, "Médicaments prescrits")

	if len(c.Prescriptions) == 0 {
		pdf.SetFont("Arial", "", 9)
		setPDFTextColor(pdf)
		pdf.MultiCell(190, 6, pdfText("Aucune prescription renseignée."), "", "L", false)
	} else {
		for index, p := range c.Prescriptions {
			pdf.SetFillColor(245, 248, 252)
			pdf.SetDrawColor(
				branding.Clinic.Border.R,
				branding.Clinic.Border.G,
				branding.Clinic.Border.B,
			)

			startY := pdf.GetY()
			pdf.Rect(10, startY, 190, 9, "FD")

			pdf.SetXY(13, startY+2)
			pdf.SetFont("Arial", "B", 10)
			pdf.SetTextColor(
				branding.Clinic.Primary.R,
				branding.Clinic.Primary.G,
				branding.Clinic.Primary.B,
			)

			pdf.CellFormat(
				184,
				5,
				pdfText(fmt.Sprintf("%d. %s", index+1, p.MedicationName)),
				"",
				1,
				"L",
				false,
				0,
				"",
			)

			pdf.Ln(3)

			if p.Dosage != "" {
				addLine("Dosage :", p.Dosage)
			}

			if p.Form != "" {
				addLine("Forme :", p.Form)
			}

			if p.Frequency != "" {
				addLine("Fréquence :", p.Frequency)
			}

			if p.Duration != "" {
				addLine("Durée :", p.Duration)
			}

			if p.Route != "" {
				addLine("Voie :", p.Route)
			}

			if p.Instructions != "" {
				addLine("Instructions :", p.Instructions)
			}

			pdf.Ln(4)
		}
	}

	pdf.Ln(5)

	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(
		branding.Clinic.Muted.R,
		branding.Clinic.Muted.G,
		branding.Clinic.Muted.B,
	)
	pdf.CellFormat(
		190,
		5,
		pdfText("Document généré le : "+time.Now().Format("02/01/2006 15:04")),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawSignatureArea(pdf, c.DoctorName)
	drawClinicFooter(pdf)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}

func consultationStatusLabel(status string) string {
	switch status {
	case "draft":
		return "Brouillon"
	case "in_progress":
		return "En cours"
	case "completed":
		return "Terminée"
	case "cancelled":
		return "Annulée"
	default:
		return status
	}
}

func formatDateTimePDF(value time.Time) string {
	return value.Local().Format("02/01/2006 15:04")
}

func hasConsultationVitals(c *Consultation) bool {
	return c.Vitals.Temperature != nil ||
		c.Vitals.BloodPressureSystolic != nil ||
		c.Vitals.BloodPressureDiastolic != nil ||
		c.Vitals.HeartRate != nil ||
		c.Vitals.RespiratoryRate != nil ||
		c.Vitals.OxygenSaturation != nil ||
		c.Vitals.Weight != nil ||
		c.Vitals.Height != nil ||
		c.Vitals.BloodGlucose != nil ||
		c.Vitals.PainScore != nil
}

func GenerateConsultationReportPDF(c *Consultation) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(10, 10, 10)
	pdf.SetAutoPageBreak(true, 28)
	pdf.AddPage()

	reference := branding.DocumentReference(
		branding.DocumentTypeConsultationReport,
		c.ID,
		c.CreatedAt,
	)

	drawClinicHeader(pdf)

	drawDocumentTitle(
		pdf,
		"Compte rendu de consultation",
		reference,
	)

	addLine := func(label string, value string) {
		if value == "" {
			value = "-"
		}

		pdf.SetFont("Arial", "B", 9)
		pdf.SetTextColor(
			branding.Clinic.Muted.R,
			branding.Clinic.Muted.G,
			branding.Clinic.Muted.B,
		)
		pdf.CellFormat(
			48,
			6,
			pdfText(label),
			"",
			0,
			"L",
			false,
			0,
			"",
		)

		pdf.SetFont("Arial", "", 9)
		setPDFTextColor(pdf)
		pdf.MultiCell(
			142,
			6,
			pdfText(value),
			"",
			"L",
			false,
		)
	}

	addParagraph := func(value string) {
		if value == "" {
			value = "-"
		}

		pdf.SetFont("Arial", "", 9)
		setPDFTextColor(pdf)
		pdf.MultiCell(190, 6, pdfText(value), "", "L", false)
	}

	drawPDFSectionTitle(pdf, "Informations du patient")
	addLine("Nom et prénoms :", patientFullName(c))
	addLine("Date de naissance / âge :", patientBirthOrAge(c))

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		addLine("Matricule assuré :", c.Patient.MatriculeAssure)
	}

	drawPDFSectionTitle(pdf, "Informations de la consultation")
	addLine("Médecin :", c.DoctorName)
	addLine("Service :", c.Service)
	addLine("Statut :", consultationStatusLabel(c.Status))
	addLine("Date de création :", formatDateTimePDF(c.CreatedAt))

	if c.StartedAt != nil {
		addLine("Débutée le :", formatDateTimePDF(*c.StartedAt))
	}

	if c.CompletedAt != nil {
		addLine("Terminée le :", formatDateTimePDF(*c.CompletedAt))
	}

	if c.CancelledAt != nil {
		addLine("Annulée le :", formatDateTimePDF(*c.CancelledAt))

		if c.CancellationReason != "" {
			addLine("Motif d'annulation :", c.CancellationReason)
		}
	}

	if hasConsultationVitals(c) {
		drawPDFSectionTitle(pdf, "Constantes vitales")

		if c.Vitals.Temperature != nil {
			addLine("Température :", fmt.Sprintf("%.1f °C", *c.Vitals.Temperature))
		}

		if c.Vitals.BloodPressureSystolic != nil &&
			c.Vitals.BloodPressureDiastolic != nil {
			addLine(
				"Tension artérielle :",
				fmt.Sprintf(
					"%d/%d mmHg",
					*c.Vitals.BloodPressureSystolic,
					*c.Vitals.BloodPressureDiastolic,
				),
			)
		}

		if c.Vitals.HeartRate != nil {
			addLine("Fréquence cardiaque :", fmt.Sprintf("%d bpm", *c.Vitals.HeartRate))
		}

		if c.Vitals.RespiratoryRate != nil {
			addLine(
				"Fréquence respiratoire :",
				fmt.Sprintf("%d cycles/min", *c.Vitals.RespiratoryRate),
			)
		}

		if c.Vitals.OxygenSaturation != nil {
			addLine("Saturation O2 :", fmt.Sprintf("%d %%", *c.Vitals.OxygenSaturation))
		}

		if c.Vitals.Weight != nil {
			addLine("Poids :", fmt.Sprintf("%.1f kg", *c.Vitals.Weight))
		}

		if c.Vitals.Height != nil {
			addLine("Taille :", fmt.Sprintf("%.1f cm", *c.Vitals.Height))
		}

		if c.Vitals.BloodGlucose != nil {
			addLine("Glycémie :", fmt.Sprintf("%.2f", *c.Vitals.BloodGlucose))
		}

		if c.Vitals.PainScore != nil {
			addLine("Score douleur :", fmt.Sprintf("%d / 10", *c.Vitals.PainScore))
		}
	}

	drawPDFSectionTitle(pdf, "Diagnostic")
	addParagraph(c.Diagnosis)

	drawPDFSectionTitle(pdf, "Observations")
	addParagraph(c.Observations)

	drawPDFSectionTitle(pdf, "Traitement")
	addParagraph(c.Treatment)

	drawPDFSectionTitle(pdf, "Examens demandés")

	if len(c.Exams) == 0 {
		addParagraph("Aucun examen demandé.")
	} else {
		for _, exam := range c.Exams {
			addParagraph(fmt.Sprintf("- %s (%s)", exam.Name, exam.Category))
		}
	}

	drawPDFSectionTitle(pdf, "Prescriptions")

	if len(c.Prescriptions) == 0 {
		addParagraph("Aucune prescription renseignée.")
	} else {
		for index, prescription := range c.Prescriptions {
			pdf.SetFont("Arial", "B", 9)
			setPDFTextColor(pdf)
			pdf.CellFormat(
				190,
				6,
				pdfText(fmt.Sprintf("%d. %s", index+1, prescription.MedicationName)),
				"",
				1,
				"L",
				false,
				0,
				"",
			)

			if prescription.Dosage != "" {
				addLine("Dosage :", prescription.Dosage)
			}

			if prescription.Form != "" {
				addLine("Forme :", prescription.Form)
			}

			if prescription.Frequency != "" {
				addLine("Fréquence :", prescription.Frequency)
			}

			if prescription.Duration != "" {
				addLine("Durée :", prescription.Duration)
			}

			if prescription.Route != "" {
				addLine("Voie :", prescription.Route)
			}

			if prescription.Instructions != "" {
				addLine("Instructions :", prescription.Instructions)
			}

			pdf.Ln(2)
		}
	}

	drawPDFSectionTitle(pdf, "Repos maladie")

	if !c.SickLeaveRequired {
		addParagraph("Aucun repos maladie prescrit.")
	} else {
		addLine("Durée :", fmt.Sprintf("%d jour(s)", c.SickLeaveDays))
		addLine("Date de début :", formatDatePDF(c.SickLeaveStartDate))
		addLine("Date de fin :", formatDatePDF(c.SickLeaveEndDate))
	}

	pdf.Ln(5)

	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(
		branding.Clinic.Muted.R,
		branding.Clinic.Muted.G,
		branding.Clinic.Muted.B,
	)
	pdf.CellFormat(
		190,
		5,
		pdfText("Document généré le : "+time.Now().Format("02/01/2006 15:04")),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawSignatureArea(pdf, c.DoctorName)
	drawClinicFooter(pdf)

	var buf bytes.Buffer

	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func patientBirthOrAge(c *Consultation) string {
	if c.Patient.DateNaissance != nil {
		return c.Patient.DateNaissance.Format("02/01/2006")
	}

	if c.Patient.Age != nil {
		return fmt.Sprintf("%d ans", *c.Patient.Age)
	}

	return "-"
}

func GenerateHospitalizationPDF(c *Consultation) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(190, 10, pdfText("FICHE D'HOSPITALISATION"))
	pdf.Ln(15)

	pdf.SetFont("Helvetica", "", 12)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Patient : %s", patientFullName(c))))
	pdf.Ln(8)

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		pdf.Cell(190, 8, pdfText(fmt.Sprintf("Matricule assuré : %s", c.Patient.MatriculeAssure)))
		pdf.Ln(8)
	}

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Date de naissance / âge : %s", patientBirthOrAge(c))))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Médecin : %s", c.DoctorName)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Service : %s", c.Service)))
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(190, 8, pdfText("Motif d'hospitalisation"))
	pdf.Ln(8)

	pdf.SetFont("Helvetica", "", 12)
	pdf.MultiCell(190, 8, pdfText(c.HospitalizationReason), "", "", false)
	pdf.Ln(5)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Type d'hospitalisation : %s", c.HospitalizationType)))
	pdf.Ln(8)

	pdf.Cell(190, 8, pdfText(fmt.Sprintf("Durée souhaitée : %d jour(s)", c.HospitalizationDuration)))
	pdf.Ln(15)

	pdf.Cell(190, 8, pdfText("Fait le : "+time.Now().Format("02/01/2006 15:04")))
	pdf.Ln(20)

	pdf.Cell(190, 8, pdfText("Signature et cachet du médecin"))

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}
