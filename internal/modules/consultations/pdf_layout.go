package consultations

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/lallene/medcore-his/backend/internal/core/branding"
)

const clinicLogoName = "clinic-logo"

func registerClinicLogo(pdf *gofpdf.Fpdf) {
	if len(branding.LogoBytes) == 0 {
		return
	}

	pdf.RegisterImageOptionsReader(
		clinicLogoName,
		gofpdf.ImageOptions{
			ImageType: "JPG",
			ReadDpi:   true,
		},
		bytes.NewReader(branding.LogoBytes),
	)
}

func setPDFTextColor(pdf *gofpdf.Fpdf) {
	c := branding.Clinic.Text
	pdf.SetTextColor(c.R, c.G, c.B)
}

func drawClinicHeader(pdf *gofpdf.Fpdf) {
	clinic := branding.Clinic

	pdf.ImageOptions(
		clinicLogoName,
		10,
		8,
		42,
		0,
		false,
		gofpdf.ImageOptions{
			ImageType: "JPG",
			ReadDpi:   true,
		},
		0,
		"",
	)

	pdf.SetXY(55, 10)

	pdf.SetFont("Arial", "B", 14)
	pdf.SetTextColor(
		clinic.Primary.R,
		clinic.Primary.G,
		clinic.Primary.B,
	)

	pdf.CellFormat(
		145,
		7,
		pdfText(strings.ToUpper(clinic.Name)),
		"",
		1,
		"C",
		false,
		0,
		"",
	)

	pdf.SetX(55)
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(
		clinic.Accent.R,
		clinic.Accent.G,
		clinic.Accent.B,
	)

	pdf.CellFormat(
		145,
		5,
		pdfText(clinic.Tagline),
		"",
		1,
		"C",
		false,
		0,
		"",
	)

	pdf.SetX(55)
	pdf.SetFont("Arial", "", 7.5)
	pdf.SetTextColor(
		clinic.Muted.R,
		clinic.Muted.G,
		clinic.Muted.B,
	)

	pdf.MultiCell(
		145,
		4,
		pdfText(clinic.Address),
		"",
		"C",
		false,
	)

	pdf.SetY(39)
	pdf.SetDrawColor(clinic.Accent.R, clinic.Accent.G, clinic.Accent.B)
	pdf.SetLineWidth(0.8)
	pdf.Line(10, 39, 200, 39)

	pdf.Ln(7)
	setPDFTextColor(pdf)
}

func drawClinicWatermark(pdf *gofpdf.Fpdf) {
	pageWidth, pageHeight := pdf.GetPageSize()

	watermarkWidth := 115.0
	x := (pageWidth - watermarkWidth) / 2
	y := (pageHeight - 85) / 2

	pdf.SetAlpha(0.06, "Normal")
	pdf.ImageOptions(
		clinicLogoName,
		x,
		y,
		watermarkWidth,
		0,
		false,
		gofpdf.ImageOptions{
			ImageType: "JPG",
			ReadDpi:   true,
		},
		0,
		"",
	)
	pdf.SetAlpha(1, "Normal")
}

func drawDocumentTitle(pdf *gofpdf.Fpdf, title string, reference string) {
	clinic := branding.Clinic

	pdf.SetFillColor(clinic.Primary.R, clinic.Primary.G, clinic.Primary.B)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 15)
	pdf.CellFormat(190, 11, pdfText(strings.ToUpper(title)), "", 1, "C", true, 0, "")

	pdf.SetFont("Arial", "", 8)
	pdf.SetTextColor(clinic.Muted.R, clinic.Muted.G, clinic.Muted.B)
	pdf.CellFormat(190, 7, pdfText("Référence : "+reference), "", 1, "R", false, 0, "")

	pdf.Ln(2)
	setPDFTextColor(pdf)
}

func drawPDFSectionTitle(pdf *gofpdf.Fpdf, title string) {
	clinic := branding.Clinic

	pdf.Ln(3)
	pdf.SetFillColor(clinic.Primary.R, clinic.Primary.G, clinic.Primary.B)
	pdf.Rect(10, pdf.GetY(), 2.5, 7, "F")

	pdf.SetX(16)
	pdf.SetFont("Arial", "B", 10)
	pdf.SetTextColor(clinic.Primary.R, clinic.Primary.G, clinic.Primary.B)
	pdf.CellFormat(184, 7, pdfText(strings.ToUpper(title)), "", 1, "L", false, 0, "")

	setPDFTextColor(pdf)
}

