package consultations

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/jung-kurt/gofpdf"
	"github.com/lallene/medcore-his/backend/internal/core/branding"
)

const clinicLogoName = "clinic-logo"

// Palette unique utilisée par tous les documents générés (ordonnance,
// repos maladie, demande d'examens, compte rendu, hospitalisation) afin
// que chaque PDF partage la même identité visuelle.
var (
	colorNavyDeep   = [3]int{11, 33, 68}
	colorNavy       = [3]int{18, 48, 92}
	colorTeal       = [3]int{22, 168, 140}
	colorGold       = [3]int{201, 168, 76}
	colorInk        = [3]int{30, 42, 58}
	colorMuted      = [3]int{91, 107, 125}
	colorLine       = [3]int{221, 227, 234}
	colorCardBg     = [3]int{231, 245, 241}
	colorCardBorder = [3]int{211, 233, 227}
	colorRowAlt     = [3]int{245, 247, 244}
	colorIvory      = [3]int{251, 250, 247}
	colorRefMuted   = [3]int{185, 198, 218}
)

func fillRGB(pdf *gofpdf.Fpdf, c [3]int) { pdf.SetFillColor(c[0], c[1], c[2]) }
func textRGB(pdf *gofpdf.Fpdf, c [3]int) { pdf.SetTextColor(c[0], c[1], c[2]) }
func drawRGB(pdf *gofpdf.Fpdf, c [3]int) { pdf.SetDrawColor(c[0], c[1], c[2]) }

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

// newModernDocument crée un PDF A4 avec les marges, la pagination et
// l'en-tête (logo + ruban de titre + filigrane) communs à tous les
// documents. Le titre et la référence sont redessinés automatiquement
// sur chaque nouvelle page via SetHeaderFunc, ce qui évite d'avoir à les
// reproduire manuellement à chaque saut de page.
func newModernDocument(title string, reference string) *gofpdf.Fpdf {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(12, 10, 12)
	pdf.SetAutoPageBreak(true, 26)

	registerClinicLogo(pdf)

	pdf.SetHeaderFunc(func() {
		drawModernWatermark(pdf)
		drawModernHeader(pdf)
		drawDocumentRibbon(pdf, title, reference)
	})

	pdf.SetFooterFunc(func() {
		drawModernFooter(pdf)
	})

	pdf.AddPage()

	return pdf
}

// drawModernHeader dessine le bandeau ivoire avec le logo, le nom de la
// clinique, le slogan et l'adresse, surmonté du triple filet
// navy / teal / or.
func drawModernHeader(pdf *gofpdf.Fpdf) {
	clinic := branding.Clinic

	fillRGB(pdf, colorIvory)
	pdf.Rect(0, 0, 210, 48, "F")

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

	pdf.SetXY(43, 10)
	pdf.SetFont("Times", "B", 15)
	textRGB(pdf, colorNavyDeep)
	pdf.CellFormat(157, 8, pdfText(clinic.Name), "", 1, "C", false, 0, "")

	pdf.SetX(43)
	pdf.SetFont("Arial", "B", 8.5)
	textRGB(pdf, colorTeal)
	pdf.CellFormat(157, 5, pdfText(strings.ToUpper(clinic.Tagline)), "", 1, "C", false, 0, "")

	pdf.SetX(43)
	pdf.SetFont("Arial", "", 7.5)
	textRGB(pdf, colorMuted)
	pdf.MultiCell(157, 4, pdfText(clinic.Address), "", "C", false)

	pdf.SetLineWidth(1.1)

	drawRGB(pdf, colorNavy)
	pdf.Line(0, 47, 70, 47)

	drawRGB(pdf, colorTeal)
	pdf.Line(70, 47, 150, 47)

	drawRGB(pdf, colorGold)
	pdf.Line(150, 47, 210, 47)

	pdf.SetY(47)
}

