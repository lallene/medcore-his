package consultations

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/lallene/medcore-his/backend/internal/core/branding"
	"golang.org/x/text/encoding/charmap"
)

func pdfText(value string) string {
	replacer := strings.NewReplacer(
		"’", "'",
		"‘", "'",
		"“", `"`,
		"”", `"`,
		"–", "-",
		"—", "-",
		"•", "-",
		"…", "...",
		"œ", "oe",
		"Œ", "OE",
		"\u00a0", " ",
	)

	cleaned := replacer.Replace(value)

	out, err := charmap.ISO8859_1.NewEncoder().String(cleaned)
	if err == nil {
		return out
	}

	return strings.Map(func(r rune) rune {
		if r >= 32 && r <= 255 {
			return r
		}
		return '?'
	}, cleaned)
}

func patientFullName(c *Consultation) string {
	name := strings.TrimSpace(c.Patient.Nom + " " + c.Patient.Prenoms)
	if name == "" {
		return fmt.Sprintf("Patient #%d", c.PatientID)
	}

	return name
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

func formatDatePDF(value *time.Time) string {
	if value == nil {
		return "-"
	}

	return value.Format("02/01/2006")
}

func formatDateTimePDF(value time.Time) string {
	return value.Local().Format("02/01/2006 15:04")
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

// ---------------------------------------------------------------------
// Fiche de repos maladie
// ---------------------------------------------------------------------

func GenerateSickLeavePDF(c *Consultation) ([]byte, error) {
	// NB : le domaine "branding" fourni ne définit pas de
	// branding.DocumentType dédié aux repos maladie. On construit donc
	// une référence lisible à partir du numéro de consultation plutôt
	// que d'appeler branding.DocumentReference avec un type inexistant.
	reference := branding.DocumentReference(
		branding.DocumentTypeSickLeave,
		c.ID,
		c.CreatedAt,
	)

	pdf := newModernDocument("Fiche de repos maladie", reference)

	drawModernSectionLabel(pdf, "Informations du patient")
	drawPatientIdentityCard(pdf, c)

	drawModernSectionLabel(pdf, "Repos maladie")

	if !c.SickLeaveRequired {
		drawModernParagraph(pdf, "Aucun repos maladie n'a été prescrit pour cette consultation.")
	} else {
		drawModernFieldRow(pdf, "Durée du repos :", fmt.Sprintf("%d jour(s)", c.SickLeaveDays))
		drawModernFieldRow(pdf, "Date de début :", formatDatePDF(c.SickLeaveStartDate))
		drawModernFieldRow(pdf, "Date de fin :", formatDatePDF(c.SickLeaveEndDate))
	}

	drawModernSectionLabel(pdf, "Diagnostic")
	drawModernParagraph(pdf, c.Diagnosis)

	drawModernSectionLabel(pdf, "Observations")
	drawModernParagraph(pdf, c.Observations)

	pdf.Ln(5)
	pdf.SetFont("Arial", "I", 7.5)
	textRGB(pdf, colorMuted)
	now := time.Now()
	pdf.CellFormat(
		186,
		5,
		pdfText(
			"Document généré le "+
				now.Format("02/01/2006")+
				" à "+
				now.Format("15:04"),
		),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawModernSignatureArea(pdf, c.DoctorName)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}

// ---------------------------------------------------------------------
// Demande / autorisation d'examens
// ---------------------------------------------------------------------

func GenerateExamRequestPDF(c *Consultation) ([]byte, error) {
	reference := branding.DocumentReference(
		branding.DocumentTypeExamRequest,
		c.ID,
		c.CreatedAt,
	)

	pdf := newModernDocument("Demande / autorisation d'examens", reference)

	drawModernSectionLabel(pdf, "Informations du patient")
	drawPatientIdentityCard(pdf, c)

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		drawModernFieldRow(pdf, "Matricule assuré :", c.Patient.MatriculeAssure)
	}

	drawModernSectionLabel(pdf, "Examens demandés")

	if len(c.Exams) == 0 {
		drawModernParagraph(pdf, "Aucun examen demandé.")
	} else {
		for index, exam := range c.Exams {
			fillRGB(pdf, colorCardBg)
			drawRGB(pdf, colorCardBorder)

			startY := pdf.GetY()
			pdf.Rect(12, startY, 186, 9, "FD")

			pdf.SetXY(15, startY+2)
			pdf.SetFont("Arial", "B", 10)
			textRGB(pdf, colorNavy)

			pdf.CellFormat(
				180,
				5,
				pdfText(fmt.Sprintf("%d. %s", index+1, exam.Name)),
				"",
				1,
				"L",
				false,
				0,
				"",
			)

			if exam.Category != "" {
				drawModernFieldRow(pdf, "Catégorie :", exam.Category)
			}

			pdf.Ln(4)
		}
	}

	drawModernSectionLabel(pdf, "Renseignement clinique")
	drawModernParagraph(pdf, c.Diagnosis)

	if len(c.Reasons) > 0 {
		var reasonNames []string

		for _, reason := range c.Reasons {
			reasonNames = append(reasonNames, reason.Name)
		}

		drawModernSectionLabel(pdf, "Motifs associés")
		drawModernParagraph(pdf, strings.Join(reasonNames, ", "))
	}

	drawModernSectionLabel(pdf, "Autorisation")
	drawModernParagraph(
		pdf,
		"Le présent document autorise la réalisation des examens médicaux mentionnés ci-dessus dans le cadre de la prise en charge du patient.",
	)

	pdf.Ln(5)
	pdf.SetFont("Arial", "I", 7.5)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(
		186,
		5,
		pdfText("Document généré le : "+time.Now().Format("02/01/2006 15:04")),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawModernSignatureArea(pdf, c.DoctorName)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}

// ---------------------------------------------------------------------
// Ordonnance
// ---------------------------------------------------------------------

func GeneratePrescriptionPDF(c *Consultation) ([]byte, error) {
	reference := branding.DocumentReference(
		branding.DocumentTypePrescription,
		c.ID,
		c.CreatedAt,
	)

	pdf := newModernDocument("Ordonnance", reference)

	drawModernSectionLabel(pdf, "Informations du patient")
	drawPatientIdentityCard(pdf, c)

	drawModernSectionLabel(pdf, "Médicaments prescrits")
	drawPrescriptionTable(pdf, c.Prescriptions)

	pdf.Ln(12)

	pdf.SetFont("Arial", "I", 7.5)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(
		186,
		5,
		pdfText(
			"Document généré le "+
				time.Now().Format("02/01/2006")+" à "+
				time.Now().Format("15:04"),
		),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawModernSignatureArea(pdf, c.DoctorName)

	var buf bytes.Buffer

	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------
// Compte rendu de consultation
// ---------------------------------------------------------------------

func GenerateConsultationReportPDF(c *Consultation) ([]byte, error) {
	reference := branding.DocumentReference(
		branding.DocumentTypeConsultationReport,
		c.ID,
		c.CreatedAt,
	)

	pdf := newModernDocument("Compte rendu de consultation", reference)

	drawModernSectionLabel(pdf, "Informations du patient")
	drawPatientIdentityCard(pdf, c)

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		drawModernFieldRow(pdf, "Matricule assuré :", c.Patient.MatriculeAssure)
	}

	drawModernSectionLabel(pdf, "Informations de la consultation")
	drawModernFieldRow(pdf, "Médecin :", c.DoctorName)
	drawModernFieldRow(pdf, "Service :", c.Service)
	drawModernFieldRow(pdf, "Statut :", consultationStatusLabel(c.Status))
	drawModernFieldRow(pdf, "Date de création :", formatDateTimePDF(c.CreatedAt))

	if c.StartedAt != nil {
		drawModernFieldRow(pdf, "Débutée le :", formatDateTimePDF(*c.StartedAt))
	}

	if c.CompletedAt != nil {
		drawModernFieldRow(pdf, "Terminée le :", formatDateTimePDF(*c.CompletedAt))
	}

	if c.CancelledAt != nil {
		drawModernFieldRow(pdf, "Annulée le :", formatDateTimePDF(*c.CancelledAt))

		if c.CancellationReason != "" {
			drawModernFieldRow(pdf, "Motif d'annulation :", c.CancellationReason)
		}
	}

	if hasConsultationVitals(c) {
		drawModernSectionLabel(pdf, "Constantes vitales")

		if c.Vitals.Temperature != nil {
			drawModernFieldRow(pdf, "Température :", fmt.Sprintf("%.1f °C", *c.Vitals.Temperature))
		}

		if c.Vitals.BloodPressureSystolic != nil && c.Vitals.BloodPressureDiastolic != nil {
			drawModernFieldRow(
				pdf,
				"Tension artérielle :",
				fmt.Sprintf(
					"%d/%d mmHg",
					*c.Vitals.BloodPressureSystolic,
					*c.Vitals.BloodPressureDiastolic,
				),
			)
		}

		if c.Vitals.HeartRate != nil {
			drawModernFieldRow(pdf, "Fréquence cardiaque :", fmt.Sprintf("%d bpm", *c.Vitals.HeartRate))
		}

		if c.Vitals.RespiratoryRate != nil {
			drawModernFieldRow(
				pdf,
				"Fréquence respiratoire :",
				fmt.Sprintf("%d cycles/min", *c.Vitals.RespiratoryRate),
			)
		}

		if c.Vitals.OxygenSaturation != nil {
			drawModernFieldRow(pdf, "Saturation O2 :", fmt.Sprintf("%d %%", *c.Vitals.OxygenSaturation))
		}

		if c.Vitals.Weight != nil {
			drawModernFieldRow(pdf, "Poids :", fmt.Sprintf("%.1f kg", *c.Vitals.Weight))
		}

		if c.Vitals.Height != nil {
			drawModernFieldRow(pdf, "Taille :", fmt.Sprintf("%.1f cm", *c.Vitals.Height))
		}

		if c.Vitals.BloodGlucose != nil {
			drawModernFieldRow(pdf, "Glycémie :", fmt.Sprintf("%.2f", *c.Vitals.BloodGlucose))
		}

		if c.Vitals.PainScore != nil {
			drawModernFieldRow(pdf, "Score douleur :", fmt.Sprintf("%d / 10", *c.Vitals.PainScore))
		}
	}

	drawModernSectionLabel(pdf, "Diagnostic")
	drawModernParagraph(pdf, c.Diagnosis)

	drawModernSectionLabel(pdf, "Observations")
	drawModernParagraph(pdf, c.Observations)

	drawModernSectionLabel(pdf, "Traitement")
	drawModernParagraph(pdf, c.Treatment)

	drawModernSectionLabel(pdf, "Examens demandés")

	if len(c.Exams) == 0 {
		drawModernParagraph(pdf, "Aucun examen demandé.")
	} else {
		for _, exam := range c.Exams {
			drawModernParagraph(pdf, fmt.Sprintf("- %s (%s)", exam.Name, exam.Category))
		}
	}

	drawModernSectionLabel(pdf, "Prescriptions")

	if len(c.Prescriptions) == 0 {
		drawModernParagraph(pdf, "Aucune prescription renseignée.")
	} else {
		for index, prescription := range c.Prescriptions {
			pdf.SetFont("Arial", "B", 9.5)
			textRGB(pdf, colorNavyDeep)
			pdf.CellFormat(
				186,
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
				drawModernFieldRow(pdf, "Dosage :", prescription.Dosage)
			}

			if prescription.Form != "" {
				drawModernFieldRow(pdf, "Forme :", prescription.Form)
			}

			if prescription.Frequency != "" {
				drawModernFieldRow(pdf, "Fréquence :", prescription.Frequency)
			}

			if prescription.Duration != "" {
				drawModernFieldRow(pdf, "Durée :", prescription.Duration)
			}

			if prescription.Route != "" {
				drawModernFieldRow(pdf, "Voie :", prescription.Route)
			}

			if prescription.Instructions != "" {
				drawModernFieldRow(pdf, "Instructions :", prescription.Instructions)
			}

			pdf.Ln(2)
		}
	}

	drawModernSectionLabel(pdf, "Repos maladie")

	if !c.SickLeaveRequired {
		drawModernParagraph(pdf, "Aucun repos maladie prescrit.")
	} else {
		drawModernFieldRow(pdf, "Durée :", fmt.Sprintf("%d jour(s)", c.SickLeaveDays))
		drawModernFieldRow(pdf, "Date de début :", formatDatePDF(c.SickLeaveStartDate))
		drawModernFieldRow(pdf, "Date de fin :", formatDatePDF(c.SickLeaveEndDate))
	}

	pdf.Ln(5)
	pdf.SetFont("Arial", "I", 7.5)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(
		186,
		5,
		pdfText("Document généré le : "+time.Now().Format("02/01/2006 15:04")),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawModernSignatureArea(pdf, c.DoctorName)

	var buf bytes.Buffer

	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------
// Fiche d'hospitalisation
// ---------------------------------------------------------------------

func GenerateHospitalizationPDF(c *Consultation) ([]byte, error) {
	// Même remarque que pour GenerateSickLeavePDF : pas de
	// branding.DocumentType dédié fourni pour ce type de document.
	reference := branding.DocumentReference(
		branding.DocumentTypeHospitalization,
		c.ID,
		c.CreatedAt,
	)

	pdf := newModernDocument("Fiche d'hospitalisation", reference)

	drawModernSectionLabel(pdf, "Informations du patient")
	drawPatientIdentityCard(pdf, c)

	if c.Patient.IsAssure && c.Patient.MatriculeAssure != "" {
		drawModernFieldRow(pdf, "Matricule assuré :", c.Patient.MatriculeAssure)
	}

	drawModernSectionLabel(pdf, "Motif d'hospitalisation")
	drawModernParagraph(pdf, c.HospitalizationReason)

	drawModernSectionLabel(pdf, "Détails de l'hospitalisation")
	drawModernFieldRow(pdf, "Type d'hospitalisation :", c.HospitalizationType)
	drawModernFieldRow(pdf, "Durée souhaitée :", fmt.Sprintf("%d jour(s)", c.HospitalizationDuration))

	pdf.Ln(5)
	pdf.SetFont("Arial", "I", 7.5)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(
		186,
		5,
		pdfText("Fait le : "+time.Now().Format("02/01/2006 15:04")),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	drawModernSignatureArea(pdf, c.DoctorName)

	var buf bytes.Buffer
	err := pdf.Output(&buf)

	return buf.Bytes(), err
}