func drawSignatureArea(pdf *gofpdf.Fpdf, doctorName string) {
	pdf.Ln(10)
	y := pdf.GetY()

	pdf.SetFont("Arial", "", 9)
	setPDFTextColor(pdf)

	pdf.SetXY(10, y)
	pdf.CellFormat(85, 6, pdfText("Médecin : "+doctorName), "", 0, "L", false, 0, "")

	pdf.SetXY(115, y)
	pdf.SetFont("Arial", "B", 9)
	pdf.CellFormat(85, 6, pdfText("Signature et cachet"), "", 1, "C", false, 0, "")

	pdf.Ln(18)
}

func drawClinicFooter(pdf *gofpdf.Fpdf) {
	clinic := branding.Clinic

	pdf.SetAutoPageBreak(false, 0)
	pdf.SetY(-24)

	pdf.SetDrawColor(clinic.Accent.R, clinic.Accent.G, clinic.Accent.B)
	pdf.SetLineWidth(0.5)
	pdf.Line(10, pdf.GetY(), 200, pdf.GetY())

	pdf.Ln(3)

	pdf.SetFont("Arial", "", 7)
	pdf.SetTextColor(clinic.Muted.R, clinic.Muted.G, clinic.Muted.B)

	legal := fmt.Sprintf(
		"%s au capital de %s - RCCM : %s",
		clinic.LegalForm,
		clinic.Capital,
		clinic.RCCM,
	)

	pdf.CellFormat(190, 4, pdfText(legal), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "I", 7)
	pdf.SetTextColor(clinic.Accent.R, clinic.Accent.G, clinic.Accent.B)
	pdf.CellFormat(190, 4, pdfText(clinic.Signature), "", 1, "C", false, 0, "")
}

func drawModernClinicHeader(pdf *gofpdf.Fpdf) {
	clinic := branding.Clinic

	// Fond ivoire de l'en-tête.
	pdf.SetFillColor(251, 250, 247)
	pdf.Rect(0, 0, 210, 48, "F")

	// Logo à gauche.
	pdf.ImageOptions(
		clinicLogoName,
		12,
		8,
		28,
		0,
		false,
		gofpdf.ImageOptions{
			ImageType: "JPG",
			ReadDpi:   true,
		},
		0,
		"",
	)

	// Nom de la clinique.
	pdf.SetXY(43, 10)
	pdf.SetFont("Times", "B", 15)
	pdf.SetTextColor(11, 33, 68)
	pdf.CellFormat(
		157,
		8,
		pdfText(clinic.Name),
		"",
		1,
		"C",
		false,
		0,
		"",
	)

	// Slogan.
	pdf.SetX(43)
	pdf.SetFont("Arial", "B", 8.5)
	pdf.SetTextColor(22, 168, 140)
	pdf.CellFormat(
		157,
		5,
		pdfText("EXCELLENCE - COMPASSION - SANTÉ"),
		"",
		1,
		"C",
		false,
		0,
		"",
	)

	// Adresse.
	pdf.SetX(43)
	pdf.SetFont("Arial", "", 7.5)
	pdf.SetTextColor(91, 107, 125)
	pdf.MultiCell(
		157,
		4,
		pdfText(clinic.Address),
		"",
		"C",
		false,
	)

	// Séparateur tricolore.
	pdf.SetLineWidth(1.1)

	pdf.SetDrawColor(18, 48, 92)
	pdf.Line(0, 47, 70, 47)

	pdf.SetDrawColor(22, 168, 140)
	pdf.Line(70, 47, 150, 47)

	pdf.SetDrawColor(201, 168, 76)
	pdf.Line(150, 47, 210, 47)

	pdf.SetY(47)
}

func drawPrescriptionRibbon(
	pdf *gofpdf.Fpdf,
	reference string,
) {
	pdf.SetFillColor(11, 33, 68)
	pdf.Rect(0, 48, 210, 22, "F")

	pdf.SetXY(12, 54)
	pdf.SetFont("Times", "B", 19)
	pdf.SetTextColor(255, 255, 255)

	pdf.CellFormat(
		100,
		10,
		pdfText("ORDONNANCE"),
		"",
		0,
		"L",
		false,
		0,
		"",
	)

	pdf.SetXY(112, 56)
	pdf.SetFont("Courier", "B", 8)
	pdf.SetTextColor(185, 198, 218)

	pdf.CellFormat(
		86,
		7,
		pdfText("RÉF. "+reference),
		"",
		1,
		"R",
		false,
		0,
		"",
	)

	pdf.SetY(77)
}

