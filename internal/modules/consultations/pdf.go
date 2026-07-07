package consultations

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
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
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(190, 10, pdfText("ORDONNANCE"))
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
	pdf.Cell(190, 8, pdfText("Prescriptions"))
	pdf.Ln(10)

	pdf.SetFont("Helvetica", "", 12)

	if len(c.Prescriptions) == 0 {
		pdf.Cell(190, 8, pdfText("Aucune prescription renseignée."))
		pdf.Ln(8)
	} else {
		for index, p := range c.Prescriptions {
			pdf.SetFont("Helvetica", "B", 12)
			pdf.Cell(190, 8, pdfText(fmt.Sprintf("%d. %s", index+1, p.MedicationName)))
			pdf.Ln(8)

			pdf.SetFont("Helvetica", "", 12)

			if p.Dosage != "" {
				pdf.Cell(190, 7, pdfText("Dosage : "+p.Dosage))
				pdf.Ln(7)
			}

			if p.Form != "" {
				pdf.Cell(190, 7, pdfText("Forme : "+p.Form))
				pdf.Ln(7)
			}

			if p.Frequency != "" {
				pdf.Cell(190, 7, pdfText("Fréquence : "+p.Frequency))
				pdf.Ln(7)
			}

			if p.Duration != "" {
				pdf.Cell(190, 7, pdfText("Durée : "+p.Duration))
				pdf.Ln(7)
			}

			if p.Route != "" {
				pdf.Cell(190, 7, pdfText("Voie : "+p.Route))
				pdf.Ln(7)
			}

			if p.Instructions != "" {
				pdf.MultiCell(190, 7, pdfText("Instructions : "+p.Instructions), "", "", false)
			}

			pdf.Ln(5)
		}
	}

	pdf.Ln(10)
	pdf.Cell(190, 8, pdfText("Fait le : "+time.Now().Format("02/01/2006 15:04")))
	pdf.Ln(20)

	pdf.Cell(190, 8, pdfText("Signature et cachet du médecin"))

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
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	sectionTitle := func(title string) {
		pdf.Ln(4)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.Cell(190, 8, pdfText(title))
		pdf.Ln(9)
	}

	addLine := func(label string, value string) {
		if value == "" {
			value = "-"
		}

		pdf.SetFont("Helvetica", "B", 10)
		pdf.Cell(45, 7, pdfText(label))

		pdf.SetFont("Helvetica", "", 10)
		pdf.MultiCell(145, 7, pdfText(value), "", "", false)
	}

	// En-tête
	pdf.SetFont("Helvetica", "B", 17)
	pdf.Cell(190, 10, pdfText("COMPTE RENDU DE CONSULTATION"))
	pdf.Ln(14)

	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(
		190,
		7,
		pdfText(fmt.Sprintf("Consultation N° : %d", c.ID)),
	)
	pdf.Ln(7)

	// Constantes
	if hasConsultationVitals(c) {
		sectionTitle("CONSTANTES VITALES")

		if c.Vitals.Temperature != nil {
			addLine(
				"Température :",
				fmt.Sprintf("%.1f °C", *c.Vitals.Temperature),
			)
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
			addLine(
				"Fréquence cardiaque :",
				fmt.Sprintf("%d bpm", *c.Vitals.HeartRate),
			)
		}

		if c.Vitals.RespiratoryRate != nil {
			addLine(
				"Fréquence respiratoire :",
				fmt.Sprintf(
					"%d cycles/min",
					*c.Vitals.RespiratoryRate,
				),
			)
		}

		if c.Vitals.OxygenSaturation != nil {
			addLine(
				"Saturation O2 :",
				fmt.Sprintf("%d %%", *c.Vitals.OxygenSaturation),
			)
		}

		if c.Vitals.Weight != nil {
			addLine(
				"Poids :",
				fmt.Sprintf("%.1f kg", *c.Vitals.Weight),
			)
		}

		if c.Vitals.Height != nil {
			addLine(
				"Taille :",
				fmt.Sprintf("%.1f cm", *c.Vitals.Height),
			)
		}

		if c.Vitals.BloodGlucose != nil {
			addLine(
				"Glycémie :",
				fmt.Sprintf("%.2f", *c.Vitals.BloodGlucose),
			)
		}

		if c.Vitals.PainScore != nil {
			addLine(
				"Score douleur :",
				fmt.Sprintf("%d / 10", *c.Vitals.PainScore),
			)
		}
	}

	// Diagnostic
	sectionTitle("DIAGNOSTIC")

	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(
		190,
		7,
		pdfText(c.Diagnosis),
		"",
		"",
		false,
	)

	// Observations
	sectionTitle("OBSERVATIONS")

	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(
		190,
		7,
		pdfText(c.Observations),
		"",
		"",
		false,
	)

	// Traitement
	sectionTitle("TRAITEMENT")

	pdf.SetFont("Helvetica", "", 10)
	pdf.MultiCell(
		190,
		7,
		pdfText(c.Treatment),
		"",
		"",
		false,
	)

	// Examens
	sectionTitle("EXAMENS DEMANDÉS")

	if len(c.Exams) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		pdf.Cell(190, 7, pdfText("Aucun examen demandé."))
		pdf.Ln(7)
	} else {
		for _, exam := range c.Exams {
			pdf.SetFont("Helvetica", "", 10)

			line := fmt.Sprintf(
				"- %s (%s)",
				exam.Name,
				exam.Category,
			)

			pdf.Cell(190, 7, pdfText(line))
			pdf.Ln(7)
		}
	}

	// Prescriptions
	sectionTitle("PRESCRIPTIONS")

	if len(c.Prescriptions) == 0 {
		pdf.SetFont("Helvetica", "", 10)
		pdf.Cell(
			190,
			7,
			pdfText("Aucune prescription renseignée."),
		)
		pdf.Ln(7)
	} else {
		for index, prescription := range c.Prescriptions {
			pdf.SetFont("Helvetica", "B", 10)

			pdf.Cell(
				190,
				7,
				pdfText(
					fmt.Sprintf(
						"%d. %s",
						index+1,
						prescription.MedicationName,
					),
				),
			)

			pdf.Ln(7)
			pdf.SetFont("Helvetica", "", 10)

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
				addLine(
					"Instructions :",
					prescription.Instructions,
				)
			}

			pdf.Ln(3)
		}
	}

	// Repos maladie
	sectionTitle("REPOS MALADIE")

	if !c.SickLeaveRequired {
		pdf.SetFont("Helvetica", "", 10)
		pdf.Cell(
			190,
			7,
			pdfText("Aucun repos maladie prescrit."),
		)
		pdf.Ln(7)
	} else {
		addLine(
			"Durée :",
			fmt.Sprintf("%d jour(s)", c.SickLeaveDays),
		)

		addLine(
			"Date de début :",
			formatDatePDF(c.SickLeaveStartDate),
		)

		addLine(
			"Date de fin :",
			formatDatePDF(c.SickLeaveEndDate),
		)
	}

	// Signature
	pdf.Ln(15)

	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(
		190,
		7,
		pdfText(
			"Document généré le : "+
				time.Now().Format("02/01/2006 15:04"),
		),
	)

	pdf.Ln(20)

	pdf.Cell(
		190,
		7,
		pdfText("Signature et cachet du médecin"),
	)

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