// drawDocumentRibbon dessine le bandeau bleu nuit portant le titre du
// document (ORDONNANCE, FICHE DE REPOS MALADIE, ...) et sa référence.
// La taille de police du titre se réduit automatiquement pour les
// intitulés longs afin de ne jamais déborder du bandeau.
func drawDocumentRibbon(pdf *gofpdf.Fpdf, title string, reference string) {
	fillRGB(pdf, colorNavyDeep)
	pdf.Rect(0, 48, 210, 22, "F")

	upperTitle := strings.ToUpper(title)
	availableWidth := 118.0

	titleSize := 19.0
	pdf.SetFont("Times", "B", titleSize)

	for titleSize > 12 && pdf.GetStringWidth(pdfText(upperTitle)) > availableWidth {
		titleSize -= 1.5
		pdf.SetFont("Times", "B", titleSize)
	}

	pdf.SetXY(12, 54)
	pdf.SetTextColor(255, 255, 255)
	pdf.CellFormat(availableWidth, 10, pdfText(upperTitle), "", 0, "L", false, 0, "")

	pdf.SetXY(12+availableWidth, 56)
	pdf.SetFont("Courier", "B", 8)
	textRGB(pdf, colorRefMuted)
	pdf.CellFormat(210-12-(12+availableWidth), 7, pdfText("RÉF. "+reference), "", 1, "R", false, 0, "")

	pdf.SetY(77)
}