func drawModernSectionLabel(
	pdf *gofpdf.Fpdf,
	title string,
) {
	pdf.Ln(3)

	y := pdf.GetY()

	pdf.SetFillColor(22, 168, 140)
	pdf.RoundedRect(12, y, 1.6, 6, 0.5, "1234", "F")

	pdf.SetXY(17, y-0.5)
	pdf.SetFont("Arial", "B", 9)
	pdf.SetTextColor(18, 48, 92)

	pdf.CellFormat(
		180,
		7,
		pdfText(strings.ToUpper(title)),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	pdf.Ln(3)
}

func drawPatientIdentityCard(
	pdf *gofpdf.Fpdf,
	c *Consultation,
) {
	x := 12.0
	y := pdf.GetY()
	width := 186.0
	height := 24.0

	pdf.SetFillColor(231, 245, 241)
	pdf.SetDrawColor(211, 233, 227)
	pdf.RoundedRect(x, y, width, height, 2, "1234", "FD")

	// Colonne nom.
	pdf.SetXY(x+7, y+5)
	pdf.SetFont("Arial", "B", 7)
	pdf.SetTextColor(91, 107, 125)
	pdf.CellFormat(
		82,
		4,
		pdfText("NOM ET PRÉNOMS"),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	pdf.SetXY(x+7, y+10)
	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(11, 33, 68)
	pdf.CellFormat(
		82,
		7,
		pdfText(patientFullName(c)),
		"",
		0,
		"L",
		false,
		0,
		"",
	)

	// Colonne naissance / âge.
	pdf.SetXY(x+97, y+5)
	pdf.SetFont("Arial", "B", 7)
	pdf.SetTextColor(91, 107, 125)
	pdf.CellFormat(
		82,
		4,
		pdfText("DATE DE NAISSANCE / ÂGE"),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	pdf.SetXY(x+97, y+10)
	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(11, 33, 68)
	pdf.CellFormat(
		82,
		7,
		pdfText(patientBirthOrAge(c)),
		"",
		0,
		"L",
		false,
		0,
		"",
	)

	pdf.SetY(y + height + 7)
}

func maxInt(values ...int) int {
	maxValue := 0

	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}

	return maxValue
}

func drawWrappedTableCell(
	pdf *gofpdf.Fpdf,
	x float64,
	y float64,
	width float64,
	height float64,
	text string,
	align string,
	bold bool,
	textColor [3]int,
	fill bool,
	fillColor [3]int,
) {
	if fill {
		pdf.SetFillColor(fillColor[0], fillColor[1], fillColor[2])
		pdf.Rect(x, y, width, height, "F")
	}

	pdf.SetDrawColor(221, 227, 234)
	pdf.Line(x, y+height, x+width, y+height)

	style := ""
	if bold {
		style = "B"
	}

	pdf.SetFont("Arial", style, 7.4)
	pdf.SetTextColor(textColor[0], textColor[1], textColor[2])

	pdf.SetXY(x+2, y+2)
	pdf.MultiCell(
		width-4,
		4.5,
		pdfText(text),
		"",
		align,
		false,
	)
}

func drawPrescriptionTable(
	pdf *gofpdf.Fpdf,
	prescriptions []ConsultationPrescription,
) {
	if len(prescriptions) == 0 {
		pdf.SetFont("Arial", "", 9)
		pdf.SetTextColor(91, 107, 125)
		pdf.MultiCell(
			186,
			6,
			pdfText("Aucune prescription renseignée."),
			"",
			"L",
			false,
		)
		return
	}

	x := 12.0
	headerHeight := 10.0

	widths := []float64{
		8,  // N°
		34, // Médicament
		21, // Dosage
		23, // Forme
		22, // Durée
		20, // Voie
		58, // Instructions
	}

	headers := []string{
		"N°",
		"MÉDICAMENT",
		"DOSAGE",
		"FORME",
		"DURÉE",
		"VOIE",
		"INSTRUCTIONS",
	}

	pdf.SetFillColor(18, 48, 92)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 7)

	currentX := x

	for index, header := range headers {
		align := "L"
		if index == 0 {
			align = "C"
		}

		pdf.SetXY(currentX, pdf.GetY())
		pdf.CellFormat(
			widths[index],
			headerHeight,
			pdfText(header),
			"",
			0,
			align,
			true,
			0,
			"",
		)

		currentX += widths[index]
	}

	pdf.Ln(headerHeight)

	for index, prescription := range prescriptions {
		values := []string{
			fmt.Sprintf("%d", index+1),
			prescription.MedicationName,
			prescription.Dosage,
			prescription.Form,
			prescription.Duration,
			prescription.Route,
			prescription.Instructions,
		}

		lineCounts := make([]int, len(values))

		for columnIndex, value := range values {
			lines := pdf.SplitText(
				pdfText(value),
				widths[columnIndex]-4,
			)

			lineCounts[columnIndex] = len(lines)
		}

		rowHeight := float64(maxInt(lineCounts...))*4.5 + 5

		// Nouvelle page si la ligne dépasse la zone disponible.
		if pdf.GetY()+rowHeight > 255 {
			pdf.AddPage()
			drawModernClinicHeader(pdf)
			drawPrescriptionRibbon(
				pdf,
				branding.DocumentReference(
					branding.DocumentTypePrescription,
					prescription.ConsultationID,
					time.Now(),
				),
			)
			drawModernSectionLabel(pdf, "Médicaments prescrits")
		}

		rowY := pdf.GetY()
		currentX = x

		fill := index%2 == 1
		fillColor := [3]int{245, 247, 244}

		for columnIndex, value := range values {
			align := "L"
			bold := false
			textColor := [3]int{30, 42, 58}

			if columnIndex == 0 {
				align = "C"
				bold = true
				textColor = [3]int{22, 168, 140}
			}

			if columnIndex == 1 {
				bold = true
				textColor = [3]int{11, 33, 68}
			}

			if columnIndex == 6 {
				textColor = [3]int{91, 107, 125}
			}

			drawWrappedTableCell(
				pdf,
				currentX,
				rowY,
				widths[columnIndex],
				rowHeight,
				value,
				align,
				bold,
				textColor,
				fill,
				fillColor,
			)

			currentX += widths[columnIndex]
		}

		pdf.SetY(rowY + rowHeight)
	}
}

func drawModernWatermark(pdf *gofpdf.Fpdf) {
	pageWidth, pageHeight := pdf.GetPageSize()

	width := 92.0
	x := pageWidth - width + 13
	y := pageHeight - 122

	pdf.SetAlpha(0.035, "Normal")

	pdf.ImageOptions(
		clinicLogoName,
		x,
		y,
		width,
		0,
		false,
		gofpdf.ImageOptions{
			ImageType: "JPG",
			ReadDpi:   true,
		},
		0,
		"",
	)

	pdf.SetAlpha(1, "Normal")
}

func drawModernPrescriptionSignature(
	pdf *gofpdf.Fpdf,
	doctorName string,
) {
	y := pdf.GetY() + 15

	if y > 230 {
		pdf.AddPage()
		drawModernClinicHeader(pdf)
		y = 85
	}

	// Médecin.
	pdf.SetXY(12, y)
	pdf.SetFont("Arial", "", 7)
	pdf.SetTextColor(91, 107, 125)
	pdf.CellFormat(
		70,
		5,
		pdfText("MÉDECIN"),
		"",
		1,
		"L",
		false,
		0,
		"",
	)

	pdf.SetX(12)
	pdf.SetFont("Arial", "B", 11)
	pdf.SetTextColor(11, 33, 68)
	pdf.CellFormat(
		70,
		7,
		pdfText(doctorName),
		"",
		0,
		"L",
		false,
		0,
		"",
	)

	// Cadre signature.
	boxX := 143.0
	boxY := y - 5
	boxWidth := 55.0
	boxHeight := 28.0

	pdf.SetDrawColor(183, 195, 210)
	pdf.SetLineWidth(0.5)
	pdf.SetDashPattern([]float64{2, 1}, 0)
	pdf.RoundedRect(
		boxX,
		boxY,
		boxWidth,
		boxHeight,
		2,
		"1234",
		"D",
	)
	pdf.SetDashPattern([]float64{}, 0)

	pdf.SetXY(boxX, boxY+10)
	pdf.SetFont("Arial", "B", 7.5)
	pdf.SetTextColor(166, 178, 195)
	pdf.CellFormat(
		boxWidth,
		6,
		pdfText("SIGNATURE & CACHET"),
		"",
		0,
		"C",
		false,
		0,
		"",
	)

	pdf.SetY(boxY + boxHeight + 5)
}

func drawModernFooterStrip(pdf *gofpdf.Fpdf) {
	y := 293.0
	stripeWidth := 8.0
	x := 0.0
	useNavy := true

	for x < 210 {
		if useNavy {
			pdf.SetFillColor(18, 48, 92)
		} else {
			pdf.SetFillColor(22, 168, 140)
		}

		pdf.Polygon(
			[]gofpdf.PointType{
				{X: x, Y: y},
				{X: x + stripeWidth, Y: y},
				{X: x + stripeWidth - 3, Y: y + 4},
				{X: x - 3, Y: y + 4},
			},
			"F",
		)

		x += stripeWidth
		useNavy = !useNavy
	}
}
