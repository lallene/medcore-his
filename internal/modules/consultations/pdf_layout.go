package consultations

import (
	"bytes"
	"fmt"
	"strings"

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

	pdf.SetXY(57, 10)

	pdf.SetFont("Arial", "B", 14)
	pdf.SetTextColor(clinic.Primary.R, clinic.Primary.G, clinic.Primary.B)
	pdf.CellFormat(143, 7, pdfText(strings.ToUpper(clinic.Name)), "", 1, "L", false, 0, "")

	pdf.SetX(57)
	pdf.SetFont("Arial", "", 9)
	pdf.SetTextColor(clinic.Accent.R, clinic.Accent.G, clinic.Accent.B)
	pdf.CellFormat(143, 5, pdfText(clinic.Tagline), "", 1, "L", false, 0, "")

	pdf.SetX(57)
	pdf.SetFont("Arial", "", 7.5)
	pdf.SetTextColor(clinic.Muted.R, clinic.Muted.G, clinic.Muted.B)
	pdf.MultiCell(143, 4, pdfText(clinic.Address), "", "L", false)

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