// drawModernWatermark superpose une version très légère du logo, centrée
// verticalement sur la page, quel que soit le type de document.
func drawModernWatermark(pdf *gofpdf.Fpdf) {
	pageWidth, pageHeight := pdf.GetPageSize()

	width := 92.0
	x := pageWidth - width - 8
	y := (pageHeight - width) / 2

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

// drawModernSectionLabel dessine le liseré teal + le libellé de section
// utilisé pour introduire chaque bloc de contenu.
func drawModernSectionLabel(pdf *gofpdf.Fpdf, title string) {
	pdf.Ln(3)

	y := pdf.GetY()

	fillRGB(pdf, colorTeal)
	pdf.RoundedRect(12, y, 1.6, 6, 0.5, "1234", "F")

	pdf.SetXY(17, y-0.5)
	pdf.SetFont("Arial", "B", 9)
	textRGB(pdf, colorNavy)

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

// drawModernFieldRow affiche une ligne libellé / valeur (ex: "Médecin :
// Dr Test"), avec retour à la ligne automatique de la valeur.
func drawModernFieldRow(pdf *gofpdf.Fpdf, label string, value string) {
	if value == "" {
		value = "-"
	}

	startY := pdf.GetY()

	pdf.SetFont("Arial", "B", 8.5)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(50, 6, pdfText(label), "", 0, "L", false, 0, "")

	pdf.SetXY(62, startY)
	pdf.SetFont("Arial", "", 9.5)
	textRGB(pdf, colorInk)
	pdf.MultiCell(136, 6, pdfText(value), "", "L", false)
}

// drawModernParagraph affiche un bloc de texte libre (diagnostic,
// observations, motif, ...) sur toute la largeur du contenu.
func drawModernParagraph(pdf *gofpdf.Fpdf, value string) {
	if value == "" {
		value = "-"
	}

	pdf.SetFont("Arial", "", 9.5)
	textRGB(pdf, colorInk)
	pdf.MultiCell(186, 6, pdfText(value), "", "L", false)
}

// drawPatientIdentityCard affiche l'encart teal clair avec le nom du
// patient et sa date de naissance / âge.
func drawPatientIdentityCard(pdf *gofpdf.Fpdf, c *Consultation) {
	x := 12.0
	y := pdf.GetY()
	width := 186.0
	height := 24.0

	fillRGB(pdf, colorCardBg)
	drawRGB(pdf, colorCardBorder)
	pdf.RoundedRect(x, y, width, height, 2, "1234", "FD")

	// Colonne nom.
	pdf.SetXY(x+7, y+5)
	pdf.SetFont("Arial", "B", 7)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(82, 4, pdfText("NOM ET PRÉNOMS"), "", 1, "L", false, 0, "")

	patientName := patientFullName(c)

	fontSize := 11.0
	pdf.SetFont("Arial", "B", fontSize)

	// Réduit progressivement la taille si le texte dépasse la largeur disponible.
	for fontSize > 7.0 && pdf.GetStringWidth(pdfText(patientName)) > 78 {
		fontSize -= 0.5
		pdf.SetFont("Arial", "B", fontSize)
	}

	pdf.SetXY(x+7, y+10)
	textRGB(pdf, colorNavyDeep)

	pdf.CellFormat(
		82,
		7,
		pdfText(patientName),
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
	textRGB(pdf, colorMuted)
	pdf.CellFormat(82, 4, pdfText("DATE DE NAISSANCE / ÂGE"), "", 1, "L", false, 0, "")

	pdf.SetXY(x+97, y+10)
	pdf.SetFont("Arial", "B", 11)
	textRGB(pdf, colorNavyDeep)
	pdf.CellFormat(82, 7, pdfText(patientBirthOrAge(c)), "", 0, "L", false, 0, "")

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
		fillRGB(pdf, fillColor)
		pdf.Rect(x, y, width, height, "F")
	}

	drawRGB(pdf, colorLine)
	pdf.Line(x, y+height, x+width, y+height)

	style := ""
	if bold {
		style = "B"
	}

	pdf.SetFont("Arial", style, 7.4)
	textRGB(pdf, textColor)

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

// prescriptionTableWidths / prescriptionTableHeaders sont partagés entre
// le dessin initial de l'en-tête du tableau et sa reconduction sur les
// pages suivantes, afin de garder les deux parfaitement synchronisés.
var prescriptionTableWidths = []float64{8, 34, 21, 23, 22, 20, 58}

var prescriptionTableHeaders = []string{
	"N°",
	"MÉDICAMENT",
	"DOSAGE",
	"FORME",
	"DURÉE",
	"VOIE",
	"INSTRUCTIONS",
}

func drawPrescriptionTableHeader(pdf *gofpdf.Fpdf) {
	x := 12.0
	headerHeight := 10.0

	fillRGB(pdf, colorNavy)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Arial", "B", 7)

	currentX := x

	for index, header := range prescriptionTableHeaders {
		align := "L"
		if index == 0 {
			align = "C"
		}

		pdf.SetXY(currentX, pdf.GetY())
		pdf.CellFormat(
			prescriptionTableWidths[index],
			headerHeight,
			pdfText(header),
			"",
			0,
			align,
			true,
			0,
			"",
		)

		currentX += prescriptionTableWidths[index]
	}

	pdf.Ln(headerHeight)
}

func drawPrescriptionTable(
	pdf *gofpdf.Fpdf,
	prescriptions []ConsultationPrescription,
) {
	if len(prescriptions) == 0 {
		pdf.SetFont("Arial", "", 9)
		textRGB(pdf, colorMuted)
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

	drawPrescriptionTableHeader(pdf)

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
			// La police doit être fixée avant chaque SplitText pour
			// correspondre exactement à celle utilisée au rendu
			// (drawWrappedTableCell), faute de quoi le nombre de
			// lignes calculé ici peut différer du nombre de lignes
			// réellement affichées.
			bold := columnIndex == 0 || columnIndex == 1

			style := ""
			if bold {
				style = "B"
			}

			pdf.SetFont("Arial", style, 7.4)

			lines := pdf.SplitText(
				pdfText(value),
				prescriptionTableWidths[columnIndex]-4,
			)

			lineCounts[columnIndex] = len(lines)
		}

		rowHeight := float64(maxInt(lineCounts...))*4.5 + 5

		// Nouvelle page si la ligne dépasse la zone disponible. Le
		// saut de page déclenche automatiquement le SetHeaderFunc
		// (en-tête + ruban + filigrane) : il ne reste qu'à reproduire
		// le libellé de section et l'en-tête du tableau.

		const prescriptionTableBottomY = 250.0

		if pdf.GetY()+rowHeight > prescriptionTableBottomY {
			pdf.AddPage()

			drawModernSectionLabel(pdf, "Médicaments prescrits (suite)")
			drawPrescriptionTableHeader(pdf)
		}

		rowY := pdf.GetY()
		currentX := x

		fill := index%2 == 1

		for columnIndex, value := range values {
			align := "L"
			bold := false
			textColor := colorInk

			if columnIndex == 0 {
				align = "C"
				bold = true
				textColor = colorTeal
			}

			if columnIndex == 1 {
				bold = true
				textColor = colorNavyDeep
			}

			if columnIndex == 6 {
				textColor = colorMuted
			}

			drawWrappedTableCell(
				pdf,
				currentX,
				rowY,
				prescriptionTableWidths[columnIndex],
				rowHeight,
				value,
				align,
				bold,
				textColor,
				fill,
				colorRowAlt,
			)

			currentX += prescriptionTableWidths[columnIndex]
		}

		pdf.SetY(rowY + rowHeight)
	}
}

// drawModernSignatureArea affiche le nom du médecin à gauche et un
// encadré pointillé "Signature & cachet" à droite. Utilisée par tous
// les documents pour garder une présentation homogène.
func drawModernSignatureArea(pdf *gofpdf.Fpdf, doctorName string) {
	y := pdf.GetY() + 15

	if y > 230 {
		pdf.AddPage()
		y = pdf.GetY() + 8
	}

	// Médecin.
	pdf.SetXY(12, y)
	pdf.SetFont("Arial", "", 7)
	textRGB(pdf, colorMuted)
	pdf.CellFormat(70, 5, pdfText("MÉDECIN"), "", 1, "L", false, 0, "")

	pdf.SetX(12)
	pdf.SetFont("Arial", "B", 11)
	textRGB(pdf, colorNavyDeep)
	pdf.CellFormat(70, 7, pdfText(doctorName), "", 0, "L", false, 0, "")

	// Cadre signature.
	boxX := 143.0
	boxY := y - 5
	boxWidth := 55.0
	boxHeight := 28.0

	drawRGB(pdf, [3]int{183, 195, 210})
	pdf.SetLineWidth(0.5)
	pdf.SetDashPattern([]float64{2, 1}, 0)
	pdf.RoundedRect(boxX, boxY, boxWidth, boxHeight, 2, "1234", "D")
	pdf.SetDashPattern([]float64{}, 0)

	pdf.SetXY(boxX, boxY+10)
	pdf.SetFont("Arial", "B", 7.5)
	textRGB(pdf, [3]int{166, 178, 195})
	pdf.CellFormat(boxWidth, 6, pdfText("SIGNATURE & CACHET"), "", 0, "C", false, 0, "")

	pdf.SetY(boxY + boxHeight + 5)
}

// drawModernFooter affiche la mention légale et la bande diagonale
// navy / teal en bas de chaque page.
func drawModernFooter(pdf *gofpdf.Fpdf) {
	clinic := branding.Clinic

	pdf.SetY(-24)

	pdf.SetFont("Arial", "", 7)
	textRGB(pdf, colorMuted)

	legal := fmt.Sprintf(
		"%s au capital de %s - RCCM : %s",
		clinic.LegalForm,
		clinic.Capital,
		clinic.RCCM,
	)

	pdf.CellFormat(186, 4, pdfText(legal), "", 1, "C", false, 0, "")

	pdf.SetFont("Arial", "I", 7)
	textRGB(pdf, colorTeal)
	pdf.CellFormat(186, 4, pdfText(clinic.Signature), "", 1, "C", false, 0, "")

	drawModernFooterStrip(pdf)
}

func drawModernFooterStrip(pdf *gofpdf.Fpdf) {
	y := 293.0
	stripeWidth := 8.0
	x := 0.0
	useNavy := true

	for x < 210 {
		if useNavy {
			fillRGB(pdf, colorNavy)
		} else {
			fillRGB(pdf, colorTeal)
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
